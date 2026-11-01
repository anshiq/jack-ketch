package credentials

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"jack-ketch/pkg/api"
)

func proxyUpstream(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			hits.Add(1)
			_, _ = io.WriteString(w, `{"data":{"access_token":"access-token-`+string(rune('0'+hits.Load()))+`","expires_in":600}}`)
		case "/echo":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "server-session-xyz", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Location", "http://"+r.Host+"/next")
			_, _ = io.WriteString(w, `{"auth":"`+r.Header.Get("Authorization")+`","tenant":"`+r.Header.Get("X-Tenant-Id")+`","cookie":"`+r.Header.Get("Cookie")+`","host":"`+r.Host+`","enc":"`+r.Header.Get("Accept-Encoding")+`"}`)
		case "/bin":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("raw-binary-tok-abcdef-123456"))
		case "/stale":
			if r.Header.Get("Authorization") == "Bearer access-token-2" {
				_, _ = io.WriteString(w, "fresh")
				return
			}
			http.Error(w, "expired", http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
}

func startProxy(t *testing.T, svc *Service, project, profile string, upstream string) (api.CredentialProxy, *ProxyManager) {
	t.Helper()
	pm := NewProxyManager(svc)
	t.Cleanup(pm.StopAll)
	u, _ := url.Parse(upstream)
	info, err := pm.Start(project, profile, u, time.Minute, "test")
	if err != nil {
		t.Fatal(err)
	}
	return info, pm
}

func get(t *testing.T, rawURL string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", rawURL, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestProxyInjectsCredentialsAndScrubsResponses(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	var hits atomic.Int32
	up := proxyUpstream(t, &hits)
	defer up.Close()
	if _, err := svc.Upsert(project, basicProfile("tok-abcdef-123456")); err != nil {
		t.Fatal(err)
	}
	info, _ := startProxy(t, svc, project, "admin", up.URL)
	resp, body := get(t, info.URL+"/echo?x=1", map[string]string{"Authorization": "Bearer agent-fake", "Cookie": "theme=dark"})
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	for _, leaked := range []string{"tok-abcdef-123456", "cookie-secret-9876"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("secret %q leaked through the proxy: %s", leaked, body)
		}
	}
	if !strings.Contains(body, "[credential:admin.token]") || !strings.Contains(body, "tenant-a") || !strings.Contains(body, "theme=dark") {
		t.Fatalf("body: %s", body)
	}
	if strings.Contains(body, "agent-fake") {
		t.Fatal("client supplied authorization must be replaced")
	}
	if !strings.Contains(body, `"enc":"identity"`) {
		t.Fatalf("upstream must not compress: %s", body)
	}
	if got := resp.Header.Get("Set-Cookie"); got != "" {
		t.Fatalf("Set-Cookie must stay inside jack-ketch: %q", got)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, info.URL) {
		t.Fatalf("redirects must stay on the proxy: %q", loc)
	}
	if resp.Header.Get("X-Jack-Ketch-Credential") != "admin" {
		t.Fatal("proxy names the credential it used")
	}
	_, body2 := get(t, info.URL+"/echo", nil)
	if !strings.Contains(body2, "sid=[credential:admin.cookie.sid]") {
		t.Fatalf("captured session cookie should be replayed and scrubbed: %s", body2)
	}
	if strings.Contains(body2, "server-session-xyz") {
		t.Fatal("jar value leaked")
	}
}

func TestProxyBinaryPassesThroughUntouched(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	var hits atomic.Int32
	up := proxyUpstream(t, &hits)
	defer up.Close()
	_, _ = svc.Upsert(project, basicProfile("tok-abcdef-123456"))
	info, _ := startProxy(t, svc, project, "admin", up.URL)
	_, body := get(t, info.URL+"/bin", nil)
	if body != "raw-binary-tok-abcdef-123456" {
		t.Fatalf("binary bodies are not rewritten: %q", body)
	}
}

func TestProxyRefreshesOn401(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	var hits atomic.Int32
	up := proxyUpstream(t, &hits)
	defer up.Close()
	req := recipeProfile()
	req.Fields = append(req.Fields, api.CredentialField{Name: "token", Kind: KindBearer, Value: "stale-token-value", Secret: true})
	if _, err := svc.Upsert(project, req); err != nil {
		t.Fatal(err)
	}
	info, pm := startProxy(t, svc, project, "login", up.URL)
	hits.Store(1)
	resp, body := get(t, info.URL+"/stale", nil)
	if resp.StatusCode != 200 || body != "fresh" {
		t.Fatalf("expected a transparent refresh and retry: %d %q", resp.StatusCode, body)
	}
	list := pm.List(project)
	if len(list) != 1 || list[0].Refreshes != 1 || list[0].Requests != 1 || list[0].LastStatus != 200 {
		t.Fatalf("stats: %+v", list)
	}
}

func TestProxyGuards(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	var hits atomic.Int32
	up := proxyUpstream(t, &hits)
	defer up.Close()
	_, _ = svc.Upsert(project, basicProfile("tok-abcdef-123456"))
	info, pm := startProxy(t, svc, project, "admin", up.URL)

	req, _ := http.NewRequest("GET", info.URL+"/echo", nil)
	req.Host = "evil.example"
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Host header must be refused: %v %v", resp, err)
	}
	if resp, _ := get(t, info.URL+"/echo", map[string]string{"Sec-Fetch-Site": "cross-site"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site must be refused: %d", resp.StatusCode)
	}
	if resp, _ := get(t, info.URL+"/echo", map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"}); resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("websocket: %d", resp.StatusCode)
	}
	host, _, _ := net.SplitHostPort(strings.TrimPrefix(info.URL, "http://"))
	if host != "127.0.0.1" {
		t.Fatalf("proxy must bind loopback, got %s", host)
	}
	if err := pm.Stop("another-project", info.ID); err == nil {
		t.Fatal("cannot stop another project's proxy")
	}
	if err := pm.Stop(project, info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get(info.URL + "/echo"); err == nil {
		t.Fatal("stopped proxy must not accept connections")
	}
	if len(pm.List(project)) != 0 {
		t.Fatal("list empty after stop")
	}
}

func TestProxyStartValidationAndTTL(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	_, _ = svc.Upsert(project, basicProfile("tok-abcdef-123456"))
	pm := NewProxyManager(svc)
	defer pm.StopAll()
	if _, err := pm.Start(project, "admin", &url.URL{Scheme: "ftp", Host: "x"}, 0, ""); err == nil {
		t.Fatal("bad scheme")
	}
	u, _ := url.Parse("http://127.0.0.1:1")
	if _, err := pm.Start(project, "ghost", u, 0, ""); err == nil {
		t.Fatal("unknown profile")
	}
	info, err := pm.Start(project, "admin", u, 100*time.Hour, "")
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(time.Unix(info.ExpiresAt, 0)) > MaxProxyTTL+time.Minute {
		t.Fatal("ttl must be capped")
	}
	short, err := pm.Start(project, "admin", u, 50*time.Millisecond, "")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, p := range pm.List(project) {
			if p.ID == short.ID {
				found = true
			}
		}
		if !found {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("proxy must expire at its ttl")
}

func TestScopeRefreshUsesDefaultClient(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	if err := svc.Scope(project).Refresh(context.Background(), "ghost", &url.URL{Scheme: "http", Host: "127.0.0.1:1"}); err == nil {
		t.Fatal("unknown profile")
	}
}
