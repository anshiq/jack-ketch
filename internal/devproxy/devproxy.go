package devproxy

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed console.js
var consoleJS string

const (
	DefaultTTL    = time.Hour
	MaxTTL        = 12 * time.Hour
	scriptPath    = "/__jack-ketch/console.js"
	reportPath    = "/__jack-ketch/console"
	maxHTML       = 16 << 20
	maxReport     = 256 << 10
	maxLineBytes  = 4000
	maxStackLines = 25
)

type Sink func(stream, line string) error

type Proxy struct {
	ID          string `json:"id"`
	ProcessID   string `json:"processId"`
	WorkspaceID string `json:"workspaceId"`
	Upstream    string `json:"upstream"`
	URL         string `json:"url"`
	Warnings    bool   `json:"warnings"`
	CreatedAt   int64  `json:"createdAt"`
	ExpiresAt   int64  `json:"expiresAt"`
	Requests    int64  `json:"requests"`
	Reports     int64  `json:"reports"`
	Pages       int64  `json:"pagesInstrumented"`
}

type Manager struct {
	mu    sync.Mutex
	items map[string]*item
}

type item struct {
	info     Proxy
	port     string
	upstream *url.URL
	srv      *http.Server
	timer    *time.Timer
	sink     Sink
	rp       *httputil.ReverseProxy
	script   []byte
	requests atomic.Int64
	reports  atomic.Int64
	pages    atomic.Int64
}

func NewManager() *Manager { return &Manager{items: map[string]*item{}} }

type StartOptions struct {
	ProcessID   string
	WorkspaceID string
	Upstream    *url.URL
	TTL         time.Duration
	Warnings    bool
	Sink        Sink
}

func newID() string {
	return "bp_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (m *Manager) Start(o StartOptions) (Proxy, error) {
	if o.Upstream == nil || o.Upstream.Host == "" || (o.Upstream.Scheme != "http" && o.Upstream.Scheme != "https") {
		return Proxy{}, errors.New("upstream must be an http(s) URL")
	}
	if o.Sink == nil {
		return Proxy{}, errors.New("no log sink")
	}
	for _, p := range m.List(o.WorkspaceID) {
		if p.ProcessID == o.ProcessID && p.Upstream == o.Upstream.String() && p.Warnings == o.Warnings {
			return p, nil
		}
	}
	ttl := o.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if ttl > MaxTTL {
		ttl = MaxTTL
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Proxy{}, err
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	up := *o.Upstream
	up.Path, up.RawQuery, up.Fragment = "", "", ""
	now := time.Now()
	it := &item{
		port:     port,
		upstream: &up,
		sink:     o.Sink,
		script:   []byte(strings.Replace(consoleJS, "__JK_WARN__", strconv.FormatBool(o.Warnings), 1)),
		info: Proxy{
			ID: newID(), ProcessID: o.ProcessID, WorkspaceID: o.WorkspaceID, Upstream: up.String(),
			URL: "http://127.0.0.1:" + port, Warnings: o.Warnings, CreatedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(),
		},
	}
	it.rp = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&up)
			pr.Out.Host = up.Host
			pr.Out.Header.Del("Accept-Encoding")
			pr.SetXForwarded()
		},
		ModifyResponse: it.inject,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			_ = it.sink("stderr", "[browser] error: dev server unreachable through the browser proxy: "+err.Error()+" ("+r.Method+" "+r.URL.Path+")")
			http.Error(w, "jack-ketch browser proxy: the dev server is not reachable ("+err.Error()+")", http.StatusBadGateway)
		},
		FlushInterval: -1,
	}
	it.srv = &http.Server{Handler: it, ReadHeaderTimeout: 10 * time.Second}
	id := it.info.ID
	it.timer = time.AfterFunc(ttl, func() { _ = m.stop(id) })
	m.mu.Lock()
	m.items[id] = it
	m.mu.Unlock()
	go func() { _ = it.srv.Serve(ln) }()
	return it.snapshot(), nil
}

func (it *item) snapshot() Proxy {
	p := it.info
	p.Requests = it.requests.Load()
	p.Reports = it.reports.Load()
	p.Pages = it.pages.Load()
	return p
}

func (m *Manager) List(workspaceID string) []Proxy {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Proxy{}
	for _, it := range m.items {
		if workspaceID == "" || it.info.WorkspaceID == workspaceID {
			out = append(out, it.snapshot())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

func (m *Manager) Stop(workspaceID, id string) error {
	m.mu.Lock()
	it, ok := m.items[id]
	m.mu.Unlock()
	if !ok || (workspaceID != "" && it.info.WorkspaceID != workspaceID) {
		return errors.New("no such browser proxy")
	}
	return m.stop(id)
}

func (m *Manager) StopProcess(processID string) {
	m.mu.Lock()
	var ids []string
	for id, it := range m.items {
		if it.info.ProcessID == processID {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.stop(id)
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.items))
	for id := range m.items {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.stop(id)
	}
}

func (m *Manager) stop(id string) error {
	m.mu.Lock()
	it, ok := m.items[id]
	delete(m.items, id)
	m.mu.Unlock()
	if !ok {
		return errors.New("no such browser proxy")
	}
	it.timer.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := it.srv.Shutdown(ctx); err != nil {
		_ = it.srv.Close()
	}
	return nil
}

func (it *item) hostOK(r *http.Request) bool {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		return false
	}
	return (host == "127.0.0.1" || host == "localhost" || host == "::1") && port == it.port
}

func (it *item) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !it.hostOK(r) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case scriptPath:
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(it.script)
		return
	case reportPath:
		it.receive(w, r)
		return
	}
	it.requests.Add(1)
	it.rp.ServeHTTP(w, r)
}

var headRe = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
var htmlRe = regexp.MustCompile(`(?i)<html(\s[^>]*)?>`)

func (it *item) inject(resp *http.Response) error {
	if loc := resp.Header.Get("Location"); loc != "" {
		origin := it.upstream.Scheme + "://" + it.upstream.Host
		if strings.HasPrefix(loc, origin) {
			resp.Header.Set("Location", "http://127.0.0.1:"+it.port+strings.TrimPrefix(loc, origin))
		}
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "text/html") || resp.Header.Get("Content-Encoding") != "" || resp.Request.Method == http.MethodHead {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTML+1))
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	if len(body) <= maxHTML {
		tag := []byte(`<script src="` + scriptPath + `"></script>`)
		switch {
		case headRe.Match(body):
			loc := headRe.FindIndex(body)
			body = append(body[:loc[1]:loc[1]], append(tag, body[loc[1]:]...)...)
			it.pages.Add(1)
		case htmlRe.Match(body):
			loc := htmlRe.FindIndex(body)
			body = append(body[:loc[1]:loc[1]], append(tag, body[loc[1]:]...)...)
			it.pages.Add(1)
		case bytes.Contains(bytes.ToLower(body[:min(len(body), 512)]), []byte("<!doctype html")):
			body = append(tag, body...)
			it.pages.Add(1)
		}
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.Header.Del("ETag")
	return nil
}

type report struct {
	Kind    string `json:"kind"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Stack   string `json:"stack"`
	Source  string `json:"source"`
	Page    string `json:"page"`
}

func (it *item) receive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxReport+1))
	if err != nil || len(data) > maxReport {
		http.Error(w, "report too large", http.StatusRequestEntityTooLarge)
		return
	}
	var batch []report
	if err := json.Unmarshal(data, &batch); err != nil {
		http.Error(w, "bad report", http.StatusBadRequest)
		return
	}
	if len(batch) > 100 {
		batch = batch[:100]
	}
	for _, rep := range batch {
		for _, line := range it.format(rep) {
			_ = it.sink("stderr", line)
		}
		it.reports.Add(1)
	}
	w.WriteHeader(http.StatusNoContent)
}

var urlRe = regexp.MustCompile(`https?://(?:127\.0\.0\.1|localhost|\[::1\]|0\.0\.0\.0)(?::\d+)?/((?:@fs)?/?[^\s?#):]*)(?:\?[^\s#):]*)?(?:#[^\s):]*)?`)

func (it *item) localize(s string) string {
	return urlRe.ReplaceAllStringFunc(s, func(m string) string {
		p := urlRe.FindStringSubmatch(m)[1]
		if rest, ok := strings.CutPrefix(p, "@fs"); ok {
			return rest
		}
		if p == "" {
			return "/"
		}
		return p
	})
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (it *item) format(rep report) []string {
	level := rep.Level
	if level != "warn" {
		level = "error"
	}
	msg := strings.ReplaceAll(clip(it.localize(rep.Message), maxLineBytes), "\n", " ⏎ ")
	head := "[browser] " + level + ": " + msg
	if rep.Kind == "exception" && rep.Source != "" {
		head += " (at " + it.localize(rep.Source) + ")"
	}
	if rep.Page != "" {
		head += " page=" + clip(rep.Page, 300)
	}
	lines := []string{head}
	if rep.Stack == "" {
		return lines
	}
	n := 0
	for raw := range strings.SplitSeq(it.localize(rep.Stack), "\n") {
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(msg, t) || strings.Contains(head, t) {
			continue
		}
		if !strings.HasPrefix(t, "at ") {
			if i := strings.Index(t, "@"); i >= 0 {
				fn := t[:i]
				if fn == "" {
					fn = "<anonymous>"
				}
				t = "at " + fn + " (" + t[i+1:] + ")"
			} else {
				continue
			}
		}
		lines = append(lines, "    "+clip(t, 500))
		if n++; n >= maxStackLines {
			break
		}
	}
	return lines
}
