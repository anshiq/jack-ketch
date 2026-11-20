//go:build !windows

package core

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jack-ketch/internal/config"
	iruntime "jack-ketch/internal/runtime"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

func watchRuntime(t *testing.T, watch *config.WatchConfig) (*ProjectRuntime, *iruntime.Runtime, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ws := t.TempDir()
	for _, d := range []string{"src", "node_modules/pkg"} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	app := config.V3App{AppConfig: config.AppConfig{Command: []string{"sh", "-c", "echo started; sleep 30"}}, Watch: watch}
	resolved := &config.Resolved{Apps: map[string]config.V3App{"web": app}}
	rt := iruntime.New(resolved.ToLoaded(ws), slog.Default())
	t.Cleanup(func() { _ = rt.Shutdown() })
	pr := &ProjectRuntime{
		wsID: "ws_test", projectID: "proj_test", wsPath: ws, dataDir: dir,
		logger: slog.Default(), store: db, rt: rt, resolved: resolved, stale: map[string]bool{},
	}
	t.Cleanup(pr.stopWatchers)
	return pr, rt, ws
}

func instanceOf(t *testing.T, rt *iruntime.Runtime, id string) string {
	t.Helper()
	st, err := rt.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	return st.InstanceID
}

func waitUntil(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestWatchRestartsAppOnSourceChange(t *testing.T) {
	pr, rt, ws := watchRuntime(t, &config.WatchConfig{Paths: []string{"src"}, Debounce: config.Duration(80 * time.Millisecond)})
	res, err := rt.Start(context.Background(), api.StartRequest{App: "web"})
	if err != nil {
		t.Fatal(err)
	}
	pr.syncWatchers()
	if len(pr.WatchedApps()["web"]) == 0 {
		t.Fatalf("no watcher for web: %+v", pr.WatchedApps())
	}
	first := instanceOf(t, rt, res.ProcessID)

	_ = os.WriteFile(filepath.Join(ws, "node_modules", "pkg", "index.js"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "src", "debug.log"), []byte("x"), 0o644)
	time.Sleep(400 * time.Millisecond)
	if got := instanceOf(t, rt, res.ProcessID); got != first {
		t.Fatalf("ignored change restarted the app (%s -> %s)", first, got)
	}

	for i := 0; i < 3; i++ {
		_ = os.WriteFile(filepath.Join(ws, "src", "main.ts"), []byte{byte('a' + i)}, 0o644)
		time.Sleep(10 * time.Millisecond)
	}
	if !waitUntil(t, 5*time.Second, func() bool { return instanceOf(t, rt, res.ProcessID) != first }) {
		t.Fatal("source change did not restart the app")
	}
	second := instanceOf(t, rt, res.ProcessID)
	time.Sleep(400 * time.Millisecond)
	if got := instanceOf(t, rt, res.ProcessID); got != second {
		t.Fatalf("burst of writes restarted more than once (%s -> %s)", second, got)
	}

	if err := os.MkdirAll(filepath.Join(ws, "src", "feature"), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	third := instanceOf(t, rt, res.ProcessID)
	_ = os.WriteFile(filepath.Join(ws, "src", "feature", "a.ts"), []byte("x"), 0o644)
	if !waitUntil(t, 5*time.Second, func() bool { return instanceOf(t, rt, res.ProcessID) != third }) {
		t.Fatal("change in a new subdirectory was not seen")
	}
}

func TestWatchSyncStopsRemovedWatchers(t *testing.T) {
	pr, _, _ := watchRuntime(t, &config.WatchConfig{Paths: []string{"src"}})
	pr.syncWatchers()
	if len(pr.WatchedApps()) != 1 {
		t.Fatalf("watchers = %+v", pr.WatchedApps())
	}
	app := pr.resolved.Apps["web"]
	app.Watch = nil
	pr.resolved.Apps["web"] = app
	pr.syncWatchers()
	if len(pr.WatchedApps()) != 0 {
		t.Fatalf("watcher not removed: %+v", pr.WatchedApps())
	}
}

func TestWatchConfigParsesShortForms(t *testing.T) {
	for _, doc := range []string{
		"version: 3\napps:\n  web:\n    command: [sh]\n    watch: src\n",
		"version: 3\napps:\n  web:\n    command: [sh]\n    watch: [src, go.mod]\n",
		"version: 3\napps:\n  web:\n    command: [sh]\n    watch:\n      paths: [src]\n      ignore: [\"*.gen.go\"]\n      debounce: 500ms\n",
	} {
		cfg, errs := config.ParseV3([]byte(doc))
		if errs != nil {
			t.Fatalf("parse %q: %v", doc, errs[0])
		}
		if !cfg.Apps["web"].Watch.Enabled() {
			t.Fatalf("watch not enabled for %q", doc)
		}
	}
	if _, errs := config.ParseV3([]byte("version: 3\napps:\n  web:\n    command: [sh]\n    watch:\n      path: [src]\n")); errs == nil {
		t.Fatal("unknown watch key accepted")
	}
	if _, errs := config.ParseV3([]byte("version: 3\napps:\n  web:\n    command: [sh]\n    watch:\n      paths: [/etc]\n")); errs == nil {
		t.Fatal("absolute watch path accepted")
	}
}
