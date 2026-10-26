package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"jack-ketch/internal/integrate"
)

func (s *Server) routeAudit(mux *http.ServeMux) {
	p := "/jackketch.v1.AuditService/"
	mux.HandleFunc(p+"ListAudit", s.wrap(s.auditList))
}

func (s *Server) auditList(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		Limit       int    `json:"limit"`
	}
	_ = decode(r, &req)
	rows, err := s.engine.Store().ListAudit(req.WorkspaceID, req.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, a := range rows {
		out = append(out, map[string]any{
			"id": a.ID, "ts": a.TS, "sessionId": a.SessionID, "harness": a.Harness,
			"projectId": a.ProjectID, "workspaceId": a.WorkspaceID,
			"action": a.Action, "args": a.Args, "result": a.Result, "durationMs": a.DurationMs,
		})
	}
	return map[string]any{"entries": out}, nil
}

func (s *Server) routeIntegration(mux *http.ServeMux) {
	p := "/jackketch.v1.IntegrationService/"
	mux.HandleFunc(p+"ListHarnesses", s.wrap(s.intHarnesses))
	mux.HandleFunc(p+"PreviewInstall", s.wrap(s.intPreview))
	mux.HandleFunc(p+"InstallMCP", s.wrap(s.intInstallMCP))
	mux.HandleFunc(p+"RemoveMCP", s.wrap(s.intRemoveMCP))
	mux.HandleFunc(p+"GetSnippet", s.wrap(s.intSnippet))
	mux.HandleFunc(p+"Verify", s.wrap(s.intVerify))
	mux.HandleFunc(p+"ListSkills", s.wrap(s.intListSkills))
	mux.HandleFunc(p+"SkillStatus", s.wrap(s.intSkillStatus))
	mux.HandleFunc(p+"InstallSkills", s.wrap(s.intInstallSkills))
	mux.HandleFunc(p+"RemoveSkills", s.wrap(s.intRemoveSkills))
	mux.HandleFunc(p+"CheckUpdates", s.wrap(s.intCheckUpdates))
}

type integrationReq struct {
	Harness     string `json:"harness"`
	Scope       string `json:"scope"`
	ProjectPath string `json:"projectPath"`
	WorkspaceID string `json:"workspaceId"`
	Action      string `json:"action"`
}

func (s *Server) integrationTarget(r *http.Request) (integrationReq, integrate.Scope, string, error) {
	var req integrationReq
	_ = decode(r, &req)
	scope, err := integrate.ParseScope(req.Scope)
	if err != nil {
		return req, "", "", fmt.Errorf("invalid_argument: %v", err)
	}
	project := req.ProjectPath
	if project == "" && req.WorkspaceID != "" {
		ws, err := s.engine.Store().GetWorkspace(req.WorkspaceID)
		if err != nil {
			return req, "", "", err
		}
		project = ws.Path
	}
	if req.Harness != "" {
		if _, ok := integrate.Lookup(req.Harness); !ok {
			return req, "", "", fmt.Errorf("invalid_argument: unknown agent %q", req.Harness)
		}
	}
	noteArgs(r, map[string]any{"harness": req.Harness, "scope": string(scope), "projectPath": project, "action": req.Action})
	return req, scope, project, nil
}

func firstChange(changes []integrate.Change) (string, string, bool) {
	changed := false
	path, backup := "", ""
	for _, c := range changes {
		if path == "" {
			path = c.Path
		}
		if backup == "" {
			backup = c.Backup
		}
		changed = changed || c.Changed
	}
	return path, backup, changed
}

func (s *Server) intHarnesses(w http.ResponseWriter, r *http.Request) (any, error) {
	_, _, project, err := s.integrationTarget(r)
	if err != nil {
		return nil, err
	}
	reports := integrate.ReportAll(project)
	counts := map[string]int{}
	for _, sess := range s.engine.Sessions().List() {
		counts[string(sess.Harness)]++
	}
	out := make([]any, 0, len(reports))
	for _, rep := range reports {
		item := structToMap(rep)
		item["detected"] = rep.Detection.Detected
		item["version"] = rep.Detection.Version
		item["evidence"] = rep.Detection.Evidence
		item["connected"] = counts[rep.ID]
		userStatus := ""
		for _, sc := range rep.Scopes {
			if sc.Scope == integrate.ScopeUser {
				userStatus = sc.Status
			}
		}
		item["mcpGlobal"] = userStatus == "installed" || userStatus == "outdated"
		var skills []string
		if rep.SkillsAvail {
			if states, err := integrate.SkillStatus(rep.ID, integrate.ScopeUser, ""); err == nil {
				for _, st := range states {
					if st.Visible {
						skills = append(skills, st.Name)
					}
				}
			}
		}
		item["skillsGlobal"] = skills
		out = append(out, item)
	}
	return map[string]any{"harnesses": out}, nil
}

func (s *Server) intPreview(w http.ResponseWriter, r *http.Request) (any, error) {
	req, scope, project, err := s.integrationTarget(r)
	if err != nil {
		return nil, err
	}
	var changes []integrate.Change
	if req.Action == "remove" {
		changes, err = integrate.PlanRemove(req.Harness, scope, integrate.Options{ProjectDir: project})
	} else {
		changes, err = integrate.PlanInstall(req.Harness, scope, integrate.Options{ProjectDir: project})
	}
	if err != nil {
		return nil, err
	}
	var diffs []string
	for _, c := range changes {
		if c.Diff != "" {
			diffs = append(diffs, c.Diff)
		}
	}
	return map[string]any{"changes": changes, "diff": strings.Join(diffs, "\n")}, nil
}

func (s *Server) intInstallMCP(w http.ResponseWriter, r *http.Request) (any, error) {
	req, scope, project, err := s.integrationTarget(r)
	if err != nil {
		return nil, err
	}
	changes, err := integrate.Install(req.Harness, scope, integrate.Options{ProjectDir: project})
	if err != nil {
		return nil, err
	}
	path, backup, changed := firstChange(changes)
	h, _ := integrate.Lookup(req.Harness)
	return map[string]any{"changes": changes, "path": path, "backup": backup, "changed": changed, "restartHint": h.RestartHint}, nil
}

func (s *Server) intRemoveMCP(w http.ResponseWriter, r *http.Request) (any, error) {
	req, scope, project, err := s.integrationTarget(r)
	if err != nil {
		return nil, err
	}
	changes, err := integrate.Remove(req.Harness, scope, integrate.Options{ProjectDir: project})
	if err != nil {
		return nil, err
	}
	path, backup, changed := firstChange(changes)
	h, _ := integrate.Lookup(req.Harness)
	return map[string]any{"changes": changes, "path": path, "backup": backup, "changed": changed, "restartHint": h.RestartHint}, nil
}

func (s *Server) intSnippet(w http.ResponseWriter, r *http.Request) (any, error) {
	req, scope, project, err := s.integrationTarget(r)
	if err != nil {
		return nil, err
	}
	return integrate.SnippetFor(req.Harness, scope, integrate.Options{ProjectDir: project})
}

func (s *Server) intVerify(w http.ResponseWriter, r *http.Request) (any, error) {
	req, scope, project, err := s.integrationTarget(r)
	if err != nil {
		return nil, err
	}
	checks, err := integrate.Verify(r.Context(), req.Harness, scope, integrate.Options{ProjectDir: project})
	if err != nil {
		return nil, err
	}
	ok := len(checks) > 0
	for _, c := range checks {
		if !c.OK {
			ok = false
		}
	}
	return map[string]any{"ok": ok, "checks": checks}, nil
}

func (s *Server) intListSkills(w http.ResponseWriter, r *http.Request) (any, error) {
	return map[string]any{"skills": integrate.EmbeddedSkills()}, nil
}

func (s *Server) intSkillStatus(w http.ResponseWriter, r *http.Request) (any, error) {
	req, scope, project, err := s.integrationTarget(r)
	if err != nil {
		return nil, err
	}
	states, err := integrate.SkillStatus(req.Harness, scope, project)
	if err != nil {
		return nil, err
	}
	return map[string]any{"skills": states}, nil
}

func (s *Server) intInstallSkills(w http.ResponseWriter, r *http.Request) (any, error) {
	req, scope, project, err := s.integrationTarget(r)
	if err != nil {
		return nil, err
	}
	states, err := integrate.InstallSkills(req.Harness, scope, project)
	if err != nil {
		return nil, err
	}
	return map[string]any{"skills": states}, nil
}

func (s *Server) intRemoveSkills(w http.ResponseWriter, r *http.Request) (any, error) {
	req, scope, project, err := s.integrationTarget(r)
	if err != nil {
		return nil, err
	}
	dirs, err := integrate.RemoveSkills(req.Harness, scope, project)
	if err != nil {
		return nil, err
	}
	return map[string]any{"removed": dirs}, nil
}

func (s *Server) intCheckUpdates(w http.ResponseWriter, r *http.Request) (any, error) {
	_, scope, project, err := s.integrationTarget(r)
	if err != nil {
		return nil, err
	}
	return map[string]any{"updates": integrate.CheckSkillUpdates(scope, project), "at": time.Now().Unix()}, nil
}
