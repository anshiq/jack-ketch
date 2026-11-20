package devproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type collector struct {
	mu    sync.Mutex
	lines []string
}

func (c *collector) sink(stream, line string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
	return nil
}

func startProxy(t *testing.T, h http.Handler) (*Manager, Proxy, *collector) {
	t.Helper()
	up := httptest.NewServer(h)
	t.Cleanup(up.Close)
	u, _ := url.Parse(up.URL)
	m := NewManager()
	t.Cleanup(m.StopAll)
	c := &collector{}
	p, err := m.Start(StartOptions{ProcessID: "p1", WorkspaceID: "ws", Upstream: u, Sink: c.sink})
	if err != nil {
		t.Fatal(err)
	}
	return m, p, c
}

func get(t *testing.T, u string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestInjectsScriptIntoHTMLOnly(t *testing.T) {
	_, p, _ := startProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><html><head><title>x</title></head><body>hi</body></html>`)
		case "/api":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "/go":
			http.Redirect(w, r, "http://"+r.Host+"/", http.StatusFound)
		}
	}))
	resp, body := get(t, p.URL+"/")
	if !strings.Contains(body, `<head><script src="/__jack-ketch/console.js"></script><title>`) {
		t.Fatalf("script not injected: %s", body)
	}
	if resp.ContentLength != int64(len(body)) {
		t.Fatalf("content-length %d != %d", resp.ContentLength, len(body))
	}
	if _, api := get(t, p.URL+"/api"); api != `{"ok":true}` {
		t.Fatalf("json body changed: %s", api)
	}
	_, js := get(t, p.URL+scriptPath)
	if !strings.Contains(js, "unhandledrejection") || strings.Contains(js, "__JK_WARN__") {
		t.Fatalf("bad script: %.80s", js)
	}
}

func TestReportsBecomeLogLinesWithLocalPaths(t *testing.T) {
	_, p, c := startProxy(t, http.NotFoundHandler())
	body := `[{"kind":"exception","level":"error","message":"TypeError: Cannot read properties of undefined (reading 'map')","stack":"TypeError: Cannot read properties of undefined (reading 'map')\n    at List (http://127.0.0.1:5173/src/components/List.tsx?t=171:14:22)\n    at renderWithHooks (http://127.0.0.1:5173/node_modules/.vite/deps/react-dom.js?v=1:11548:26)","source":"http://127.0.0.1:5173/src/components/List.tsx?t=171:14:22","page":"/items"},
	{"kind":"console","level":"error","message":"boom","stack":"fn@http://localhost:3000/@fs/home/me/app/src/x.js:3:9\n@http://localhost:3000/src/y.js:1:1"}]`
	req, _ := http.NewRequest(http.MethodPost, p.URL+reportPath, strings.NewReader(body))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("report: %v %v", resp, err)
	}
	got := strings.Join(c.lines, "\n")
	for _, want := range []string{
		"[browser] error: TypeError: Cannot read properties of undefined (reading 'map') (at src/components/List.tsx:14:22) page=/items",
		"    at List (src/components/List.tsx:14:22)",
		"    at renderWithHooks (node_modules/.vite/deps/react-dom.js:11548:26)",
		"[browser] error: boom",
		"    at fn (/home/me/app/src/x.js:3:9)",
		"    at <anonymous> (src/y.js:1:1)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	cross, _ := http.NewRequest(http.MethodPost, p.URL+reportPath, strings.NewReader(`[]`))
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	if r, _ := http.DefaultClient.Do(cross); r == nil || r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site report accepted: %v", r)
	}
}

func TestStartReusesExistingProxyForSameProcess(t *testing.T) {
	m, p, c := startProxy(t, http.NotFoundHandler())
	u, _ := url.Parse(p.Upstream)
	again, err := m.Start(StartOptions{ProcessID: "p1", WorkspaceID: "ws", Upstream: u, Sink: c.sink})
	if err != nil || again.ID != p.ID {
		t.Fatalf("expected reuse, got %+v %v", again, err)
	}
	if err := m.Stop("ws", p.ID); err != nil {
		t.Fatal(err)
	}
	if len(m.List("ws")) != 0 {
		t.Fatal("proxy not stopped")
	}
}
