package server

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"jack-ketch/internal/devproxy"
	"jack-ketch/internal/logs"
)

func (s *Server) routeBrowser(mux *http.ServeMux) {
	p := "/jackketch.v1.ProcessService/"
	mux.HandleFunc(p+"StartBrowserProxy", s.wrap(s.handleBrowserStart))
	mux.HandleFunc(p+"ListBrowserProxies", s.wrap(s.handleBrowserList))
	mux.HandleFunc(p+"StopBrowserProxy", s.wrap(s.handleBrowserStop))
}

func (s *Server) handleBrowserStart(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID  string `json:"processId"`
		URL        string `json:"url"`
		Port       int    `json:"port"`
		TTLMinutes int    `json:"ttlMinutes"`
		Warnings   bool   `json:"warnings"`
	}
	if err := decode(r, &req); err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	pr, rt, wsID, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	noteScope(r, wsID, pr.ProjectID())
	noteArgs(r, map[string]any{"processId": req.ProcessID, "url": req.URL, "port": req.Port})
	raw := req.URL
	if raw == "" {
		base, err := rt.BaseURL(req.ProcessID, req.Port)
		if err != nil {
			return nil, err
		}
		raw = base
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid_argument: invalid url %q", raw)
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" && host != "0.0.0.0" {
		return nil, fmt.Errorf("invalid_argument: the browser proxy only fronts dev servers on this machine, not %s", host)
	}
	if host == "0.0.0.0" {
		u.Host = "127.0.0.1:" + u.Port()
	}
	id := req.ProcessID
	px, err := s.engine.BrowserProxies().Start(devproxy.StartOptions{
		ProcessID: id, WorkspaceID: wsID, Upstream: u, Warnings: req.Warnings,
		TTL: time.Duration(req.TTLMinutes) * time.Minute,
		Sink: func(stream, line string) error {
			st := logs.StreamStderr
			if stream == "stdout" {
				st = logs.StreamStdout
			}
			return rt.AppendLog(id, st, line)
		},
	})
	if err != nil {
		return nil, err
	}
	return px, nil
}

func (s *Server) handleBrowserList(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
	}
	_ = decode(r, &req)
	return map[string]any{"proxies": s.engine.BrowserProxies().List(req.WorkspaceID)}, nil
}

func (s *Server) handleBrowserStop(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ID          string `json:"id"`
	}
	_ = decode(r, &req)
	if err := s.engine.BrowserProxies().Stop(req.WorkspaceID, req.ID); err != nil {
		return nil, err
	}
	return map[string]any{"stopped": true}, nil
}
