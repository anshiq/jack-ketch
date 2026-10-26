package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"jack-ketch/pkg/client"
)

func TestLogSearchAnalyzedAndRegexError(t *testing.T) {
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
		Command:     []string{"sh", "-c", "echo needle-1 && echo needle-2 && sleep 30"},
		WorkDir:     ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cl.ProcessService().Stop(ctx, started.ProcessID)

	var matches []any
	for i := 0; i < 50; i++ {
		out, err := cl.LogService().Search(ctx, map[string]any{"workspaceId": res.WorkspaceID, "query": "needle"})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		matches, _ = out["matches"].([]any)
		if len(matches) >= 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2: %v", len(matches), matches)
	}

	_, err = cl.LogService().Search(ctx, map[string]any{"workspaceId": res.WorkspaceID, "query": "needle-[", "regex": true})
	if err == nil || !strings.Contains(err.Error(), "invalid_argument") {
		t.Fatalf("regex error = %v, want invalid_argument", err)
	}
}
