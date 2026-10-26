package server

import (
	"context"
	"os"
	"testing"
	"time"

	"jack-ketch/pkg/client"
)

func TestOpenShellCreatesSessionScopedRow(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	reg, err := cl.SessionService().RegisterWith(ctx, client.RegisterRequest{
		Kind: "mcp", Harness: "claude-code", ClientPID: os.Getpid(), WorkspaceID: res.WorkspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := cl.ProcessService().OpenShell(ctx, client.OpenShellRequest{
		WorkspaceID: res.WorkspaceID, ProcessID: started.ProcessID, SessionID: reg.SessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	shellID, _ := out["processId"].(string)
	if shellID == "" {
		t.Fatalf("no shell processId: %v", out)
	}
	defer cl.ProcessService().Stop(ctx, shellID)
	if out["lifetime"] != "session" {
		t.Fatalf("lifetime = %v, want session", out["lifetime"])
	}
	row, err := eng.Store().GetProcess(shellID)
	if err != nil {
		t.Fatalf("store row: %v", err)
	}
	if row.Lifetime != "session" {
		t.Fatalf("row lifetime = %q, want session", row.Lifetime)
	}
}
