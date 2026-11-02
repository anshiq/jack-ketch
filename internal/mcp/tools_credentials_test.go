package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/core"
	"jack-ketch/internal/server"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

func credentialBridge(t *testing.T) (*Bridge, *core.Engine) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	eng := core.NewWithOptions(db, core.Options{DataDir: dir})
	eng.Credentials().Keys().UseKeychain(false)
	sock := filepath.Join(dir, "jackketchd.sock")
	srv, err := server.New(eng, sock, "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()
	for i := 0; i < 100; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, err := DialBridge(sock, t.TempDir(), "test-harness", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = b.Close()
		cancel()
		_ = srv.Close()
		_ = eng.Close()
		_ = db.Close()
	})
	return b, eng
}

func seedProfile(t *testing.T, eng *core.Engine, b *Bridge) {
	t.Helper()
	_, err := eng.Credentials().Upsert(b.ProjectID(), api.UpsertCredentialRequest{
		Name: "admin", Default: true, Environment: "test",
		Fields: []api.CredentialField{
			{Name: "token", Kind: "bearer", Value: "tok-abcdef-123456", Secret: true},
			{Name: "tenant", Kind: "header", Target: "X-Tenant-Id", Value: "tenant-a"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestListCredentialsToolNeverReturnsSecrets(t *testing.T) {
	b, eng := credentialBridge(t)
	session := surfaceSession(t, NewServer(b, nil), nil, nil)

	empty, ok := surfaceCall(t, session, "list_credentials", map[string]any{})
	if !ok || empty["hint"] == nil {
		t.Fatalf("empty project should explain how to add a profile: %v", empty)
	}

	seedProfile(t, eng, b)
	out, ok := surfaceCall(t, session, "list_credentials", map[string]any{})
	if !ok {
		t.Fatal("list_credentials failed")
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "tok-abcdef-123456") {
		t.Fatalf("secret reached the agent: %s", raw)
	}
	profiles := out["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("profiles: %v", profiles)
	}
	p := profiles[0].(map[string]any)
	if p["name"] != "admin" || p["default"] != true || p["status"] != "ok" || p["environment"] != "test" {
		t.Fatalf("profile: %v", p)
	}
	var tenant, token map[string]any
	for _, f := range p["fields"].([]any) {
		m := f.(map[string]any)
		switch m["name"] {
		case "tenant":
			tenant = m
		case "token":
			token = m
		}
	}
	if tenant["value"] != "tenant-a" || token["value"] != nil && token["value"] != "" || token["has_value"] != true || token["secret"] != true {
		t.Fatalf("fields: tenant=%v token=%v", tenant, token)
	}
}

func TestProbeToolAttachesCredentialFromProfile(t *testing.T) {
	b, eng := credentialBridge(t)
	seedProfile(t, eng, b)
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"auth": r.Header.Get("Authorization"), "tenant": r.Header.Get("X-Tenant-Id")})
	}))
	defer echo.Close()
	session := surfaceSession(t, NewServer(b, nil), nil, nil)
	start, ok := surfaceCall(t, session, "start_process", map[string]any{"command": "sh", "args": []string{"-c", "sleep 30"}, "workdir": t.TempDir()})
	if !ok {
		t.Fatal("start failed")
	}
	out, ok := surfaceCall(t, session, "probe", map[string]any{"process_id": start["process_id"], "url": echo.URL + "/me"})
	if !ok {
		t.Fatal("probe failed")
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "tok-abcdef-123456") {
		t.Fatalf("probe leaked the token to the agent: %s", raw)
	}
	if out["credential"] != "admin" || !strings.Contains(out["body"].(string), "tenant-a") {
		t.Fatalf("probe = %v", out)
	}
	none, _ := surfaceCall(t, session, "probe", map[string]any{"process_id": start["process_id"], "url": echo.URL + "/me", "credential": "none"})
	if strings.Contains(none["body"].(string), "tenant-a") {
		t.Fatalf("credential=none must send no credentials: %v", none)
	}
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "probe", Arguments: map[string]any{"process_id": start["process_id"], "url": echo.URL + "/me", "credential": "ghost"}})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "admin") {
		t.Fatalf("unknown profile names the real ones: %+v %v", res, err)
	}
}

func TestCredentialProxyToolRequiresApproval(t *testing.T) {
	b, eng := credentialBridge(t)
	seedProfile(t, eng, b)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"auth": r.Header.Get("Authorization")})
	}))
	defer up.Close()

	plain := surfaceSession(t, NewServer(b, nil), nil, nil)
	res, err := plain.CallTool(context.Background(), &mcp.CallToolParams{Name: "credential_proxy", Arguments: map[string]any{"action": "start", "profile": "admin", "upstream": up.URL}})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "confirmed=true") {
		t.Fatalf("without elicitation the agent must be told to ask: %+v %v", res, err)
	}

	asked := 0
	declining := surfaceSession(t, NewServer(b, nil), nil, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			asked++
			if !strings.Contains(req.Params.Message, "admin") {
				t.Errorf("approval message should name the profile: %s", req.Params.Message)
			}
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approve": false}}, nil
		},
	})
	res, err = declining.CallTool(context.Background(), &mcp.CallToolParams{Name: "credential_proxy", Arguments: map[string]any{"action": "start", "profile": "admin", "upstream": up.URL}})
	if err != nil || !res.IsError || asked != 1 || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "declined") {
		t.Fatalf("declined: %+v %v asked=%d", res, err, asked)
	}

	approving := surfaceSession(t, NewServer(b, nil), nil, &mcp.ClientOptions{
		ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approve": true}}, nil
		},
	})
	out, ok := surfaceCall(t, approving, "credential_proxy", map[string]any{"action": "start", "profile": "admin", "upstream": up.URL, "ttl_minutes": 5})
	if !ok {
		t.Fatal("approved start failed")
	}
	proxy := out["proxy"].(map[string]any)
	proxyURL := proxy["url"].(string)
	if !strings.Contains(out["usage"].(string), proxyURL) {
		t.Fatalf("usage should tell the agent where to send requests: %v", out["usage"])
	}
	resp, err := http.Get(proxyURL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(body), "tok-abcdef-123456") || !strings.Contains(string(body), "[credential:admin.token]") {
		t.Fatalf("proxied body: %s", body)
	}
	listed, ok := surfaceCall(t, approving, "credential_proxy", map[string]any{"action": "list"})
	if !ok || len(listed["proxies"].([]any)) != 1 {
		t.Fatalf("list: %v", listed)
	}
	stopped, ok := surfaceCall(t, approving, "credential_proxy", map[string]any{"action": "stop", "id": proxy["id"]})
	if !ok || stopped["stopped"] != true || len(stopped["proxies"].([]any)) != 0 {
		t.Fatalf("stop: %v", stopped)
	}
	bad, err := approving.CallTool(context.Background(), &mcp.CallToolParams{Name: "credential_proxy", Arguments: map[string]any{"action": "explode"}})
	if err != nil || !bad.IsError {
		t.Fatalf("unknown action: %+v %v", bad, err)
	}
}
