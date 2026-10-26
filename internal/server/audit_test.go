package server

import (
	"context"
	"testing"
	"time"

	"jack-ketch/pkg/client"
)

func TestAuditMutationsOnly(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ws := t.TempDir()
	res, err := cl.ProjectService().Resolve(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	started, err := cl.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: res.WorkspaceID,
		Command:     []string{"sleep", "30"},
		WorkDir:     ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cl.ProcessService().Stop(ctx, started.ProcessID)

	entries, err := cl.AuditService().List(ctx, res.WorkspaceID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("audit rows = %d, want 1: %v", len(entries), entries)
	}
	if entries[0]["action"] != "ProcessService/Start" {
		t.Fatalf("action = %v", entries[0]["action"])
	}
	if entries[0]["workspaceId"] != res.WorkspaceID {
		t.Fatalf("workspaceId = %v", entries[0]["workspaceId"])
	}

	_ = cl.SessionService().Ping(ctx, "sess-missing")
	_, _ = cl.ProcessService().List(ctx, res.WorkspaceID, false)

	entries, err = cl.AuditService().List(ctx, res.WorkspaceID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("non-mutation calls were audited: %d rows", len(entries))
	}
}
