package core

import (
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"jack-ketch/internal/config"
	iruntime "jack-ketch/internal/runtime"
	"jack-ketch/internal/session"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

func TestSessionLeaseStopsProcess(t *testing.T) {
	prev := session.SessionLeaseGrace
	session.SessionLeaseGrace = 50 * time.Millisecond
	t.Cleanup(func() { session.SessionLeaseGrace = prev })

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	p, err := db.CreateProject("test")
	if err != nil {
		t.Fatal(err)
	}
	wsPath := t.TempDir()
	ws, err := db.CreateWorkspace(p.ID, wsPath)
	if err != nil {
		t.Fatal(err)
	}
	rt := iruntime.New(config.EmptyLoaded(wsPath), nil)

	e := NewWithOptions(db, Options{DataDir: dir, Logger: slog.Default()})
	t.Cleanup(func() { e.Close() })
	e.runtimes.Store(ws.ID, &ProjectRuntime{
		wsID: ws.ID, projectID: p.ID, wsPath: wsPath, dataDir: dir,
		logger: slog.Default(), store: db, rt: rt, stale: map[string]bool{},
	})

	sess, err := e.Sessions().Register(session.SessionKindMCP, session.HarnessClaudeCode, 0, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(sess.ID, "mcp", "claude-code", 0, ws.ID); err != nil {
		t.Fatal(err)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found in PATH")
	}
	res, err := rt.Start(context.Background(), api.StartRequest{
		Command: sleep,
		Args:    []string{"60"},
		WorkDir: wsPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateProcess(res.ProcessID, ws.ID, "", "", "session", "", sess.ID, 0); err != nil {
		t.Fatal(err)
	}
	e.SessionClosed(sess.ID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := rt.Status(res.ProcessID); err == nil && st.Status == "stopped" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	st, _ := rt.Status(res.ProcessID)
	t.Fatalf("status = %+v, want stopped", st)
}
