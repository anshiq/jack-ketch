package httpserve

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/time/rate"
)

const (
	defaultRate  = 20.0
	defaultBurst = 50
)

type Options struct {
	Token    string
	Loopback bool
	Logger   *slog.Logger
}

func Handler(serverFor func(*http.Request) *mcp.Server, o Options) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(serverFor, &mcp.StreamableHTTPOptions{
		Logger:    o.Logger,
		Stateless: true,
	})

	var h http.Handler = streamable
	h = rateLimit(h, o.Token, defaultRate, defaultBurst)
	h = auth(h, o.Token)
	h = originGuard(h, o.Loopback)
	return h
}

func originGuard(next http.Handler, loopback bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !isLoopbackHost(u.Hostname()) {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}
		if loopback {
			host, _, err := net.SplitHostPort(r.Host)
			if err != nil {
				host = r.Host
			}
			if !isLoopbackHost(host) {
				http.Error(w, "forbidden host", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "[::1]"
}

func auth(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			http.Error(w, "jack-ketch http transport requires a token (--token, --token-file or JACK_KETCH_HTTP_TOKEN)", http.StatusServiceUnavailable)
			return
		}
		if !bearerMatches(r.Header.Get("Authorization"), token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerMatches(header, token string) bool {
	parts := strings.SplitN(header, " ", 2)
	return len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && subtle.ConstantTimeCompare([]byte(parts[1]), []byte(token)) == 1
}

type rateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	r        rate.Limit
	b        int
}

func newRateLimiter(r rate.Limit, b int) *rateLimiter {
	return &rateLimiter{limiters: make(map[string]*rate.Limiter), r: r, b: b}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	l, ok := rl.limiters[key]
	if !ok {
		l = rate.NewLimiter(rl.r, rl.b)
		rl.limiters[key] = l
	}
	rl.mu.Unlock()
	return l.Allow()
}

func rateLimit(next http.Handler, token string, r float64, burst int) http.Handler {
	rl := newRateLimiter(rate.Limit(r), burst)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !rl.allow(token) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, req)
	})
}
