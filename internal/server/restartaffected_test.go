package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jack-ketch/pkg/client"
)

func TestConfigApply_RestartAffectedActuallyRestarts(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ws := t.TempDir()
	res, err := cl.ProjectService().Resolve(ctx, ws)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := cl.ConfigService().Apply(ctx, map[string]any{
		"projectId": res.ProjectID, "layer": "project",
		"yaml": "apps:\n  web:\n    command: [sleep, \"30\"]\n", "message": "v1",
	}); err != nil {
		t.Fatalf("apply v1: %v", err)
	}

	started, err := cl.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: res.WorkspaceID, App: "web",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	before, err := cl.ProcessService().Get(ctx, started.ProcessID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	applied, err := cl.ConfigService().Apply(ctx, map[string]any{
		"projectId": res.ProjectID, "layer": "project",
		"yaml": "apps:\n  web:\n    command: [sleep, \"31\"]\n", "message": "v2",
		"restartAffected": true,
	})
	if err != nil {
		t.Fatalf("apply v2: %v", err)
	}
	restarted, _ := applied["restarted"].([]any)
	found := false
	for _, r := range restarted {
		if r == started.ProcessID {
			found = true
		}
	}
	if !found {
		t.Fatalf("restarted list = %v, want to contain %q", restarted, started.ProcessID)
	}

	after, err := cl.ProcessService().Get(ctx, started.ProcessID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after["instance_id"] == before["instance_id"] {
		t.Fatalf("instance_id unchanged (%v); process was not actually restarted", after["instance_id"])
	}
}

func liveEnvHas(t *testing.T, ctx context.Context, cl *client.Client, id, want string) bool {
	t.Helper()
	out, err := cl.ProcessService().GetEnv(ctx, id, true, true)
	if err != nil {
		t.Fatalf("getenv: %v", err)
	}
	list, _ := out["env"].([]any)
	for _, e := range list {
		if e == want {
			return true
		}
	}
	return false
}

func TestRestart_ReReadsAppConfig(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.env"), []byte("FOO=one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "b.env"), []byte("FOO=two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := cl.ProjectService().Resolve(ctx, ws)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	apply := func(yaml string, restart bool) map[string]any {
		out, err := cl.ConfigService().Apply(ctx, map[string]any{
			"projectId": res.ProjectID, "layer": "project", "yaml": yaml, "restartAffected": restart,
		})
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		return out
	}
	apply("apps:\n  web:\n    command: [sleep, \"30\"]\n    env_file: a.env\n", false)
	started, err := cl.ProcessService().Start(ctx, &client.StartRequest{WorkspaceID: res.WorkspaceID, App: "web"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !liveEnvHas(t, ctx, cl, started.ProcessID, "FOO=one") {
		t.Fatal("initial env missing FOO=one")
	}

	out := apply("apps:\n  web:\n    command: [sleep, \"30\"]\n    env_file: b.env\n", true)
	if r, _ := out["restarted"].([]any); len(r) != 1 {
		t.Fatalf("restarted = %v", out["restarted"])
	}
	if !liveEnvHas(t, ctx, cl, started.ProcessID, "FOO=two") {
		t.Fatal("after apply restart: env still stale, want FOO=two")
	}

	if err := os.WriteFile(filepath.Join(ws, "b.env"), []byte("FOO=three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.ProcessService().Restart(ctx, started.ProcessID); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if !liveEnvHas(t, ctx, cl, started.ProcessID, "FOO=three") {
		t.Fatal("after manual restart: env file edit not picked up, want FOO=three")
	}

	apply("apps:\n  web:\n    command: [sleep, \"30\"]\n    env_file: missing.env\n", false)
	if _, err := cl.ProcessService().Restart(ctx, started.ProcessID); err == nil {
		t.Fatal("restart with unresolvable config should fail")
	}
	st, err := cl.ProcessService().Get(ctx, started.ProcessID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if st["status"] != "running" {
		t.Fatalf("failed restart must leave the process running, status=%v", st["status"])
	}
}
