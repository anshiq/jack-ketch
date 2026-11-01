package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	iruntime "jack-ketch/internal/runtime"
	"jack-ketch/pkg/api"
)

func (s *Server) routeAssist(mux *http.ServeMux) {
	p := "/jackketch.v1.ProcessService/"
	mux.HandleFunc(p+"Diagnose", s.wrap(s.handleProcDiagnose))
	mux.HandleFunc(p+"Probe", s.wrap(s.handleProcProbe))
	mux.HandleFunc(p+"RunChecks", s.wrap(s.handleProcRunChecks))
	mux.HandleFunc(p+"SaveSuggestion", s.wrap(s.handleProcSaveSuggestion))
	mux.HandleFunc(p+"Outcome", s.wrap(s.handleProcOutcome))
	c := "/jackketch.v1.ConfigService/"
	mux.HandleFunc(c+"SaveRun", s.wrap(s.cfgSaveRun))
	mux.HandleFunc(c+"GitignoreStatus", s.wrap(s.cfgGitignoreStatus))
	mux.HandleFunc(c+"UpdateGitignore", s.wrap(s.cfgUpdateGitignore))
}

func (s *Server) handleProcDiagnose(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	return rt.Diagnose(req.ProcessID)
}

func (s *Server) handleProcOutcome(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	return rt.Outcome(req.ProcessID)
}

func (s *Server) handleProcProbe(w http.ResponseWriter, r *http.Request) (any, error) {
	var req api.ProbeRequest
	if err := decode(r, &req); err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	pr, rt, wsID, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	noteScope(r, wsID, pr.ProjectID())
	noteArgs(r, map[string]any{"processId": req.ProcessID, "method": req.Method, "url": req.URL, "tcp": req.TCP, "confirmed": req.Confirmed})
	timeout := time.Duration(req.TimeoutMS)*time.Millisecond + 15*time.Second
	if req.TimeoutMS <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	res, err := rt.Probe(ctx, req)
	var ce *iruntime.ConfirmError
	if errors.As(err, &ce) {
		return map[string]any{"needsConfirmation": true, "reason": ce.Reason, "error": ce.Error()}, nil
	}
	return res, err
}

func (s *Server) handleProcRunChecks(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		api.RunChecksRequest
		WorkspaceID string `json:"workspaceId"`
	}
	if err := decode(r, &req); err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	var rt *coreRT
	if req.ProcessID != "" {
		pr, rtv, wsID, err := s.findProcess(req.ProcessID)
		if err != nil {
			return nil, err
		}
		noteScope(r, wsID, pr.ProjectID())
		rt = rtv
	} else {
		if req.WorkspaceID == "" {
			return nil, fmt.Errorf("invalid_argument: workspaceId required when processId is empty")
		}
		pr, rtv, err := s.engine.RuntimeFor(req.WorkspaceID)
		if err != nil {
			return nil, err
		}
		noteScope(r, req.WorkspaceID, pr.ProjectID())
		rt = rtv
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	return rt.RunChecks(ctx, req.RunChecksRequest)
}

func (s *Server) handleProcSaveSuggestion(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
	}
	_ = decode(r, &req)
	_, _, wsID, err := s.findProcess(req.ProcessID)
	if err != nil {
		row, rerr := s.engine.Store().GetProcess(req.ProcessID)
		if rerr != nil {
			return nil, err
		}
		wsID = row.WorkspaceID
	}
	sug, err := s.engine.SaveSuggestion(wsID, req.ProcessID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"suggestion": sug}, nil
}

func (s *Server) cfgSaveRun(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		api.SaveRunRequest
		SessionID string `json:"sessionId"`
	}
	if err := decode(r, &req); err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	if req.ProcessID == "" {
		return nil, fmt.Errorf("invalid_argument: processId required")
	}
	row, err := s.engine.Store().GetProcess(req.ProcessID)
	if err != nil {
		return nil, fmt.Errorf("unknown process %q", req.ProcessID)
	}
	pr, err := s.engine.GetOrCreateRuntime(row.WorkspaceID)
	if err != nil {
		return nil, err
	}
	noteScope(r, row.WorkspaceID, pr.ProjectID())
	noteArgs(r, map[string]any{"processId": req.ProcessID, "action": req.Action, "app": req.App, "mode": req.Mode, "scope": req.Scope})
	sess := req.SessionID
	if sess == "" {
		sess, _ = s.sessionOf(r)
	}
	res, err := s.engine.SaveRun(row.WorkspaceID, req.SaveRunRequest, sess)
	if err != nil {
		return nil, err
	}
	out := structToMap(res)
	if res.Saved && !res.Unchanged {
		if st, err := s.engine.GitignoreStatus(row.WorkspaceID); err == nil && len(st.Missing) > 0 {
			out["gitignore"] = st
		}
	}
	return out, nil
}

func (s *Server) cfgGitignoreStatus(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
	}
	_ = decode(r, &req)
	if req.WorkspaceID == "" {
		return nil, fmt.Errorf("invalid_argument: workspaceId required")
	}
	return s.engine.GitignoreStatus(req.WorkspaceID)
}

func (s *Server) cfgUpdateGitignore(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string   `json:"workspaceId"`
		Entries     []string `json:"entries"`
		Action      string   `json:"action"`
	}
	if err := decode(r, &req); err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	if req.WorkspaceID == "" {
		return nil, fmt.Errorf("invalid_argument: workspaceId required")
	}
	if req.Action != "" && req.Action != "add" && req.Action != "dismiss" {
		return nil, fmt.Errorf("invalid_argument: action must be add or dismiss")
	}
	pr, err := s.engine.GetOrCreateRuntime(req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	noteScope(r, req.WorkspaceID, pr.ProjectID())
	noteArgs(r, map[string]any{"entries": req.Entries, "action": req.Action})
	return s.engine.UpdateGitignore(req.WorkspaceID, req.Entries, req.Action == "dismiss")
}
