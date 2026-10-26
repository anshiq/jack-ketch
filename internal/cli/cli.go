package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/config"
	"jack-ketch/internal/daemon"
	"jack-ketch/internal/httpserve"
	rtmcp "jack-ketch/internal/mcp"
	"jack-ketch/internal/platform/paths"
	"jack-ketch/internal/runtime"
)

func Serve(loaded *config.Loaded, logger *slog.Logger) error {
	return serveStdio(loaded, logger, serveOptions{})
}

type serveOptions struct {
	embedded    bool
	project     string
	harness     string
	maxWaitMS   int
	toolset     string
	check       bool
	httpAddr    string
	allowRemote bool
	token       string
	tokenFile   string
}

func serveStdio(loaded *config.Loaded, logger *slog.Logger, opts serveOptions) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	lazy := rtmcp.NewLazyBackend()
	var mu sync.Mutex
	var cleanup func() error
	go func() {
		if !opts.embedded {
			b, err := dialBridge(logger, opts)
			if err == nil {
				mu.Lock()
				cleanup = b.Close
				mu.Unlock()
				lazy.Resolve(b, "daemon", nil)
				return
			}
			logger.Warn("daemon unreachable; falling back to embedded runtime", "error", err)
		}
		base := loaded
		if opts.project != "" {
			base = config.EmptyLoaded(opts.project)
		}
		base.StateDir = embeddedStateDir(base.ProjectDir)
		rt := runtime.New(base, logger)
		mu.Lock()
		cleanup = rt.Shutdown
		mu.Unlock()
		lazy.Resolve(rt, "embedded", nil)
	}()
	server := rtmcp.NewServerWith(rtmcp.Options{
		Backend: lazy.Get, Mode: lazy.Mode, Logger: logger,
		Harness: opts.harness, HarnessExplicit: opts.harness != "",
		MaxWaitMS: opts.maxWaitMS, Toolset: opts.toolset,
	})
	err := server.Run(ctx, &mcp.StdioTransport{})
	mu.Lock()
	c := cleanup
	mu.Unlock()
	if c != nil {
		_ = c()
	}
	return err
}

func dialBridge(logger *slog.Logger, opts serveOptions) (*rtmcp.Bridge, error) {
	return dialBridgeWorkspace(logger, opts, opts.project)
}

func dialBridgeWorkspace(logger *slog.Logger, opts serveOptions, workspace string) (*rtmcp.Bridge, error) {
	p := paths.User()
	if err := p.EnsureDirs(); err != nil {
		return nil, err
	}
	d := daemon.New(p.SocketPath(), p.Data)
	start := func() error {
		if d.IsRunning() {
			return nil
		}
		if err := d.Start(); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		return d.WaitReady(10 * time.Second)
	}
	if err := start(); err != nil {
		return nil, err
	}
	return rtmcp.DialBridgeWith(rtmcp.BridgeOptions{SocketPath: p.SocketPath(), WorkspacePath: workspace, Harness: opts.harness, Respawn: start, Logger: logger})
}

func ServeHTTP(logger *slog.Logger, opts serveOptions) error {
	if opts.httpAddr == "" {
		return errors.New("serve --http requires a listen address")
	}
	addr := opts.httpAddr
	if strings.HasPrefix(addr, ":") && !opts.allowRemote {
		addr = "127.0.0.1" + addr
	}
	host, _, err := net.SplitHostPort(addr)
	loopback := err == nil && loopbackHost(host)
	if !loopback {
		fmt.Fprintf(os.Stderr, "warning: serving MCP over HTTP on non-loopback host %s\n", addr)
	}

	token := opts.token
	tokenSource := "token from --token or JACK_KETCH_HTTP_TOKEN"
	if token == "" && opts.tokenFile != "" {
		data, err := os.ReadFile(opts.tokenFile)
		if err != nil {
			return fmt.Errorf("read token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
		tokenSource = opts.tokenFile
	}
	if token == "" {
		if env := os.Getenv("JACK_KETCH_HTTP_TOKEN"); env != "" {
			token = env
			tokenSource = "token from JACK_KETCH_HTTP_TOKEN"
		}
	}
	if token == "" {
		tok, err := readOrCreateToken(paths.User().TokenPath())
		if err != nil {
			return err
		}
		token = tok
		tokenSource = paths.User().TokenPath()
	}
	fmt.Fprintf(os.Stderr, "MCP endpoint: http://%s/  (Authorization: Bearer %s)\n", addr, tokenSource)

	newServer := func(facade runtime.Facade) *mcp.Server {
		return rtmcp.NewServerWith(rtmcp.Options{Facade: facade, Logger: logger, Harness: opts.harness, HarnessExplicit: opts.harness != "", MaxWaitMS: opts.maxWaitMS, Toolset: opts.toolset})
	}

	type wsEntry struct {
		server  *mcp.Server
		cleanup func() error
		last    time.Time
	}

	var mu sync.Mutex
	cache := map[string]*wsEntry{}

	var defaultEntry *wsEntry
	if b, err := dialBridge(logger, opts); err == nil {
		defaultEntry = &wsEntry{server: newServer(b), cleanup: b.Close, last: time.Now()}
	} else {
		logger.Warn("daemon unreachable for --http; serving embedded", "error", err)
		projectDir := opts.project
		if projectDir == "" {
			projectDir = rtmcp.DefaultWorkspacePath()
		}
		base := config.EmptyLoaded(projectDir)
		base.StateDir = embeddedStateDir(base.ProjectDir)
		rt := runtime.New(base, logger)
		defaultEntry = &wsEntry{server: newServer(rt), cleanup: rt.Shutdown, last: time.Now()}
	}

	closeAll := func() {
		mu.Lock()
		defer mu.Unlock()
		for k, e := range cache {
			if e.cleanup != nil {
				_ = e.cleanup()
			}
			delete(cache, k)
		}
		if defaultEntry.cleanup != nil {
			_ = defaultEntry.cleanup()
			defaultEntry.cleanup = nil
		}
	}

	serverFor := func(r *http.Request) *mcp.Server {
		key := r.Header.Get("X-Jack-Ketch-Workspace")
		if key == "" {
			key = r.URL.Query().Get("workspace")
		}
		if key == "" {
			return defaultEntry.server
		}
		mu.Lock()
		defer mu.Unlock()
		if e, ok := cache[key]; ok {
			e.last = time.Now()
			return e.server
		}
		b, err := dialBridgeWorkspace(logger, opts, key)
		if err != nil {
			logger.Warn("workspace bridge unavailable; serving the default workspace", "workspace", key, "error", err)
			return defaultEntry.server
		}
		for len(cache) >= 32 {
			oldestKey := ""
			var oldest time.Time
			for k, e := range cache {
				if oldestKey == "" || e.last.Before(oldest) {
					oldestKey, oldest = k, e.last
				}
			}
			if e := cache[oldestKey]; e != nil && e.cleanup != nil {
				_ = e.cleanup()
			}
			delete(cache, oldestKey)
		}
		e := &wsEntry{server: newServer(b), cleanup: b.Close, last: time.Now()}
		cache[key] = e
		return e.server
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer closeAll()

	srv := &http.Server{Addr: addr, Handler: httpserve.Handler(serverFor, httpserve.Options{Token: token, Loopback: loopback, Logger: logger})}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	logger.Info("serving streamable-HTTP", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func serveCheck(logger *slog.Logger, opts serveOptions) error {
	out := map[string]any{"version": rtmcp.Version, "harness": opts.harness}
	emit := func() {
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
	}
	b, err := dialBridge(logger, opts)
	if err != nil {
		out["ok"], out["error"] = false, err.Error()
		emit()
		return err
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if v, err := b.Client().GetVersion(ctx); err == nil {
		out["daemonVersion"] = v.DaemonVersion
	}
	out["workspace"] = b.WorkspacePath()
	server := rtmcp.NewServerWith(rtmcp.Options{Facade: b, Logger: logger, Harness: opts.harness, HarnessExplicit: opts.harness != "", Toolset: opts.toolset})
	ct, st := mcp.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, st) }()
	cl := mcp.NewClient(&mcp.Implementation{Name: "jack-ketch-check", Version: rtmcp.Version}, nil)
	sess, err := cl.Connect(ctx, ct, nil)
	if err != nil {
		out["ok"], out["error"] = false, err.Error()
		emit()
		return err
	}
	defer sess.Close()
	tools, err := sess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		out["ok"], out["error"] = false, err.Error()
		emit()
		return err
	}
	out["ok"], out["tools"] = true, len(tools.Tools)
	emit()
	return nil
}

func loopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

func embeddedStateDir(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))
	return filepath.Join(paths.User().Data, "embedded", hex.EncodeToString(sum[:8]))
}
