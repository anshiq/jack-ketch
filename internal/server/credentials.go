package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"jack-ketch/internal/credentials"
	iruntime "jack-ketch/internal/runtime"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

func (s *Server) routeCredential(mux *http.ServeMux) {
	p := "/jackketch.v1.CredentialService/"
	mux.HandleFunc(p+"List", s.wrap(s.credList))
	mux.HandleFunc(p+"Upsert", s.wrap(s.credUpsert))
	mux.HandleFunc(p+"Delete", s.wrap(s.credDelete))
	mux.HandleFunc(p+"Reveal", s.wrap(s.credReveal))
	mux.HandleFunc(p+"ClearJar", s.wrap(s.credClearJar))
	mux.HandleFunc(p+"Refresh", s.wrap(s.credRefresh))
	mux.HandleFunc(p+"ListProxies", s.wrap(s.credListProxies))
	mux.HandleFunc(p+"StartProxy", s.wrap(s.credStartProxy))
	mux.HandleFunc(p+"StopProxy", s.wrap(s.credStopProxy))
	mux.HandleFunc(p+"GetKeystore", s.wrap(s.credGetKeystore))
	mux.HandleFunc(p+"SetKeystore", s.wrap(s.credSetKeystore))
	mux.HandleFunc(p+"ResetKey", s.wrap(s.credResetKey))
}

func requireWeb(r *http.Request, what string) error {
	if !viaWeb(r) {
		return fmt.Errorf("policy_denied: %s is only available from the web UI", what)
	}
	return nil
}

func (s *Server) credProject(r *http.Request, workspaceID string) (string, error) {
	if workspaceID == "" {
		return "", fmt.Errorf("invalid_argument: workspaceId required")
	}
	ws, err := s.engine.Store().GetWorkspace(workspaceID)
	if err != nil {
		return "", fmt.Errorf("workspace not found: %s", workspaceID)
	}
	noteScope(r, workspaceID, ws.ProjectID)
	return ws.ProjectID, nil
}

func credErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, credentials.ErrNoProfile), errors.Is(err, store.ErrCredentialNotFound):
		return err
	}
	msg := err.Error()
	if strings.Contains(msg, "credentials key") || strings.Contains(msg, "cannot decrypt") || strings.Contains(msg, "keychain") {
		return err
	}
	return fmt.Errorf("invalid_argument: %v", err)
}

func (s *Server) credList(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
	}
	_ = decode(r, &req)
	project, err := s.credProject(r, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	profiles, err := s.engine.Credentials().List(project)
	if err != nil {
		return nil, err
	}
	out := api.ListCredentialsResult{Profiles: profiles}
	if len(profiles) == 0 {
		out.Hint = "No credential profiles yet. Ask the user to add one on the Credentials page of the web UI; the agent can then call probe with credential=<name> without ever seeing the secret."
	}
	return out, nil
}

func (s *Server) credUpsert(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := requireWeb(r, "saving credentials"); err != nil {
		return nil, err
	}
	var req struct {
		WorkspaceID string                      `json:"workspaceId"`
		Profile     api.UpsertCredentialRequest `json:"profile"`
	}
	if err := decode(r, &req); err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	project, err := s.credProject(r, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(req.Profile.Fields))
	for _, f := range req.Profile.Fields {
		names = append(names, f.Name)
	}
	noteArgs(r, map[string]any{"profile": req.Profile.Name, "id": req.Profile.ID, "fields": names, "default": req.Profile.Default, "recipe": req.Profile.Recipe != nil})
	got, err := s.engine.Credentials().Upsert(project, req.Profile)
	if err != nil {
		return nil, credErr(err)
	}
	return got, nil
}

func (s *Server) credDelete(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := requireWeb(r, "deleting credentials"); err != nil {
		return nil, err
	}
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ID          string `json:"id"`
	}
	_ = decode(r, &req)
	project, err := s.credProject(r, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	noteArgs(r, map[string]any{"id": req.ID})
	if err := s.engine.Credentials().Delete(project, req.ID); err != nil {
		return nil, credErr(err)
	}
	return map[string]any{"deleted": true}, nil
}

func (s *Server) credReveal(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := requireWeb(r, "revealing credentials"); err != nil {
		return nil, err
	}
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ID          string `json:"id"`
		Field       string `json:"field"`
	}
	_ = decode(r, &req)
	project, err := s.credProject(r, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	noteArgs(r, map[string]any{"id": req.ID, "field": req.Field})
	_, rt, err := s.engine.RuntimeFor(req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := rt.CheckReveal("reveal_credential", map[string]any{"id": req.ID, "field": req.Field}); err != nil {
		return nil, err
	}
	v, err := s.engine.Credentials().Reveal(project, req.ID, req.Field)
	if err != nil {
		return nil, credErr(err)
	}
	return map[string]any{"value": v}, nil
}

func (s *Server) credClearJar(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := requireWeb(r, "clearing the cookie jar"); err != nil {
		return nil, err
	}
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ID          string `json:"id"`
	}
	_ = decode(r, &req)
	project, err := s.credProject(r, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	noteArgs(r, map[string]any{"id": req.ID})
	if err := s.engine.Credentials().ClearJar(project, req.ID); err != nil {
		return nil, credErr(err)
	}
	return map[string]any{"cleared": true}, nil
}

type credTarget struct {
	ProcessID string
	Upstream  string
	Port      int
}

func (s *Server) credBase(req credTarget) (*url.URL, error) {
	if req.Upstream != "" {
		u, err := url.Parse(strings.TrimSpace(req.Upstream))
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("invalid_argument: upstream must be an http(s) URL")
		}
		return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: strings.TrimSuffix(u.Path, "/")}, nil
	}
	if req.ProcessID == "" {
		return nil, fmt.Errorf("invalid_argument: pass processId or upstream")
	}
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	return rt.ProcessBase(req.ProcessID, req.Port)
}

func (s *Server) credRefresh(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ID          string `json:"id"`
		ProcessID   string `json:"processId"`
		Upstream    string `json:"upstream"`
		Port        int    `json:"port"`
	}
	_ = decode(r, &req)
	project, err := s.credProject(r, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	noteArgs(r, map[string]any{"id": req.ID, "processId": req.ProcessID, "upstream": req.Upstream})
	loaded, err := s.engine.Credentials().GetByID(project, req.ID)
	if err != nil {
		return nil, credErr(err)
	}
	base, err := s.credBase(credTarget{req.ProcessID, req.Upstream, req.Port})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.engine.Credentials().Scope(project).Refresh(ctx, loaded.Row.Name, base); err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	profiles, err := s.engine.Credentials().List(project)
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		if p.ID == req.ID {
			return p, nil
		}
	}
	return map[string]any{"refreshed": true}, nil
}

func (s *Server) credListProxies(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
	}
	_ = decode(r, &req)
	project, err := s.credProject(r, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"proxies": s.engine.Proxies().List(project)}, nil
}

func (s *Server) credStartProxy(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		api.StartCredentialProxyRequest
	}
	if err := decode(r, &req); err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	project, err := s.credProject(r, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	noteArgs(r, map[string]any{"profile": req.Profile, "processId": req.ProcessID, "upstream": req.Upstream, "ttlMinutes": req.TTLMinutes, "confirmed": req.Confirmed})
	if strings.TrimSpace(req.Profile) == "" {
		return nil, fmt.Errorf("invalid_argument: profile required")
	}
	base, err := s.credBase(credTarget{req.ProcessID, req.Upstream, req.Port})
	if err != nil {
		return nil, err
	}
	if !req.Confirmed {
		reason := "this opens a local port that sends " + base.Host + " requests with credential profile " + req.Profile + " attached"
		if !iruntime.LoopbackTarget(base) {
			reason = "the upstream " + base.Host + " is not on this machine; " + reason
		}
		return map[string]any{"needsConfirmation": true, "reason": reason, "error": iruntime.ConfirmationRequired + ": " + reason}, nil
	}
	ttl := time.Duration(req.TTLMinutes) * time.Minute
	got, err := s.engine.Proxies().Start(project, req.Profile, base, ttl, req.Label)
	if err != nil {
		return nil, credErr(err)
	}
	return got, nil
}

func (s *Server) credStopProxy(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ID          string `json:"id"`
	}
	_ = decode(r, &req)
	project, err := s.credProject(r, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	noteArgs(r, map[string]any{"id": req.ID})
	if err := s.engine.Proxies().Stop(project, req.ID); err != nil {
		return nil, fmt.Errorf("proxy not found: %s", req.ID)
	}
	return map[string]any{"stopped": true}, nil
}

func (s *Server) credGetKeystore(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.engine.Credentials().Keys().Status(), nil
}

func (s *Server) credSetKeystore(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := requireWeb(r, "changing the credentials key storage"); err != nil {
		return nil, err
	}
	var req struct {
		Backend string `json:"backend"`
	}
	_ = decode(r, &req)
	noteArgs(r, map[string]any{"backend": req.Backend})
	st, err := s.engine.Credentials().Keys().SetBackend(req.Backend)
	if err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	return st, nil
}

func (s *Server) credResetKey(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := requireWeb(r, "resetting the credentials key"); err != nil {
		return nil, err
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	_ = decode(r, &req)
	if req.Confirm != "reset" {
		return nil, fmt.Errorf("invalid_argument: confirm must be \"reset\"")
	}
	keys := s.engine.Credentials().Keys()
	if err := keys.Reset(); err != nil {
		return nil, err
	}
	n, err := s.engine.Credentials().ResetSecrets()
	if err != nil {
		return nil, err
	}
	return map[string]any{"profilesCleared": n, "keystore": keys.Status()}, nil
}
