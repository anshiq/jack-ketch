package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

type auditScope struct {
	mu          sync.Mutex
	workspaceID string
	projectID   string
	args        string
}

type auditScopeKey struct{}

func withAuditScope(r *http.Request) (*http.Request, *auditScope) {
	sc := &auditScope{}
	return r.WithContext(context.WithValue(r.Context(), auditScopeKey{}, sc)), sc
}

func scopeOf(r *http.Request) *auditScope {
	sc, _ := r.Context().Value(auditScopeKey{}).(*auditScope)
	return sc
}

func noteScope(r *http.Request, workspaceID, projectID string) {
	sc := scopeOf(r)
	if sc == nil {
		return
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if workspaceID != "" {
		sc.workspaceID = workspaceID
	}
	if projectID != "" {
		sc.projectID = projectID
	}
}

func noteArgs(r *http.Request, v any) {
	sc := scopeOf(r)
	if sc == nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	if len(b) > 2048 {
		b = b[:2048]
	}
	sc.mu.Lock()
	sc.args = string(b)
	sc.mu.Unlock()
}

var auditedMethods = map[string]bool{
	"ProcessService/Start":             true,
	"ProcessService/Stop":              true,
	"ProcessService/Restart":           true,
	"ProcessService/Signal":            true,
	"ProcessService/SendStdin":         true,
	"ProcessService/Remove":            true,
	"ProcessService/SetRestartPolicy":  true,
	"ProcessService/OpenShell":         true,
	"ProcessService/StartStack":        true,
	"LogService/ClearLogs":             true,
	"ConfigService/Apply":              true,
	"ConfigService/Rollback":           true,
	"ConfigService/ResolveProposal":    true,
	"ConfigService/SaveRun":            true,
	"ConfigService/UpdateGitignore":    true,
	"ProcessService/Probe":             true,
	"ProcessService/RunChecks":         true,
	"ProjectService/UpdateProject":     true,
	"ProjectService/LinkWorkspace":     true,
	"ProjectService/ForgetProject":     true,
	"ProjectService/GC":                true,
	"IntegrationService/InstallMCP":    true,
	"IntegrationService/RemoveMCP":     true,
	"IntegrationService/InstallSkills": true,
	"IntegrationService/RemoveSkills":  true,
	"SystemService/UpdateSettings":     true,
	"SystemService/Shutdown":           true,
	"SystemService/EnableTCP":          true,
	"SessionService/Close":             true,
}

func auditMethod(path string) string {
	return strings.TrimPrefix(path, "/jackketch.v1.")
}
