package jackketchd

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"jack-ketch/internal/core"
	"jack-ketch/internal/daemon"
	"jack-ketch/internal/platform/paths"
	"jack-ketch/internal/server"
	"jack-ketch/internal/store"
)

func Main(args []string, version string) error {
	fs := flag.NewFlagSet("jackketchd", flag.ContinueOnError)
	var foreground, showVersion bool
	var socketOverride, dataOverride, tcpAddr, tokenFile string
	fs.BoolVar(&foreground, "foreground", false, "run in foreground (no double-fork)")
	fs.StringVar(&socketOverride, "socket", "", "override daemon socket path")
	fs.StringVar(&dataOverride, "data", "", "override data dir")
	fs.StringVar(&tcpAddr, "tcp", "", "also serve authenticated loopback TCP (e.g. 127.0.0.1:7350) for the web UI")
	fs.StringVar(&tokenFile, "token-file", "", "bearer token file (default $XDG_CONFIG_HOME/jack-ketch/token)")
	fs.BoolVar(&showVersion, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = foreground
	if showVersion {
		fmt.Println(version)
		return nil
	}

	level := new(slog.LevelVar)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	p := paths.User()
	socketPath := p.SocketPath()
	if socketOverride != "" {
		socketPath = socketOverride
	}
	dataDir := p.Data
	if dataOverride != "" {
		dataDir = dataOverride
	}
	if err := p.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}

	d := daemon.New(socketPath, dataDir)
	lock, err := d.AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Close()

	if err := d.WritePid(); err != nil {
		return fmt.Errorf("write pid: %w", err)
	}
	defer os.Remove(d.PidPath())
	defer server.RemoveTCPAddrFile()

	dbPath := p.StateDBPath()
	if dataOverride != "" {
		dbPath = filepath.Join(dataDir, "state.db")
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open state.db: %w", err)
	}
	defer db.Close()

	if n, _ := db.ImportLegacyConfigs(p.Data); n > 0 {
		logger.Info("imported legacy configs", "count", n)
	}

	engine := core.NewWithOptions(db, core.Options{
		DataDir:  dataDir,
		Logger:   logger,
		Version:  version,
		LogLevel: level,
	})
	defer engine.Close()
	engine.ApplySettings()

	if n := engine.CloseStaleInstances(); n > 0 {
		logger.Info("marked processes from the previous daemon as exited", "count", n)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopAll := func() {
		for _, pr := range engine.LoadedRuntimes() {
			if rt, err := pr.Runtime(); err == nil {
				_ = rt.Shutdown()
			}
		}
	}
	srv, err := server.NewWithOptions(engine, socketPath, version, func() {
		logger.Info("shutdown requested via API; stopping managed processes")
		stopAll()
		cancel()
	})
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	defer srv.Close()
	if tokenFile != "" {
		srv.SetTokenPath(tokenFile)
	} else {
		srv.SetTokenPath(p.TokenPath())
	}

	go func() {
		if err := srv.Serve(ctx); err != nil {
			logger.Error("api server", "error", err.Error())
		}
	}()

	if tcpAddr != "" {
		tf := tokenFile
		if tf == "" {
			tf = p.TokenPath()
		}
		token, err := server.EnsureToken(tf)
		if err != nil {
			return fmt.Errorf("token: %w", err)
		}
		go func() {
			logger.Info("tcp listener", "addr", tcpAddr)
			if err := srv.ServeTCP(tcpAddr, token); err != nil {
				logger.Error("tcp server", "error", err.Error())
			}
		}()
	}

	daemon.NotifyReady()
	logger.Info("jackketchd ready", "socket", socketPath, "state", dbPath, "version", version)

	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if d := engine.IdleExit(); d > 0 && engine.IdleLongerThan(d) {
					logger.Info("idle exit", "after", d.String())
					cancel()
					return
				}
			}
		}
	}()

	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGHUP)
	for {
		select {
		case <-ctx.Done():
			logger.Info("jackketchd stopping")
			return nil
		case sig := <-ch:
			switch sig {
			case syscall.SIGHUP:
				logger.Info("reload (SIGHUP): config watcher picks up changes; nothing to do")
			default:
				logger.Info("jackketchd stopping managed processes", "signal", sig.String())
				stopAll()
				return nil
			}
		}
	}
}
