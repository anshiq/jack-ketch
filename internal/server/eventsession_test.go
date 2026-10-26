package server

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"jack-ketch/pkg/client"
)

func TestEventSubscribeDrainDeliversLifecycle(t *testing.T) {
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
	reg, err := cl.SessionService().RegisterWith(ctx, client.RegisterRequest{
		Kind: "mcp", Harness: "claude-code", ClientPID: os.Getpid(), WorkspaceID: res.WorkspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	subID, err := cl.EventService().Subscribe(ctx, client.SubscribeRequest{
		SessionID: reg.SessionID, Types: []string{"process.started"},
	})
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

	found := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !found {
		drained, err := cl.EventService().Drain(ctx, subID, 0)
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
		for _, ev := range drained.Events {
			if ev.Type == "process.started" {
				found = true
			}
		}
		if !found {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !found {
		t.Fatal("process.started event never arrived")
	}
	if err := cl.EventService().Unsubscribe(ctx, subID); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.EventService().Drain(ctx, subID, 0); err == nil || !strings.Contains(err.Error(), "not_found") {
		t.Fatalf("drain after unsubscribe = %v, want not_found", err)
	}
}
