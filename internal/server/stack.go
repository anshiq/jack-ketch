package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"jack-ketch/internal/core"
	"jack-ketch/pkg/api"
)

func (s *Server) routeStack(mux *http.ServeMux) {
	mux.HandleFunc("/jackketch.v1.ProcessService/StartStack", s.wrap(s.handleStartStack))
}

func expandDeps(apps []string, deps map[string][]string) ([]string, []string, error) {
	seen := map[string]bool{}
	requested := map[string]bool{}
	for _, a := range apps {
		requested[a] = true
	}
	var out, pulled []string
	var visit func(a string, path []string) error
	visit = func(a string, path []string) error {
		for _, p := range path {
			if p == a {
				return fmt.Errorf("depends_on cycle: %v -> %s", path, a)
			}
		}
		if seen[a] {
			return nil
		}
		if _, ok := deps[a]; !ok {
			return fmt.Errorf("unknown app %q", a)
		}
		for _, d := range deps[a] {
			if err := visit(d, append(path, a)); err != nil {
				return err
			}
		}
		seen[a] = true
		out = append(out, a)
		if !requested[a] {
			pulled = append(pulled, a)
		}
		return nil
	}
	for _, a := range apps {
		if err := visit(a, nil); err != nil {
			return nil, nil, err
		}
	}
	return out, pulled, nil
}

func (s *Server) handleStartStack(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string            `json:"workspaceId"`
		Apps        []string          `json:"apps"`
		Modes       map[string]string `json:"modes"`
		SessionID   string            `json:"sessionId"`
		TimeoutMs   int               `json:"timeoutMs"`
		NoReuse     bool              `json:"noReuse"`
	}
	_ = decode(r, &req)
	if req.WorkspaceID == "" {
		return nil, fmt.Errorf("workspaceId required")
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
	noteArgs(r, map[string]any{"apps": req.Apps, "modes": req.Modes})
	deps := map[string][]string{}
	if res := pr.ResolvedConfig(); res != nil {
		for name, app := range res.Apps {
			deps[name] = app.DependsOn
		}
	}
	apps := req.Apps
	if len(apps) == 0 {
		for name := range deps {
			apps = append(apps, name)
		}
		sort.Strings(apps)
	}
	if len(apps) == 0 {
		return nil, fmt.Errorf("this project declares no apps")
	}
	order, pulled, err := expandDeps(apps, deps)
	if err != nil {
		return nil, err
	}
	running := map[string]api.ProcessSummary{}
	if !req.NoReuse {
		if l, err := rt.List(); err == nil {
			for _, p := range l.Processes {
				if p.App == "" || (p.Status != "running" && p.Status != "starting") {
					continue
				}
				if want := req.Modes[p.App]; want != "" && want != p.Mode {
					continue
				}
				if _, ok := running[p.App]; !ok {
					running[p.App] = p
				}
			}
		}
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID, _ = s.sessionOf(r)
	}
	started := []any{}
	result := func(extra map[string]any) map[string]any {
		out := map[string]any{"started": started, "order": order}
		if len(pulled) > 0 {
			out["pulledIn"] = pulled
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	for _, name := range order {
		if p, ok := running[name]; ok {
			started = append(started, map[string]any{"app": name, "processId": p.ProcessID, "instanceId": p.InstanceID, "mode": p.Mode, "ready": true, "reused": true})
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		mode := req.Modes[name]
		res, _, err := s.startAndRecord(ctx, pr, rt, req.WorkspaceID, api.StartRequest{App: name, Mode: mode}, core.RunOrigin{App: name}, "persistent", sessionID)
		if err != nil {
			cancel()
			return result(map[string]any{"failed": name, "error": err.Error()}), nil
		}
		ready, why := waitStackReady(ctx, rt, res, timeout)
		cancel()
		started = append(started, map[string]any{"app": name, "processId": res.ProcessID, "instanceId": res.InstanceID, "mode": res.Mode, "ready": ready})
		if why != "" {
			return result(map[string]any{"failed": name, "failedProcessId": res.ProcessID, "error": why}), nil
		}
	}
	return result(nil), nil
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
