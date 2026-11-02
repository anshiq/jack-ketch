package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jack-ketch/internal/core"
	"jack-ketch/pkg/api"
	"jack-ketch/pkg/client"
)

type credHarness struct {
	t     *testing.T
	eng   *core.Engine
	cl    *client.Client
	web   string
	token string
	ws    string
	dir   string
}

func newCredHarness(t *testing.T) *credHarness {
	t.Helper()
	eng, done := testEngine(t)
	t.Cleanup(done)
	eng.Credentials().Keys().UseKeychain(false)
	srv, err := New(eng, sockPath(t, eng)+".tcp", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	free, err := freeLoopbackAddr()
	if err != nil {
		t.Fatal(err)
	}
	token := "cred-test-token-0123456789"
	go func() { _ = srv.ServeTCP(free, token) }()
	waitForTCP(t, free)
	cl := client.New(sockPath(t, eng), "test/agent")
	dir := t.TempDir()
	res, err := cl.ProjectService().Resolve(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return &credHarness{t: t, eng: eng, cl: cl, web: free, token: token, ws: res.WorkspaceID, dir: dir}
}

func (h *credHarness) webCall(method string, body map[string]any) (map[string]any, int) {
	h.t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/jackketch.v1.CredentialService/%s", h.web, method), bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+h.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out, resp.StatusCode
}

func (h *credHarness) udsCall(method string, body map[string]any) (map[string]any, error) {
	var out map[string]any
	err := h.cl.Call(context.Background(), "CredentialService", method, body, &out)
	return out, err
}

func (h *credHarness) saveProfile(profile map[string]any) map[string]any {
	h.t.Helper()
	out, code := h.webCall("Upsert", map[string]any{"workspaceId": h.ws, "profile": profile})
	if code != http.StatusOK {
		h.t.Fatalf("upsert: %d %v", code, out)
	}
	return out
}

func adminBody() map[string]any {
	return map[string]any{
		"name": "admin", "default": true, "environment": "test",
		"fields": []map[string]any{
			{"name": "token", "kind": "bearer", "value": "tok-abcdef-123456", "secret": true},
			{"name": "tenant", "kind": "header", "target": "X-Tenant-Id", "value": "tenant-a"},
		},
	}
}

func TestCredentialMutationsAreWebOnly(t *testing.T) {
	h := newCredHarness(t)
	saved := h.saveProfile(adminBody())
	id, _ := saved["id"].(string)
	if id == "" {
		t.Fatalf("no id: %v", saved)
	}
	for method, body := range map[string]map[string]any{
		"Upsert":      {"workspaceId": h.ws, "profile": adminBody()},
		"Delete":      {"workspaceId": h.ws, "id": id},
		"Reveal":      {"workspaceId": h.ws, "id": id, "field": "token"},
		"ClearJar":    {"workspaceId": h.ws, "id": id},
		"SetKeystore": {"backend": "file"},
		"ResetKey":    {"confirm": "reset"},
	} {
		if _, err := h.udsCall(method, body); err == nil || !strings.Contains(err.Error(), "403") {
			t.Fatalf("%s over the agent socket must be refused, got %v", method, err)
		}
	}
	list, err := h.udsCall("List", map[string]any{"workspaceId": h.ws})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "tok-abcdef-123456") {
		t.Fatalf("agent-visible list must never contain secrets: %s", raw)
	}
	if !strings.Contains(string(raw), "tenant-a") || !strings.Contains(string(raw), `"status":"ok"`) {
		t.Fatalf("non-secret values and status are visible: %s", raw)
	}
}

func TestCredentialRevealIsAuditedWithoutValue(t *testing.T) {
	h := newCredHarness(t)
	saved := h.saveProfile(adminBody())
	id := saved["id"].(string)
	out, code := h.webCall("Reveal", map[string]any{"workspaceId": h.ws, "id": id, "field": "token"})
	if code != http.StatusOK || out["value"] != "tok-abcdef-123456" {
		t.Fatalf("reveal: %d %v", code, out)
	}
	rows, err := h.eng.Store().ListAudit(h.ws, 50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.Action] = true
		if strings.Contains(r.Args, "tok-abcdef-123456") {
			t.Fatalf("audit leaked a secret: %s %s", r.Action, r.Args)
		}
	}
	if !seen["CredentialService/Upsert"] || !seen["CredentialService/Reveal"] {
		t.Fatalf("upsert and reveal must be audited: %v", seen)
	}
}

func TestCredentialValidationAndNotFound(t *testing.T) {
	h := newCredHarness(t)
	if out, code := h.webCall("Upsert", map[string]any{"workspaceId": h.ws, "profile": map[string]any{"name": "bad name"}}); code != http.StatusBadRequest {
		t.Fatalf("validation: %d %v", code, out)
	}
	if _, code := h.webCall("Delete", map[string]any{"workspaceId": h.ws, "id": "cred_nope"}); code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", code)
	}
	if _, code := h.webCall("List", map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("workspace required: %d", code)
	}
	if out, code := h.webCall("Delete", map[string]any{"workspaceId": h.ws, "id": h.saveProfile(adminBody())["id"]}); code != http.StatusOK || out["deleted"] != true {
		t.Fatalf("delete: %d %v", code, out)
	}
	empty, err := h.udsCall("List", map[string]any{"workspaceId": h.ws})
	if err != nil || empty["hint"] == nil {
		t.Fatalf("empty list explains how to add one: %v %v", empty, err)
	}
}

func TestProbeOverDaemonUsesDefaultProfileAndScrubs(t *testing.T) {
	h := newCredHarness(t)
	h.saveProfile(adminBody())
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"auth": r.Header.Get("Authorization"), "tenant": r.Header.Get("X-Tenant-Id")})
	}))
	defer echo.Close()
	ctx := context.Background()
	started, err := h.cl.ProcessService().Start(ctx, &client.StartRequest{WorkspaceID: h.ws, Command: []string{"sh", "-c", "sleep 30"}, WorkDir: h.dir})
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.cl.ProcessService().Probe(ctx, api.ProbeRequest{ProcessID: started.ProcessID, URL: echo.URL + "/me"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), "tok-abcdef-123456") {
		t.Fatalf("daemon probe leaked the token: %s", raw)
	}
	if res.Credential != "admin" || !strings.Contains(res.Body, "tenant-a") || !strings.Contains(res.Body, "[credential:admin.token]") {
		t.Fatalf("probe: %+v", res)
	}
	rows, _ := h.eng.Store().ListAudit(h.ws, 20)
	found := false
	for _, r := range rows {
		if r.Action == "ProcessService/Probe" && strings.Contains(r.Args, `"credential"`) && !strings.Contains(r.Args, "tok-abcdef") {
			found = true
		}
	}
	if !found {
		t.Fatalf("probe audit should record which profile was used: %+v", rows)
	}
}

func TestLogEndpointsRedactCredentialValues(t *testing.T) {
	h := newCredHarness(t)
	h.saveProfile(adminBody())
	ctx := context.Background()
	started, err := h.cl.ProcessService().Start(ctx, &client.StartRequest{WorkspaceID: h.ws, Command: []string{"sh", "-c", "echo token=tok-abcdef-123456; sleep 30"}, WorkDir: h.dir})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var lines string
	for time.Now().Before(deadline) {
		out, err := h.cl.LogService().GetLogs(ctx, started.ProcessID, 50)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(out)
		lines = string(b)
		if strings.Contains(lines, "token=") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(lines, "token=***") || strings.Contains(lines, "tok-abcdef-123456") {
		t.Fatalf("get_logs must mask credential values: %s", lines)
	}
	var wait map[string]any
	if err := h.cl.Call(ctx, "LogService", "WaitForLog", map[string]any{"process_id": started.ProcessID, "contains": "token=", "timeout_ms": 3000}, &wait); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(wait)
	if strings.Contains(string(b), "tok-abcdef-123456") {
		t.Fatalf("wait_for_log must mask credential values: %s", b)
	}
	var diag map[string]any
	if err := h.cl.Call(ctx, "ProcessService", "Diagnose", map[string]any{"processId": started.ProcessID}, &diag); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(diag)
	if strings.Contains(string(b), "tok-abcdef-123456") {
		t.Fatalf("diagnose must mask credential values: %s", b)
	}
}

func TestCredentialProxyLifecycleOverRPC(t *testing.T) {
	h := newCredHarness(t)
	h.saveProfile(adminBody())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"auth": r.Header.Get("Authorization")})
	}))
	defer up.Close()
	body := map[string]any{"workspaceId": h.ws, "profile": "admin", "upstream": up.URL, "ttl_minutes": 5, "label": "e2e"}
	gate, err := h.udsCall("StartProxy", body)
	if err != nil {
		t.Fatal(err)
	}
	if gate["needsConfirmation"] != true {
		t.Fatalf("starting a proxy needs approval: %v", gate)
	}
	body["confirmed"] = true
	got, err := h.udsCall("StartProxy", body)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, _ := got["url"].(string)
	if proxyURL == "" || got["profile"] != "admin" {
		t.Fatalf("proxy: %v", got)
	}
	resp, err := http.Get(proxyURL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(data), "tok-abcdef-123456") || !strings.Contains(string(data), "[credential:admin.token]") {
		t.Fatalf("proxied response: %s", data)
	}
	list, err := h.udsCall("ListProxies", map[string]any{"workspaceId": h.ws})
	if err != nil || len(list["proxies"].([]any)) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	if _, err := h.udsCall("StopProxy", map[string]any{"workspaceId": h.ws, "id": got["id"]}); err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get(proxyURL + "/x"); err == nil {
		t.Fatal("stopped proxy must refuse connections")
	}
	if _, err := h.udsCall("StopProxy", map[string]any{"workspaceId": h.ws, "id": "px_missing"}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("stop missing: %v", err)
	}
	bad := map[string]any{"workspaceId": h.ws, "profile": "ghost", "upstream": up.URL, "confirmed": true}
	if _, err := h.udsCall("StartProxy", bad); err == nil {
		t.Fatal("unknown profile")
	}
}

func TestCredentialRefreshRPCRunsRecipe(t *testing.T) {
	h := newCredHarness(t)
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "minted-by-recipe"})
	}))
	defer login.Close()
	saved := h.saveProfile(map[string]any{
		"name": "login", "default": true,
		"fields": []map[string]any{{"name": "pass", "kind": "variable", "secret": true, "value": "hunter2hunter2"}},
		"recipe": map[string]any{"method": "POST", "url": "/login", "body": `{"p":"${cred:pass}"}`, "extract": []map[string]any{{"source": "json", "path": "token", "field": "token"}}},
	})
	if saved["status"] != "empty" {
		t.Fatalf("a profile that needs a login starts empty: %v", saved["status"])
	}
	out, err := h.udsCall("Refresh", map[string]any{"workspaceId": h.ws, "id": saved["id"], "upstream": login.URL})
	if err != nil {
		t.Fatal(err)
	}
	if out["status"] != "ok" {
		t.Fatalf("after the recipe the profile is ready: %v", out)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "minted-by-recipe") {
		t.Fatalf("refresh result leaked the token: %s", raw)
	}
	if _, err := h.udsCall("Refresh", map[string]any{"workspaceId": h.ws, "id": saved["id"]}); err == nil {
		t.Fatal("refresh needs a process or an upstream")
	}
}

func TestKeystoreEndpoints(t *testing.T) {
	h := newCredHarness(t)
	st, err := h.udsCall("GetKeystore", map[string]any{})
	if err != nil || st["backend"] == nil {
		t.Fatalf("status: %v %v", st, err)
	}
	if out, code := h.webCall("SetKeystore", map[string]any{"backend": "nonsense"}); code != http.StatusBadRequest {
		t.Fatalf("bad backend: %d %v", code, out)
	}
	h.saveProfile(adminBody())
	if out, code := h.webCall("SetKeystore", map[string]any{"backend": "file"}); code != http.StatusOK || out["active"] != "file" || out["backend"] != "file" {
		t.Fatalf("set file: %d %v", code, out)
	}
	if out, code := h.webCall("SetKeystore", map[string]any{"backend": "keychain"}); code != http.StatusBadRequest {
		t.Fatalf("keychain is disabled in tests: %d %v", code, out)
	}
	if _, code := h.webCall("ResetKey", map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("reset needs confirmation: %d", code)
	}
	out, code := h.webCall("ResetKey", map[string]any{"confirm": "reset"})
	if code != http.StatusOK || out["profilesCleared"] != float64(1) {
		t.Fatalf("reset: %d %v", code, out)
	}
	list, _ := h.udsCall("List", map[string]any{"workspaceId": h.ws})
	raw, _ := json.Marshal(list)
	if !strings.Contains(string(raw), `"status":"empty"`) {
		t.Fatalf("profiles need re-entry after a reset: %s", raw)
	}
}
