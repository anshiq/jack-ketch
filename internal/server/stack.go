// StartStack (Phase 9): compose-like ordered startup from depends_on.
// Apps start in topological order; each is awaited to running state
// (and to readiness when it declares patterns) before the next starts.
package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"jack-ketch/pkg/api"
)

func (s *Server) routeStack(mux *http.ServeMux) {
	mux.HandleFunc("/jackketch.v1.ProcessService/StartStack", s.wrap(s.handleStartStack))
}

// topoOrder sorts apps so dependencies start first (Kahn's algorithm).
// Unknown deps were rejected at config validation; cycles error here.
func topoOrder(apps []string, deps map[string][]string) ([]string, error) {
	in := map[string]bool{}
	for _, a := range apps {
		in[a] = true
	}
	edges := map[string][]string{}
	indeg := map[string]int{}
	for _, a := range apps {
		for _, d := range deps[a] {
			if !in[d] {
				continue // external dep: assumed already running
			}
			edges[d] = append(edges[d], a)
			indeg[a]++
		}
	}
	var queue []string
	for _, a := range apps {
		if indeg[a] == 0 {
			queue = append(queue, a)
		}
	}
	var out []string
	for len(queue) > 0 {
		a := queue[0]
		queue = queue[1:]
		out = append(out, a)
		for _, m := range edges[a] {
			indeg[m]--
			if indeg[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	if len(out) != len(apps) {
		return nil, fmt.Errorf("depends_on cycle in %v", apps)
	}
	return out, nil
}

func (s *Server) handleStartStack(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string   `json:"workspaceId"`
		Apps        []string `json:"apps"`
		SessionID   string   `json:"sessionId"`
		TimeoutMs   int      `json:"timeoutMs"`
	}
	_ = decode(r, &req)
	if req.WorkspaceID == "" || len(req.Apps) == 0 {
		return nil, fmt.Errorf("workspaceId and apps required")
	}
	timeout := 30 * time.Second
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}
	if timeout > 600*time.Second {
		timeout = 600 * time.Second
	}
	pr, rt, err := s.engine.RuntimeFor(req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	noteScope(r, req.WorkspaceID, pr.ProjectID())
	noteArgs(r, map[string]any{"apps": req.Apps})
	deps := map[string][]string{}
	if res := pr.ResolvedConfig(); res != nil {
		for name, app := range res.Apps {
			deps[name] = app.DependsOn
		}
	}
	order, err := topoOrder(req.Apps, deps)
	if err != nil {
		return nil, err
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID, _ = s.sessionOf(r)
	}
	var started []any
	for _, name := range order {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		res, _, err := s.startAndRecord(ctx, pr, rt, req.WorkspaceID, api.StartRequest{App: name}, nil, "persistent", sessionID)
		if err != nil {
			cancel()
			return map[string]any{"started": started, "order": order, "failed": name, "error": err.Error()}, nil
		}
		ready, why := waitStackReady(ctx, rt, res, timeout)
		cancel()
		started = append(started, map[string]any{"app": name, "processId": res.ProcessID, "instanceId": res.InstanceID, "ready": ready})
		if why != "" {
			return map[string]any{"started": started, "order": order, "failed": name, "error": why}, nil
		}
	}
	return map[string]any{"started": started, "order": order}, nil
}

func waitStackReady(ctx context.Context, rt *coreRT, res *api.StartResult, timeout time.Duration) (bool, string) {
	if len(res.Readiness) > 0 {
		wr, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Ready: true, TimeoutMS: int(timeout / time.Millisecond)})
		if err != nil {
			return false, err.Error()
		}
		if wr.Exited {
			return false, "exited before becoming ready"
		}
		return wr.Matched, ""
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, err := rt.Status(res.ProcessID)
		if err == nil && st.Status != "starting" && st.Status != "created" {
			if st.Status == "exited" || st.Status == "failed" || st.Status == "crashed" {
				return false, "exited during startup"
			}
			return true, ""
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true, ""
}
