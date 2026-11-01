package diagnose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exitCode(n int) *int { return &n }

func analyze(t *testing.T, dir string, env []string, lines ...string) *Input {
	t.Helper()
	in := &Input{ProcessID: "p1", Status: "exited", ExitCode: exitCode(1), WorkDir: dir, ProjectDir: dir, Env: env}
	for _, l := range lines {
		in.Lines = append(in.Lines, Line{Stream: "stderr", Text: l})
	}
	return in
}

func TestCategories(t *testing.T) {
	cases := []struct {
		name     string
		lines    []string
		category string
		actor    string
	}{
		{"port", []string{"Error: listen EADDRINUSE: address already in use :::3000"}, CatPortInUse, ActorAgent},
		{"postgres down", []string{"Error: connect ECONNREFUSED 127.0.0.1:5432"}, CatServiceDown, ActorAgentApproval},
		{"remote down", []string{"dial tcp 10.0.4.7:6379: connect: connection refused"}, CatServiceDown, ActorUser},
		{"python module", []string{"Traceback (most recent call last):", "  File \"app.py\", line 1, in <module>", "ModuleNotFoundError: No module named 'yaml'"}, CatMissingDep, ActorAgentApproval},
		{"go sum", []string{"main.go:4:2: missing go.sum entry for module providing package github.com/x/y (imported by app)"}, CatMissingDep, ActorAgentApproval},
		{"creds", []string{"error: password authentication failed for user \"app\""}, CatInvalidCreds, ActorUser},
		{"migration", []string{"error: relation \"users\" does not exist"}, CatMigration, ActorAgentApproval},
		{"tool", []string{"sh: 1: vite: not found"}, CatMissingDep, ActorAgentApproval},
		{"unknown tool", []string{"exec: \"protoc\": executable file not found in $PATH"}, CatMissingTool, ActorAgentApproval},
		{"engine", []string{"error app@1.0.0: The engine \"node\" is incompatible with this module. Expected version \">=20\". Got \"18.19.0\""}, CatToolchain, ActorAgentApproval},
		{"privileged", []string{"Error: listen EACCES: permission denied 0.0.0.0:80"}, CatPermission, ActorAgent},
		{"code", []string{"src/index.ts(12,5): error TS2304: Cannot find name 'foo'."}, CatCodeError, ActorAgent},
		{"go compile", []string{"./main.go:10:2: undefined: handler"}, CatCodeError, ActorAgent},
		{"oom", []string{"FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory"}, CatOutOfMemory, ActorAgent},
		{"pydantic", []string{"pydantic_core._pydantic_core.ValidationError: 1 validation error for Settings", "database_url", "  Field required [type=missing, input_value={}, input_type=dict]"}, CatMissingEnv, ActorUser},
		{"local import", []string{"Error: Cannot find module './routes/users'"}, CatCodeError, ActorAgent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.name == "tool" {
				_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"devDependencies":{"vite":"^5"}}`), 0o644)
			}
			d := Analyze(*analyze(t, dir, nil, c.lines...))
			if d.Category != c.category || d.Actor != c.actor {
				t.Fatalf("got %s/%s, want %s/%s: %+v", d.Category, d.Actor, c.category, c.actor, d.Issues)
			}
			if d.Guidance == "" || d.Summary == "" {
				t.Fatalf("missing guidance/summary: %+v", d)
			}
		})
	}
}

func TestServiceContainerHintUsesEnvCredentials(t *testing.T) {
	dir := t.TempDir()
	d := Analyze(*analyze(t, dir, []string{"DATABASE_URL=postgres://shop:s3cret@localhost:5432/shopdb"}, "Error: connect ECONNREFUSED 127.0.0.1:5432"))
	if d.Service == nil || d.Service.Name != "postgres" || d.Service.Port != 5432 {
		t.Fatalf("service = %+v", d.Service)
	}
	run := d.Service.RunHint
	for _, want := range []string{"POSTGRES_USER=shop", "POSTGRES_PASSWORD=s3cret", "POSTGRES_DB=shopdb", "-p 5432:5432", "postgres:16"} {
		if !strings.Contains(run, want) {
			t.Fatalf("run hint %q missing %q", run, want)
		}
	}
	var needsApproval bool
	for _, f := range d.Issues[0].Fixes {
		if f.Kind == "container" && f.NeedsApproval {
			needsApproval = true
		}
	}
	if !needsApproval {
		t.Fatalf("container fix must need approval: %+v", d.Issues[0].Fixes)
	}
}

func TestProjectAppPreferredOverContainer(t *testing.T) {
	in := *analyze(t, t.TempDir(), nil, "Error: Redis connection to localhost:6379 failed - connect ECONNREFUSED 127.0.0.1:6379")
	in.ServiceApps = map[string]string{"redis": "cache"}
	d := Analyze(in)
	if d.Actor != ActorAgent || d.Service.ProjectApp != "cache" {
		t.Fatalf("diagnosis = %+v service=%+v", d, d.Service)
	}
}

func TestPortOwnerNamed(t *testing.T) {
	in := *analyze(t, t.TempDir(), nil, "Error: listen EADDRINUSE: address already in use 127.0.0.1:8080")
	in.PortOwner = func(p int) string {
		if p == 8080 {
			return "process proc_1 (app api)"
		}
		return ""
	}
	d := Analyze(in)
	if !strings.Contains(d.Summary, "proc_1") {
		t.Fatalf("summary = %s", d.Summary)
	}
}

func TestExampleGapsWhenUnknown(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, ".env.example"), []byte("STRIPE_KEY=sk_test_x # from the Stripe dashboard\nPORT=3000\n"), 0o644)
	d := Analyze(*analyze(t, dir, []string{"PORT=3000"}, "something odd happened"))
	if d.Category != CatMissingEnv || len(d.MissingEnv) != 1 || d.MissingEnv[0].Key != "STRIPE_KEY" || d.MissingEnv[0].Hint != "from the Stripe dashboard" {
		t.Fatalf("diagnosis = %+v %+v", d, d.MissingEnv)
	}
}

func TestHealthyProcessHasNoIssue(t *testing.T) {
	in := Input{ProcessID: "p", Status: "running", Lines: []Line{{Stream: "stdout", Text: "listening on :3000"}}}
	d := Analyze(in)
	if d.Failing || d.Category != CatNone {
		t.Fatalf("diagnosis = %+v", d)
	}
}

func TestANSIStripped(t *testing.T) {
	d := Analyze(*analyze(t, t.TempDir(), nil, "\x1b[31mError: listen EADDRINUSE: address already in use :::4000\x1b[0m"))
	if d.Category != CatPortInUse || !strings.Contains(d.Summary, "4000") {
		t.Fatalf("diagnosis = %+v", d)
	}
}
