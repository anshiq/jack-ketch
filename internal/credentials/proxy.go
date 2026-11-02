package credentials

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"jack-ketch/pkg/api"
)

const (
	DefaultProxyTTL   = 30 * time.Minute
	MaxProxyTTL       = 8 * time.Hour
	maxProxyReqBody   = 8 << 20
	maxProxyScrubBody = 16 << 20
)

type ProxyManager struct {
	svc   *Service
	mu    sync.Mutex
	items map[string]*proxyItem
}

type proxyItem struct {
	info      api.CredentialProxy
	projectID string
	upstream  *url.URL
	srv       *http.Server
	port      string
	timer     *time.Timer
	svc       *Service
	client    *http.Client
	requests  atomic.Int64
	refreshes atomic.Int64
	status    atomic.Int32
}

func NewProxyManager(svc *Service) *ProxyManager {
	return &ProxyManager{svc: svc, items: map[string]*proxyItem{}}
}

func (sc *Scope) ResolveFor(name string) (*Active, error) {
	l, err := sc.svc.Get(sc.projectID, name)
	if err != nil {
		return nil, err
	}
	return &Active{svc: sc.svc, projectID: sc.projectID, profile: l.Row.ID, explicit: true, loaded: l}, nil
}

func (m *ProxyManager) Start(projectID, profile string, upstream *url.URL, ttl time.Duration, label string) (api.CredentialProxy, error) {
	if upstream == nil || upstream.Host == "" || (upstream.Scheme != "http" && upstream.Scheme != "https") {
		return api.CredentialProxy{}, errors.New("upstream must be an http(s) URL")
	}
	act, err := m.svc.Scope(projectID).ResolveFor(profile)
	if err != nil {
		return api.CredentialProxy{}, err
	}
	if ttl <= 0 {
		ttl = DefaultProxyTTL
	}
	if ttl > MaxProxyTTL {
		ttl = MaxProxyTTL
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return api.CredentialProxy{}, err
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	now := time.Now()
	id := "px_" + strings.TrimPrefix(newID(), "cred_")[:12]
	base := *upstream
	base.Path = strings.TrimSuffix(base.Path, "/")
	base.RawQuery = ""
	base.Fragment = ""
	it := &proxyItem{
		projectID: projectID,
		upstream:  &base,
		port:      port,
		svc:       m.svc,
		client: &http.Client{
			Timeout:       90 * time.Second,
			Transport:     &http.Transport{DisableCompression: true, Proxy: nil, ResponseHeaderTimeout: 60 * time.Second},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		info: api.CredentialProxy{
			ID: id, Profile: act.Name(), Upstream: base.String(), URL: "http://127.0.0.1:" + port,
			Label: label, CreatedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(),
		},
	}
	it.srv = &http.Server{Handler: it, ReadHeaderTimeout: 10 * time.Second}
	it.timer = time.AfterFunc(ttl, func() { _ = m.stop(id) })
	m.mu.Lock()
	m.items[id] = it
	m.mu.Unlock()
	go func() { _ = it.srv.Serve(ln) }()
	return it.snapshot(), nil
}

func (it *proxyItem) snapshot() api.CredentialProxy {
	info := it.info
	info.Requests = it.requests.Load()
	info.Refreshes = it.refreshes.Load()
	info.LastStatus = int(it.status.Load())
	return info
}

func (m *ProxyManager) stop(id string) error {
	m.mu.Lock()
	it, ok := m.items[id]
	delete(m.items, id)
	m.mu.Unlock()
	if !ok {
		return errors.New("no such proxy")
	}
	it.timer.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := it.srv.Shutdown(ctx); err != nil {
		_ = it.srv.Close()
	}
	return nil
}

func (m *ProxyManager) Stop(projectID, id string) error {
	m.mu.Lock()
	it, ok := m.items[id]
	m.mu.Unlock()
	if !ok || it.projectID != projectID {
		return errors.New("no such proxy")
	}
	return m.stop(id)
}

func (m *ProxyManager) List(projectID string) []api.CredentialProxy {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []api.CredentialProxy{}
	for _, it := range m.items {
		if projectID == "" || it.projectID == projectID {
			out = append(out, it.snapshot())
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt < out[j-1].CreatedAt; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (m *ProxyManager) StopAll() {
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

var hopHeaders = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

func (it *proxyItem) hostOK(r *http.Request) bool {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		return false
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return false
	}
	return port == it.port
}

func textual(ct string) bool {
	ct = strings.ToLower(ct)
	if ct == "" {
		return true
	}
	for _, p := range []string{"text/", "application/json", "application/xml", "application/javascript", "application/x-www-form-urlencoded", "application/xhtml", "+json", "+xml", "application/graphql"} {
		if strings.Contains(ct, p) {
			return true
		}
	}
	return false
}

func (it *proxyItem) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !it.hostOK(r) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "cross-site requests are not allowed through the credential proxy", http.StatusForbidden)
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "the credential proxy does not support WebSocket upgrades", http.StatusNotImplemented)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxProxyReqBody+1))
	_ = r.Body.Close()
	if err != nil || len(body) > maxProxyReqBody {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	it.requests.Add(1)
	act, err := it.svc.Scope(it.projectID).ResolveFor(it.info.Profile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	target := *it.upstream
	target.Path = it.upstream.Path + r.URL.Path
	target.RawQuery = r.URL.RawQuery
	do := func(req *http.Request) (*http.Response, error) { return it.client.Do(req) }
	if act.NeedsRefresh() {
		if err := act.Refresh(r.Context(), it.upstream, do); err == nil {
			it.refreshes.Add(1)
		}
	}
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		out, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		for k, vs := range r.Header {
			for _, v := range vs {
				out.Header.Add(k, v)
			}
		}
		for _, h := range hopHeaders {
			out.Header.Del(h)
		}
		out.Header.Del("Origin")
		out.Header.Del("Referer")
		out.Header.Set("Accept-Encoding", "identity")
		out.Host = it.upstream.Host
		act.Apply(out)
		resp, err = it.client.Do(out)
		if err != nil {
			http.Error(w, act.Scrubber().Scrub("upstream request failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 && act.HasRecipe() {
			if err := act.Refresh(r.Context(), it.upstream, do); err == nil {
				it.refreshes.Add(1)
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
				_ = resp.Body.Close()
				continue
			}
		}
		break
	}
	defer resp.Body.Close()
	it.status.Store(int32(resp.StatusCode))
	act.Capture(it.upstream.Hostname(), resp.Cookies())
	it.relay(w, resp, act)
}

func (it *proxyItem) relay(w http.ResponseWriter, resp *http.Response, act *Active) {
	scrub := act.Scrubber()
	ct := resp.Header.Get("Content-Type")
	stream := strings.Contains(strings.ToLower(ct), "text/event-stream")
	proxyOrigin := "http://127.0.0.1:" + it.port
	upstreamOrigin := it.upstream.Scheme + "://" + it.upstream.Host
	hdr := w.Header()
	for k, vs := range resp.Header {
		if strings.EqualFold(k, "Set-Cookie") || strings.EqualFold(k, "Content-Length") {
			continue
		}
		skip := false
		for _, h := range hopHeaders {
			if strings.EqualFold(h, k) {
				skip = true
			}
		}
		if skip {
			continue
		}
		for _, v := range vs {
			v = scrub.Scrub(v)
			if strings.EqualFold(k, "Location") {
				v = strings.Replace(v, upstreamOrigin, proxyOrigin, 1)
			}
			hdr.Add(k, v)
		}
	}
	hdr.Set("X-Jack-Ketch-Credential", act.Name())
	if stream {
		w.WriteHeader(resp.StatusCode)
		flusher, _ := w.(http.Flusher)
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				_, _ = w.Write([]byte(scrub.Scrub(string(buf[:n]))))
				if flusher != nil {
					flusher.Flush()
				}
			}
			if err != nil {
				return
			}
		}
	}
	if !textual(ct) || scrub.Empty() {
		hdr.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
		if resp.ContentLength < 0 {
			hdr.Del("Content-Length")
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProxyScrubBody+1))
	if err != nil || len(data) > maxProxyScrubBody {
		http.Error(w, fmt.Sprintf("response is larger than %d MiB and cannot be scrubbed", maxProxyScrubBody>>20), http.StatusBadGateway)
		return
	}
	clean := scrub.Scrub(string(data))
	hdr.Set("Content-Length", strconv.Itoa(len(clean)))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.WriteString(w, clean)
}
