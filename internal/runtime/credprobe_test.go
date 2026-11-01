package runtime_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"jack-ketch/internal/credentials"
	"jack-ketch/internal/runtime"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

type credEnv struct {
	rt      *runtime.Runtime
	svc     *credentials.Service
	project string
}

func newCredEnv(t *testing.T, rt *runtime.Runtime) *credEnv {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p, err := db.CreateProject("proj")
	if err != nil {
		t.Fatal(err)
	}
	keys := credentials.NewKeyManager(dir, db)
	keys.UseKeychain(false)
	svc := credentials.NewService(db, keys)
	rt.SetCredentials(svc.Scope(p.ID))
	return &credEnv{rt: rt, svc: svc, project: p.ID}
}

func (e *credEnv) add(t *testing.T, req api.UpsertCredentialRequest) {
	t.Helper()
	if _, err := e.svc.Upsert(e.project, req); err != nil {
		t.Fatal(err)
	}
}

func echoServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"auth": r.Header.Get("Authorization"), "tenant": r.Header.Get("X-Tenant-Id"),
			"cookie": r.Header.Get("Cookie"), "path": r.URL.RequestURI(), "body": string(body),
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func adminProfile() api.UpsertCredentialRequest {
	return api.UpsertCredentialRequest{
		Name: "admin", Default: true, Environment: "test",
		Fields: []api.CredentialField{
			{Name: "token", Kind: credentials.KindBearer, Value: "tok-abcdef-123456", Secret: true},
			{Name: "tenant", Kind: credentials.KindHeader, Target: "X-Tenant-Id", Value: "tenant-a"},
			{Name: "sid", Kind: credentials.KindCookie, Target: "sid", Value: "cookie-secret-9876", Secret: true},
		},
	}
}

func TestProbeAttachesDefaultCredentialWithoutLeakingIt(t *testing.T) {
	rt := newRuntime(t)
	env := newCredEnv(t, rt)
	env.add(t, adminProfile())
	srv := echoServer(t, 200)
	id := startHTTP(t, rt)

	res, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/echo?token=tok-abcdef-123456"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	for _, secret := range []string{"tok-abcdef-123456", "cookie-secret-9876"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("secret %q leaked in the probe result: %s", secret, raw)
		}
	}
	if !res.OK || res.Credential != "admin" || res.CredentialStatus != credentials.StatusOK {
		t.Fatalf("result: %+v", res)
	}
	if !strings.Contains(res.Body, "[credential:admin.token]") || !strings.Contains(res.Body, "tenant-a") {
		t.Fatalf("the server must have received the credential: %s", res.Body)
	}
	if res.Check == nil || res.Check.Credential != "" {
		t.Fatalf("a default profile is not pinned into saved checks: %+v", res.Check)
	}
	explicit, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/echo", Credential: "admin"})
	if err != nil || explicit.Check == nil || explicit.Check.Credential != "admin" {
		t.Fatalf("explicit credential is pinned: %+v %v", explicit, err)
	}
	none, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/echo", Credential: "none"})
	if err != nil || strings.Contains(none.Body, "tenant-a") || none.Credential != "" {
		t.Fatalf("none must send no credentials: %+v %v", none, err)
	}
}

func TestProbeScrubsSecretsEchoedInProcessLogs(t *testing.T) {
	rt := newRuntime(t)
	env := newCredEnv(t, rt)
	env.add(t, adminProfile())
	id := startHTTP(t, rt)
	var res *api.ProbeResult
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: "/leak-tok-abcdef-123456", Credential: "admin"})
		if err == nil && r.Status != 0 {
			res = r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if res == nil {
		t.Fatal("probe never reached the server")
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), "tok-abcdef-123456") {
		t.Fatalf("secret in url, body or logs: %s", raw)
	}
	found := false
	for _, l := range res.Logs {
		if strings.Contains(l.Line, "[credential:admin.token]") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a scrubbed log line: %+v", res.Logs)
	}
}

func TestProbePlaceholdersAndErrors(t *testing.T) {
	rt := newRuntime(t)
	env := newCredEnv(t, rt)
	req := adminProfile()
	req.Fields = append(req.Fields, api.CredentialField{Name: "pw", Kind: credentials.KindVariable, Value: `pa"ss`, Secret: true})
	req.Apps = []string{"web"}
	env.add(t, req)
	srv := echoServer(t, 200)
	id := startHTTP(t, rt)

	res, err := rt.Probe(context.Background(), api.ProbeRequest{
		ProcessID: id, Credential: "admin", Method: "POST", Confirmed: true,
		URL: srv.URL + "/t/${cred:tenant}", Headers: map[string]string{"X-Copy": "${cred:tenant}"}, Body: `{"pw":"${cred:pw}"}`,
	})
	if err == nil {
		t.Fatalf("profile restricted to app web must reject a raw-command process: %+v", res)
	}

	env2 := newCredEnv(t, rt)
	req.Apps = nil
	env2.add(t, req)
	res, err = rt.Probe(context.Background(), api.ProbeRequest{
		ProcessID: id, Credential: "admin", Method: "POST", Confirmed: true,
		URL: srv.URL + "/t/${cred:tenant}", Body: `{"pw":"${cred:pw}"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Body, "/t/tenant-a") {
		t.Fatalf("url placeholder: %s", res.Body)
	}
	if strings.Contains(res.Body, "pa\\\"ss") || strings.Contains(res.Body, `pa"ss`) {
		t.Fatalf("body secret must be scrubbed: %s", res.Body)
	}
	if !strings.Contains(res.Body, "[credential:admin.pw]") {
		t.Fatalf("the server received the escaped body value: %s", res.Body)
	}
	if _, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/${cred:nope}", Credential: "admin"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown placeholder: %v", err)
	}
	if _, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/x", Credential: "ghost"}); err == nil || !strings.Contains(err.Error(), "admin") {
		t.Fatalf("unknown profile lists the known ones: %v", err)
	}
	if _, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/${cred:tenant}", Credential: "none"}); err == nil {
		t.Fatal("a placeholder without an active profile must fail")
	}
	if _, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, TCP: "127.0.0.1:1", Credential: "admin"}); err == nil {
		t.Fatal("credentials do not apply to tcp probes")
	}
}

func TestProbeReportsRejectedCredentialForTheUser(t *testing.T) {
	rt := newRuntime(t)
	env := newCredEnv(t, rt)
	env.add(t, adminProfile())
	srv := echoServer(t, 401)
	id := startHTTP(t, rt)
	res, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/me"})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.CredentialStatus != "rejected" || !strings.Contains(res.CredentialHint, "Credentials page") || strings.Contains(res.CredentialHint, "tok-abcdef") {
		t.Fatalf("rejection: %+v", res)
	}
}

func TestProbeReportsExpiredTokenWithoutRecipe(t *testing.T) {
	rt := newRuntime(t)
	env := newCredEnv(t, rt)
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	expired := enc(map[string]any{"alg": "none"}) + "." + enc(map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}) + ".sig-sig-sig"
	env.add(t, api.UpsertCredentialRequest{Name: "old", Default: true, Fields: []api.CredentialField{{Name: "t", Kind: credentials.KindBearer, Value: expired, Secret: true}}})
	srv := echoServer(t, 200)
	id := startHTTP(t, rt)
	res, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/me"})
	if err != nil {
		t.Fatal(err)
	}
	if res.CredentialStatus != credentials.StatusExpired || !strings.Contains(res.CredentialHint, "expired") {
		t.Fatalf("expired token must be flagged: %+v", res)
	}
}

func TestProbeRefreshesViaLoginRecipe(t *testing.T) {
	rt := newRuntime(t)
	env := newCredEnv(t, rt)
	var logins atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			logins.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": fmt.Sprintf("minted-token-%d", logins.Load())})
		default:
			if r.Header.Get("Authorization") == "Bearer minted-token-1" {
				_, _ = io.WriteString(w, "welcome")
				return
			}
			http.Error(w, "no", http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	env.add(t, api.UpsertCredentialRequest{
		Name: "login", Default: true,
		Fields: []api.CredentialField{{Name: "pass", Kind: credentials.KindVariable, Value: "hunter2hunter2", Secret: true}},
		Recipe: &api.CredentialRecipe{Method: "POST", URL: "/login", Body: `{"p":"${cred:pass}"}`, Extract: []api.CredentialExtract{{Source: "json", Path: "token", Field: "token"}}},
	})
	id := startHTTP(t, rt)
	res, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/me"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Body != "welcome" || !res.CredentialRefreshed || logins.Load() != 1 {
		t.Fatalf("first call logs in on demand: %+v logins=%d", res, logins.Load())
	}
	again, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/me"})
	if err != nil || !again.OK || again.CredentialRefreshed || logins.Load() != 1 {
		t.Fatalf("a fresh token is reused: %+v logins=%d", again, logins.Load())
	}
}

func TestProbeRetriesOnceAfter401WithRecipe(t *testing.T) {
	rt := newRuntime(t)
	env := newCredEnv(t, rt)
	var logins atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			logins.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "second-token-value", "ttl": 300})
			return
		}
		if r.Header.Get("Authorization") == "Bearer second-token-value" {
			_, _ = io.WriteString(w, "ok")
			return
		}
		http.Error(w, "stale", http.StatusUnauthorized)
	}))
	defer srv.Close()
	env.add(t, api.UpsertCredentialRequest{
		Name: "login", Default: true,
		Fields: []api.CredentialField{
			{Name: "pass", Kind: credentials.KindVariable, Value: "hunter2hunter2", Secret: true},
			{Name: "token", Kind: credentials.KindBearer, Value: "first-token-value", Secret: true, ExpiresAt: time.Now().Add(time.Hour).Unix()},
		},
		Recipe: &api.CredentialRecipe{URL: "/login", Body: `{}`, Extract: []api.CredentialExtract{{Source: "json", Path: "token", Field: "token", ExpiresInPath: "ttl"}}},
	})
	id := startHTTP(t, rt)
	res, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/me"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Body != "ok" || !res.CredentialRefreshed || logins.Load() != 1 {
		t.Fatalf("a 401 triggers one refresh and one retry: %+v logins=%d", res, logins.Load())
	}
}

func TestProductionProfileNeedsApprovalEvenOnLocalhost(t *testing.T) {
	rt := newRuntime(t)
	env := newCredEnv(t, rt)
	req := adminProfile()
	req.Name, req.Environment = "prodadmin", "production"
	env.add(t, req)
	srv := echoServer(t, 200)
	id := startHTTP(t, rt)
	_, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/x"})
	var ce *runtime.ConfirmError
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "production") {
		t.Fatalf("production profile must ask first: %v", err)
	}
	if res, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: srv.URL + "/x", Confirmed: true}); err != nil || !res.OK {
		t.Fatalf("confirmed: %+v %v", res, err)
	}
}

func TestCredentialedRemoteTargetNeedsApprovalAndNeverFromChecks(t *testing.T) {
	rt := newRuntime(t)
	env := newCredEnv(t, rt)
	env.add(t, adminProfile())
	id := startHTTP(t, rt)
	_, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: "http://example.com/api"})
	var ce *runtime.ConfirmError
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "admin") {
		t.Fatalf("remote target names the credential that would be sent: %v", err)
	}
}

func TestRunChecksUsesCheckCredential(t *testing.T) {
	dir := t.TempDir()
	rt := loadYAML(t, dir, `
apps:
  web:
    command: ["`+helperPath+`", "http-server", "200"]
    checks:
      - name: hello
        url: /hello
        credential: admin
      - name: remote
        url: http://example.com/x
        credential: admin
      - name: anon
        url: /hello
        credential: none
`)
	env := newCredEnv(t, rt)
	env.add(t, adminProfile())
	res, err := rt.Start(context.Background(), api.StartRequest{App: "web"})
	if err != nil {
		t.Fatal(err)
	}
	firstLine(t, rt, res.ProcessID, "LISTENING")
	time.Sleep(100 * time.Millisecond)
	out, err := rt.RunChecks(context.Background(), api.RunChecksRequest{App: "web"})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]api.ProbeResult{}
	for _, r := range out.Results {
		byName[r.Name] = r
	}
	if r := byName["hello"]; !r.OK || r.Credential != "admin" {
		t.Fatalf("credentialed check: %+v", r)
	}
	if r := byName["anon"]; !r.OK || r.Credential != "" {
		t.Fatalf("credential: none disables the default: %+v", r)
	}
	if r := byName["remote"]; r.OK || !strings.Contains(strings.Join(r.Failures, ";"), "saved check") {
		t.Fatalf("a saved check never sends credentials off this machine: %+v", r)
	}
}

func TestCredentialsUnavailableWithoutDaemonScope(t *testing.T) {
	rt := newRuntime(t)
	id := startHTTP(t, rt)
	if _, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: "/hello", Credential: "admin"}); err == nil {
		t.Fatal("explicit credential without a credential store must fail")
	}
	if res, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: "/hello"}); err != nil || res == nil {
		t.Fatalf("plain probes keep working: %v", err)
	}
}

func TestCheckRevealFollowsSecurityPolicy(t *testing.T) {
	trusted := newRuntime(t)
	if err := trusted.CheckReveal("reveal_credential", map[string]any{"id": "x"}); err != nil {
		t.Fatalf("trusted mode allows reveal: %v", err)
	}
	restricted := loadYAML(t, t.TempDir(), `
runtime:
  security:
    mode: restricted
    allowed_commands: [echo]
    reveal_secrets: false
`)
	err := restricted.CheckReveal("reveal_credential", map[string]any{"id": "x"})
	if err == nil || !strings.Contains(err.Error(), "reveal") {
		t.Fatalf("restricted mode must deny reveal: %v", err)
	}
}
