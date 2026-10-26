// TCP listener for the web UI and remote tooling (§5.5, Phase 9 remote
// daemon). Disabled by default; enabled with jackketchd --tcp 127.0.0.1:7350.
// Auth: bearer token (from the token file, 0600, generated when absent),
// strict Host (127.0.0.1:port/localhost:port) and Origin checks
// (DNS-rebinding defence), no permissive CORS, token never in cookies.
// Remote hosts reach it over SSH-forwarded sockets/ports.
package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"jack-ketch/internal/platform/paths"
	"jack-ketch/internal/webui"
)

// EnsureToken reads the bearer token, generating + storing one (0600)
// when absent.
func EnsureToken(tokenPath string) (string, error) {
	if data, err := os.ReadFile(tokenPath); err == nil {
		if tok := strings.TrimSpace(string(data)); tok != "" {
			return tok, nil
		}
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b[:])
	if err := os.MkdirAll(dirOf(tokenPath), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(tokenPath, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

func hostGuard(next http.Handler) http.Handler {
	limiter := rate.NewLimiter(rate.Every(time.Second/50), 200)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !limiter.Allow() {
			http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
			return
		}
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, `{"error":"forbidden host"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerGuard(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			ok := false
			for _, prefix := range []string{"http://127.0.0.1:", "http://localhost:", "http://[::1]:"} {
				if strings.HasPrefix(origin, prefix) {
					ok = true
					break
				}
			}
			if !ok {
				http.Error(w, `{"error":"forbidden origin"}`, http.StatusForbidden)
				return
			}
		}
		// Bearer token (login screen pastes it; GUI `web open` prints a
		// one-time URL with #token=... — fragment never hits the wire).
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="jack-ketch"`)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// stripAPIPrefix lets the TCP listener serve both /api/<svc>/<m> (web UI
// fetch paths) and bare /<svc>/<m> paths.
func stripAPIPrefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
			next.ServeHTTP(w, r2)
			return
		}
		next.ServeHTTP(w, r)
	})
}

const webUICSP = "default-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"worker-src 'self' blob:; img-src 'self' data:; font-src 'self' data:"

func staticWebUI() http.Handler {
	dist := webui.Dist()
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", webUICSP)
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(dist, path); err != nil {
			w.Header().Set("Cache-Control", "no-cache")
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		} else if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func tcpMux(apiHandler http.Handler, token string) http.Handler {
	api := bearerGuard(stripAPIPrefix(apiHandler), token)
	static := staticWebUI()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		static.ServeHTTP(w, r)
	})
}

// Blocks until ctx done or error; call sites run it alongside Serve.
func (s *Server) ServeTCP(addr, token string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("server: tcp listen %s: %w", addr, err)
	}
	return s.serveTCPListener(ln, token)
}

func (s *Server) serveTCPListener(ln net.Listener, token string) error {
	srv := &http.Server{Handler: hostGuard(tcpMux(s.handler, token)), ReadHeaderTimeout: 5 * time.Second}
	s.tcpMu.Lock()
	s.tcpAddr = ln.Addr().String()
	s.tcpSrv = srv
	s.tcpMu.Unlock()
	writeTCPPortFile(ln.Addr().String())
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// TCPAddr returns the running TCP listener address, or "" when none.
func (s *Server) TCPAddr() string {
	s.tcpMu.Lock()
	defer s.tcpMu.Unlock()
	return s.tcpAddr
}

func writeTCPPortFile(addr string) {
	p := paths.User()
	_ = os.MkdirAll(p.Runtime, 0o700)
	_ = os.WriteFile(p.TCPAddrPath(), []byte(addr+"\n"), 0o600)
}

// TCPAddrFromFile returns the recorded TCP listener address, or "".
func TCPAddrFromFile() string {
	data, err := os.ReadFile(paths.User().TCPAddrPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// RemoveTCPAddrFile deletes the recorded TCP listener address.
func RemoveTCPAddrFile() {
	_ = os.Remove(paths.User().TCPAddrPath())
}
