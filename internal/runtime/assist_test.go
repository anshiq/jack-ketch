package runtime_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jack-ketch/internal/config"
	"jack-ketch/internal/runtime"
	"jack-ketch/pkg/api"
)

func loadYAML(t *testing.T, dir, yaml string) *runtime.Runtime {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "jack-ketch.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(filepath.Join(dir, "jack-ketch.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(loaded, nil)
	t.Cleanup(func() { rt.Shutdown() })
	return rt
}

func firstLine(t *testing.T, rt *runtime.Runtime, id, contains string) string {
	t.Helper()
	w, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{ProcessID: id, Contains: contains, TimeoutMS: 5000})
	if err != nil || !w.Matched || w.Entry == nil {
		t.Fatalf("no %q line: %+v %v", contains, w, err)
	}
	return w.Entry.Line
}

func TestModesAndOrderedEnvSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("LAYER=file\nONLY_FILE=yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.prod"), []byte("LAYER=prod-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := loadYAML(t, dir, `
apps:
  api:
    default_mode: dev
    env_sources:
      - file: .env
      - file: .env.local
        optional: true
      - vars:
          LAYER: vars
    modes:
      dev:
        command: ["`+helperPath+`", "env", "LAYER"]
      prod:
        description: production build
        command: ["`+helperPath+`", "env", "LAYER"]
        env_sources:
          - file: .env.prod
      vars-first:
        command: ["`+helperPath+`", "env", "LAYER"]
        env_sources:
          - vars: {LAYER: mode-vars}
          - file: .env.prod
`)
	dev, err := rt.Start(context.Background(), api.StartRequest{App: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if dev.Mode != "dev" {
		t.Fatalf("default mode = %q, want dev", dev.Mode)
	}
	if got := firstLine(t, rt, dev.ProcessID, "LAYER="); got != "LAYER=vars" {
		t.Fatalf("dev env: %s", got)
	}
	prod, err := rt.Start(context.Background(), api.StartRequest{App: "api", Mode: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if got := firstLine(t, rt, prod.ProcessID, "LAYER="); got != "LAYER=prod-file" {
		t.Fatalf("prod env: %s", got)
	}
	env, err := rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: prod.ProcessID})
	if err != nil {
		t.Fatal(err)
	}
	if env.Source["LAYER"] != "mode:prod/file:.env.prod" || env.Source["ONLY_FILE"] != "file:.env" {
		t.Fatalf("provenance = %v", env.Source)
	}
	ordered, err := rt.Start(context.Background(), api.StartRequest{App: "api", Mode: "vars-first"})
	if err != nil {
		t.Fatal(err)
	}
	if got := firstLine(t, rt, ordered.ProcessID, "LAYER="); got != "LAYER=prod-file" {
		t.Fatalf("later file must beat earlier vars: %s", got)
	}
	st, err := rt.Status(prod.ProcessID)
	if err != nil || st.App != "api" || st.Mode != "prod" {
		t.Fatalf("status app/mode = %+v %v", st, err)
	}
	if _, err := rt.Start(context.Background(), api.StartRequest{App: "api", Mode: "nope"}); err == nil || !strings.Contains(err.Error(), "dev, prod, vars-first") {
		t.Fatalf("unknown mode error = %v", err)
	}
	apps, err := rt.Apps()
	if err != nil || len(apps.Apps) != 1 {
		t.Fatalf("apps = %+v %v", apps, err)
	}
	a := apps.Apps[0]
	if a.DefaultMode != "dev" || len(a.Modes) != 3 || !a.Modes[0].Default || a.Modes[1].Description != "production build" {
		t.Fatalf("app info = %+v", a)
	}
}

func TestRequiredEnvFileMissing(t *testing.T) {
	dir := t.TempDir()
	rt := loadYAML(t, dir, `
apps:
  api:
    command: ["`+helperPath+`", "env", "X"]
    env_sources:
      - file: .env.missing
`)
	_, err := rt.Start(context.Background(), api.StartRequest{App: "api"})
	if err == nil || !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "optional") {
		t.Fatalf("err = %v", err)
	}
}

func TestOutcomeTaskSuccess(t *testing.T) {
	rt := newRuntime(t)
	res := startSh(t, rt, "echo done", api.StartRequest{WorkDir: t.TempDir()})
	if _, err := rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000}); err != nil {
		t.Fatal(err)
	}
	out, err := rt.Outcome(res.ProcessID)
	if err != nil || !out.Success || out.Kind != "task" || out.Reason != "exit_0" {
		t.Fatalf("outcome = %+v %v", out, err)
	}
	failed := startSh(t, rt, "exit 3", api.StartRequest{WorkDir: t.TempDir()})
	_, _ = rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: failed.ProcessID, TimeoutMS: 5000})
	if out, _ := rt.Outcome(failed.ProcessID); out.Success {
		t.Fatalf("failed run reported success: %+v", out)
	}
}

func TestDiagnoseMissingEnvWithExample(t *testing.T) {
	rt := newRuntime(t)
	dir := t.TempDir()
	example := "# Postgres connection string for the app\nDATABASE_URL=postgres://localhost:5432/app\nPORT=3000\n"
	if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte(example), 0o644); err != nil {
		t.Fatal(err)
	}
	res := startSh(t, rt, "echo 'Error: Missing required environment variable: DATABASE_URL' >&2; exit 1", api.StartRequest{WorkDir: dir, Env: []string{"PORT=4000"}})
	_, _ = rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000})
	d, err := rt.Diagnose(res.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Category != "missing_env" || d.Actor != "user" || !d.Failing {
		t.Fatalf("diagnosis = %+v", d)
	}
	if len(d.MissingEnv) != 1 || d.MissingEnv[0].Key != "DATABASE_URL" || !strings.Contains(d.MissingEnv[0].Hint, "Postgres") || d.MissingEnv[0].Example == "" {
		t.Fatalf("missing env = %+v", d.MissingEnv)
	}
}

func TestDiagnoseMissingNodeModules(t *testing.T) {
	rt := newRuntime(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"express":"^4"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte("lockfileVersion: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := startSh(t, rt, `echo "Error: Cannot find module 'express'" >&2; exit 1`, api.StartRequest{WorkDir: dir})
	_, _ = rt.WaitForExit(context.Background(), api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 5000})
	d, err := rt.Diagnose(res.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Category != "missing_dependency" || d.Actor != "agent_with_approval" {
		t.Fatalf("diagnosis = %+v", d)
	}
	if len(d.Issues[0].Fixes) == 0 || d.Issues[0].Fixes[0].Command != "pnpm install" || !d.Issues[0].Fixes[0].NeedsApproval {
		t.Fatalf("fixes = %+v", d.Issues[0].Fixes)
	}
}

func startHTTP(t *testing.T, rt *runtime.Runtime) string {
	t.Helper()
	res, err := rt.Start(context.Background(), api.StartRequest{Command: helperPath, Args: []string{"http-server", "200"}, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	firstLine(t, rt, res.ProcessID, "LISTENING")
	return res.ProcessID
}

func TestProbeCorrelatesLogs(t *testing.T) {
	rt := newRuntime(t)
	id := startHTTP(t, rt)
	var res *api.ProbeResult
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err = rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: "/hello"})
		if err == nil && res.Status == 200 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || !res.OK || res.Status != 200 || res.Body != "ok /hello" {
		t.Fatalf("probe = %+v %v", res, err)
	}
	found := false
	for _, l := range res.Logs {
		if strings.Contains(l.Line, "path=/hello") {
			found = true
		}
	}
	if !found {
		t.Fatalf("request log not correlated: %+v", res.Logs)
	}
	if res.Check == nil || res.Check.URL != "/hello" || res.Check.ExpectStatus != 200 || res.Check.Name != "hello" {
		t.Fatalf("check = %+v", res.Check)
	}
	boom, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: "/boom"})
	if err != nil || boom.OK || boom.Status != 500 || boom.ErrorLogs == 0 {
		t.Fatalf("boom probe = %+v %v", boom, err)
	}
	_, err = rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: "/orders", Method: "POST", Body: `{"q":1}`})
	var ce *runtime.ConfirmError
	if !errors.As(err, &ce) {
		t.Fatalf("POST without confirmation: %v", err)
	}
	post, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: "/orders", Method: "POST", Body: `{"q":1}`, Confirmed: true})
	if err != nil || !post.OK {
		t.Fatalf("confirmed POST = %+v %v", post, err)
	}
	if _, err := rt.Probe(context.Background(), api.ProbeRequest{ProcessID: id, URL: "http://example.com/"}); !errors.As(err, &ce) {
		t.Fatalf("remote target without confirmation: %v", err)
	}
}

func TestRunChecks(t *testing.T) {
	dir := t.TempDir()
	rt := loadYAML(t, dir, `
apps:
  web:
    command: ["`+helperPath+`", "http-server", "200"]
    checks:
      - name: hello
        url: /hello
        expect_contains: ok /hello
      - name: created
        url: /x
        expect_status: 201
`)
	res, err := rt.Start(context.Background(), api.StartRequest{App: "web"})
	if err != nil {
		t.Fatal(err)
	}
	firstLine(t, rt, res.ProcessID, "LISTENING")
	time.Sleep(100 * time.Millisecond)
	out, err := rt.RunChecks(context.Background(), api.RunChecksRequest{App: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Passed != 1 || out.Failed != 1 || out.ProcessID != res.ProcessID {
		t.Fatalf("checks = %+v", out)
	}
	if out.Results[1].Name != "created" || !strings.Contains(strings.Join(out.Results[1].Failures, ";"), "expected status 201") {
		t.Fatalf("failing check = %+v", out.Results[1])
	}
}
