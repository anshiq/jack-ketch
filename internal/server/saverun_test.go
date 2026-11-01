package server

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jack-ketch/pkg/api"
	"jack-ketch/pkg/client"
)

func waitExit(t *testing.T, ctx context.Context, cl *client.Client, id string) {
	t.Helper()
	if _, err := cl.ProcessService().WaitForExit(ctx, id, 10000); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRunEndToEnd(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ws := t.TempDir()
	if _, err := exec.LookPath("git"); err == nil {
		_ = exec.Command("git", "init", "-q", ws).Run()
	}
	script := filepath.Join(ws, "smoke.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"FOO=$FOO BAR=$BAR\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".env.local"), []byte("FOO=from-file\nBAR=file-bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := cl.ProjectService().Resolve(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	started, err := cl.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: res.WorkspaceID, Command: []string{"./smoke.sh"}, WorkDir: ws,
		Env: map[string]string{"FOO": "from-request"}, EnvFile: ".env.local",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitExit(t, ctx, cl, started.ProcessID)

	sug, err := cl.ProcessService().SaveSuggestion(ctx, started.ProcessID)
	if err != nil || sug == nil {
		t.Fatalf("suggestion = %+v %v", sug, err)
	}
	if sug.Kind != "task" || !sug.NewApp || sug.Mode != "run" || len(sug.EnvSources) != 2 || sug.EnvSources[0].File != ".env.local" || !strings.Contains(sug.YAML, "modes:") || sug.Ask == "" {
		t.Fatalf("suggestion = %+v", sug)
	}

	saved, err := cl.ConfigService().SaveRun(ctx, "", api.SaveRunRequest{ProcessID: started.ProcessID, App: "tool", Mode: "smoke", Description: "smoke script",
		Checks: []api.CheckSpec{{Name: "port", TCP: "127.0.0.1:1"}}})
	if err != nil || !saved.Saved || saved.App != "tool" || saved.Mode != "smoke" || !strings.Contains(saved.StartWith, `mode="smoke"`) {
		t.Fatalf("save = %+v %v", saved, err)
	}
	if again, _ := cl.ProcessService().SaveSuggestion(ctx, started.ProcessID); again != nil {
		t.Fatalf("saved run suggested again: %+v", again)
	}
	cfg, err := cl.ConfigService().Get(ctx, res.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := cfg["raw"].(map[string]any)
	proj, _ := raw["project"].(string)
	for _, want := range []string{"tool:", "default_mode: smoke", "smoke:", "file: .env.local", "FOO: from-request", "description: smoke script", "tcp: 127.0.0.1:1"} {
		if !strings.Contains(proj, want) {
			t.Fatalf("project yaml missing %q:\n%s", want, proj)
		}
	}

	replay, err := cl.ProcessService().Start(ctx, &client.StartRequest{WorkspaceID: res.WorkspaceID, App: "tool", Mode: "smoke"})
	if err != nil {
		t.Fatal(err)
	}
	waitExit(t, ctx, cl, replay.ProcessID)
	logs, err := cl.LogService().Query(ctx, api.GetLogsRequest{ProcessID: replay.ProcessID, Lines: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asJSON(logs), "FOO=from-request BAR=file-bar") {
		t.Fatalf("replayed run did not get the saved env order: %v", logs)
	}
	if s, _ := cl.ProcessService().SaveSuggestion(ctx, replay.ProcessID); s != nil {
		t.Fatalf("app run without overrides suggested: %+v", s)
	}

	other, err := cl.ProcessService().Start(ctx, &client.StartRequest{WorkspaceID: res.WorkspaceID, Command: []string{"./smoke.sh", "x"}, WorkDir: ws})
	if err != nil {
		t.Fatal(err)
	}
	waitExit(t, ctx, cl, other.ProcessID)
	if s, _ := cl.ProcessService().SaveSuggestion(ctx, other.ProcessID); s == nil {
		t.Fatal("expected a suggestion before dismissing")
	}
	if d, err := cl.ConfigService().SaveRun(ctx, "", api.SaveRunRequest{ProcessID: other.ProcessID, Action: "dismiss"}); err != nil || !d.Dismissed {
		t.Fatalf("dismiss = %+v %v", d, err)
	}
	if s, _ := cl.ProcessService().SaveSuggestion(ctx, other.ProcessID); s != nil {
		t.Fatalf("dismissed run suggested again: %+v", s)
	}

	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	st, err := cl.ConfigService().GitignoreStatus(ctx, res.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asJSON(st["missing"]), ".env.local") {
		t.Fatalf("gitignore status = %v", st)
	}
	upd, err := cl.ConfigService().UpdateGitignore(ctx, res.WorkspaceID, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(ws, ".gitignore"))
	if !strings.Contains(string(data), "/.env.local") || !strings.Contains(string(data), "# jack-ketch") {
		t.Fatalf(".gitignore = %q (%v)", data, upd)
	}
	st, _ = cl.ConfigService().GitignoreStatus(ctx, res.WorkspaceID)
	if strings.Contains(asJSON(st["missing"]), ".env.local") {
		t.Fatalf("still missing after update: %v", st)
	}
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
