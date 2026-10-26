package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"jack-ketch/internal/core"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/client"
)

func TestShutdownDefaultKeepsProcesses(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	eng := core.NewWithOptions(db, core.Options{DataDir: dir})
	defer eng.Close()
	ch := make(chan bool, 1)
	srv, err := NewWithOptions(eng, filepath.Join(dir, "jackketchd.sock"), "test", func(keep bool) { ch <- keep })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()
	defer srv.Close()

	cl := client.New(filepath.Join(dir, "jackketchd.sock"), "test/cli")
	callCtx, callCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer callCancel()
	for i := 0; i < 100; i++ {
		if _, err := cl.SystemService().GetVersion(callCtx); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cl.Call(callCtx, "SystemService", "Shutdown", map[string]any{}, nil); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case keep := <-ch:
		if !keep {
			t.Fatal("default keepProcesses = false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onShutdown never called")
	}
}

func TestSettingsBooleanRoundTrip(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var out map[string]any
	if err := cl.Call(ctx, "SystemService", "UpdateSettings", map[string]any{"learnRawStarts": false}, &out); err != nil {
		t.Fatal(err)
	}
	settings, _ := out["settings"].(map[string]any)
	v, ok := settings["learn_raw_starts"].(bool)
	if !ok || v {
		t.Fatalf("learn_raw_starts = %#v, want false bool", settings["learn_raw_starts"])
	}

	var got map[string]any
	if err := cl.Call(ctx, "SystemService", "GetSettings", map[string]any{}, &got); err != nil {
		t.Fatal(err)
	}
	settings, _ = got["settings"].(map[string]any)
	v, ok = settings["learn_raw_starts"].(bool)
	if !ok || v {
		t.Fatalf("read-back learn_raw_starts = %#v, want false bool", settings["learn_raw_starts"])
	}
}

func TestSettingsRejectUnknown(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := cl.Call(ctx, "SystemService", "UpdateSettings", map[string]any{"bogusSetting": 1}, nil)
	if err == nil || !containsStr(err.Error(), "invalid_argument") {
		t.Fatalf("unknown setting error = %v, want invalid_argument", err)
	}
}
