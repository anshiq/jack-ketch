package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jack-ketch/internal/config"
	"jack-ketch/internal/logs"
)

func isolateCLI(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, kv := range [][2]string{
		{"HOME", home},
		{"USERPROFILE", home},
		{"XDG_RUNTIME_DIR", filepath.Join(home, "run")},
		{"XDG_DATA_HOME", filepath.Join(home, "data")},
		{"XDG_CONFIG_HOME", filepath.Join(home, "config")},
		{"XDG_STATE_HOME", filepath.Join(home, "state")},
		{"XDG_CACHE_HOME", filepath.Join(home, "cache")},
	} {
		t.Setenv(kv[0], kv[1])
	}
	return home
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	dir, _ := os.Getwd()
	root := newRootCmd(nil, config.EmptyLoaded(dir), nil)
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestIntegrateLegacyClaudeWrite(t *testing.T) {
	isolateCLI(t)
	t.Setenv("JACK_KETCH_BIN", "/opt/ar/jack-ketch")
	proj := t.TempDir()
	t.Chdir(proj)

	out, err := runCLI(t, "integrate", "claude", "--write", "--yes")
	if err != nil {
		t.Fatalf("integrate claude --write: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(proj, ".mcp.json"))
	if err != nil {
		t.Fatalf(".mcp.json not written: %v\n%s", err, out)
	}
	if !strings.Contains(string(data), `"jack-ketch"`) {
		t.Fatalf(".mcp.json missing jack-ketch entry:\n%s", data)
	}
	if !strings.Contains(string(data), `"serve"`) {
		t.Fatalf(".mcp.json missing serve args:\n%s", data)
	}
}

func TestIntegrateInstallCursorUserScope(t *testing.T) {
	home := isolateCLI(t)
	t.Setenv("JACK_KETCH_BIN", "/opt/ar/jack-ketch")

	out, err := runCLI(t, "integrate", "install", "cursor", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("integrate install cursor: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(home, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatalf(".cursor/mcp.json not written: %v\n%s", err, out)
	}
	if !strings.Contains(string(data), "jack-ketch") {
		t.Fatalf(".cursor/mcp.json missing jack-ketch entry:\n%s", data)
	}
	if !strings.Contains(string(data), "serve") {
		t.Fatalf(".cursor/mcp.json missing serve args:\n%s", data)
	}
}

func TestIntegrateListJSON(t *testing.T) {
	isolateCLI(t)
	proj := t.TempDir()
	t.Chdir(proj)

	out, err := runCLI(t, "integrate", "list", "--json")
	if err != nil {
		t.Fatalf("integrate list --json: %v\n%s", err, out)
	}
	for _, want := range []string{`"claude-code"`, `"cursor"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("list --json missing %s:\n%s", want, out)
		}
	}
}

func TestIntegrateSkillLegacy(t *testing.T) {
	home := isolateCLI(t)
	t.Setenv("PATH", "")

	out, err := runCLI(t, "integrate", "skill", "--write")
	if err != nil {
		t.Fatalf("integrate skill --write: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "jack-ketch-ready")); err != nil {
		t.Fatalf("claude skills not installed: %v\n%s", err, out)
	}
}

func TestServeFlagsFromEnv(t *testing.T) {
	t.Setenv("JACK_KETCH_HARNESS", "codex")
	t.Setenv("JACK_KETCH_MAX_WAIT_MS", "7000")
	t.Setenv("JACK_KETCH_TOOLSET", "")
	t.Setenv("JACK_KETCH_HTTP_TOKEN", "")

	opts := fillServeEnv(serveOptions{})
	if opts.harness != "codex" {
		t.Fatalf("harness = %q, want codex", opts.harness)
	}
	if opts.maxWaitMS != 7000 {
		t.Fatalf("maxWaitMS = %d, want 7000", opts.maxWaitMS)
	}
}

func TestStringList(t *testing.T) {
	var l stringList
	if got := l.String(); got != "[]" {
		t.Fatalf("String() of empty list = %q, want %q", got, "[]")
	}
	for _, v := range []string{"A=1", "B=2", "C=3"} {
		if err := l.Set(v); err != nil {
			t.Fatalf("Set(%q): %v", v, err)
		}
	}
	want := []string{"A=1", "B=2", "C=3"}
	if len(l) != len(want) {
		t.Fatalf("len = %d, want %d", len(l), len(want))
	}
	for i := range want {
		if l[i] != want[i] {
			t.Fatalf("l[%d] = %q, want %q", i, l[i], want[i])
		}
	}
	if got := l.String(); got != "[A=1 B=2 C=3]" {
		t.Fatalf("String() = %q, want %q", got, "[A=1 B=2 C=3]")
	}
}

func TestParseRunArgs(t *testing.T) {
	opts, err := parseRunArgs([]string{
		"--app", "web",
		"--workdir", "/tmp/w",
		"--env", "A=1",
		"--env", "B=2",
		"--env-file", "x.env",
		"cmd", "arg1", "arg2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.app != "web" || opts.workdir != "/tmp/w" {
		t.Fatalf("app/workdir = %q/%q, want web//tmp/w", opts.app, opts.workdir)
	}
	if len(opts.env) != 2 || opts.env[0] != "A=1" || opts.env[1] != "B=2" {
		t.Fatalf("env = %v, want [A=1 B=2]", []string(opts.env))
	}
	if opts.envFile != "x.env" {
		t.Fatalf("envFile = %q, want x.env", opts.envFile)
	}
	if opts.command != "cmd" || len(opts.args) != 2 || opts.args[0] != "arg1" || opts.args[1] != "arg2" {
		t.Fatalf("command/args = %q/%v, want cmd/[arg1 arg2]", opts.command, opts.args)
	}
}

func TestParseRunArgsRejectsBadFlag(t *testing.T) {
	if _, err := parseRunArgs([]string{"--no-such-flag"}); err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
}

// TestDrainLogsPrintsPendingOnce exercises the shared drain helper: given a
// cursor that has already consumed some entries (simulating what the tail tick
// loop printed), it prints exactly the remaining pending entries once, advances
// the cursor, and prints nothing on a second call.
func TestDrainLogsPrintsPendingOnce(t *testing.T) {
	pl := logs.NewProcessLogs(1000, nil)
	pl.Append(logs.StreamStdout, "already printed by tail")
	pl.Append(logs.StreamStdout, "server listening on http://127.0.0.1:8080")
	pl.Append(logs.StreamStderr, "shutting down")

	next := uint64(1)

	var buf bytes.Buffer
	drainLogs(pl, &buf, &next)
	want := "[stdout] server listening on http://127.0.0.1:8080\n[stderr] shutting down\n"
	if buf.String() != want {
		t.Fatalf("drainLogs output = %q, want %q", buf.String(), want)
	}
	if next != 3 {
		t.Fatalf("cursor after drain = %d, want 3", next)
	}

	buf.Reset()
	drainLogs(pl, &buf, &next)
	if buf.String() != "" {
		t.Fatalf("second drain printed %q, want nothing (no double-print)", buf.String())
	}
}

func TestShellCommandMissingTarget(t *testing.T) {
	isolateCLI(t)
	err := ShellCommand(nil, config.EmptyLoaded(t.TempDir()), nil)
	if err == nil {
		t.Fatal("expected usage error for missing target, got nil")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Fatalf("error = %v, want usage error", err)
	}
}

func TestParseShellArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantShell string
		wantPID   int
		wantPos   []string
		wantErr   string
	}{
		{
			name:      "flag after target",
			args:      []string{"proc_abc", "--shell", "/bin/sh"},
			wantShell: "/bin/sh",
			wantPos:   []string{"proc_abc"},
		},
		{
			name:      "flag before target",
			args:      []string{"--shell", "/bin/sh", "proc_abc"},
			wantShell: "/bin/sh",
			wantPos:   []string{"proc_abc"},
		},
		{
			name:      "inline double dash",
			args:      []string{"proc_abc", "--shell=/bin/sh"},
			wantShell: "/bin/sh",
			wantPos:   []string{"proc_abc"},
		},
		{
			name:      "inline single dash",
			args:      []string{"-shell=/bin/bash", "proc_abc"},
			wantShell: "/bin/bash",
			wantPos:   []string{"proc_abc"},
		},
		{
			name:      "single dash space separated",
			args:      []string{"-shell", "/bin/sh", "proc_abc"},
			wantShell: "/bin/sh",
			wantPos:   []string{"proc_abc"},
		},
		{
			name:      "multiple targets still returned",
			args:      []string{"proc_a", "proc_b", "--shell", "/bin/sh"},
			wantShell: "/bin/sh",
			wantPos:   []string{"proc_a", "proc_b"},
		},
		{
			name:      "pid space separated double dash",
			args:      []string{"--pid", "1234", "--shell", "/bin/sh"},
			wantShell: "/bin/sh",
			wantPID:   1234,
		},
		{
			name:    "pid space separated single dash",
			args:    []string{"-pid", "1234"},
			wantPID: 1234,
		},
		{
			name:    "pid equals form",
			args:    []string{"--pid=1234"},
			wantPID: 1234,
		},
		{
			name:    "pid after target still parses both",
			args:    []string{"web", "--pid", "1234"},
			wantPID: 1234,
			wantPos: []string{"web"},
		},
		{
			name:    "empty args",
			args:    nil,
			wantPos: []string{},
		},
		{
			name:    "unknown flag",
			args:    []string{"proc_abc", "--bogus"},
			wantErr: `unknown flag "--bogus"`,
		},
		{
			name:    "help treated as unknown flag",
			args:    []string{"--help"},
			wantErr: `unknown flag "--help"`,
		},
		{
			name:    "shell flag at end without value",
			args:    []string{"proc_abc", "--shell"},
			wantErr: "--shell requires an argument",
		},
		{
			name:    "single dash shell flag at end without value",
			args:    []string{"-shell"},
			wantErr: "--shell requires an argument",
		},
		{
			name:    "pid flag at end without value",
			args:    []string{"--pid"},
			wantErr: "--pid requires an argument",
		},
		{
			name:    "single dash pid flag at end without value",
			args:    []string{"-pid"},
			wantErr: "--pid requires an argument",
		},
		{
			name:    "invalid pid value",
			args:    []string{"--pid=abc"},
			wantErr: `invalid --pid value "abc"`,
		},
		{
			name:    "invalid pid value space form",
			args:    []string{"--pid", "abc"},
			wantErr: `invalid --pid value "abc"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotShell, gotPID, gotPos, err := parseShellArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got shell=%q pid=%d pos=%v", tt.wantErr, gotShell, gotPID, gotPos)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if gotShell != tt.wantShell {
				t.Fatalf("shell = %q, want %q", gotShell, tt.wantShell)
			}
			if gotPID != tt.wantPID {
				t.Fatalf("pid = %d, want %d", gotPID, tt.wantPID)
			}
			if len(gotPos) != len(tt.wantPos) {
				t.Fatalf("pos = %v, want %v", gotPos, tt.wantPos)
			}
			for i := range tt.wantPos {
				if gotPos[i] != tt.wantPos[i] {
					t.Fatalf("pos = %v, want %v", gotPos, tt.wantPos)
				}
			}
		})
	}
}

// TestShellCommandRejectsPidWithTarget verifies the combination check in
// ShellCommand: --pid and a positional target are mutually exclusive. The
// check fires before any runtime is constructed.
func TestShellCommandRejectsPidWithTarget(t *testing.T) {
	isolateCLI(t)
	err := ShellCommand([]string{"--pid", "1234", "web"}, config.EmptyLoaded(t.TempDir()), nil)
	if err == nil {
		t.Fatal("expected error combining --pid with a target")
	}
	if !strings.Contains(err.Error(), "cannot combine --pid with a target") {
		t.Fatalf("error = %v, want combination error", err)
	}
}

func TestShellTarget(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		wantProcID string
		wantApp    string
	}{
		{
			name:       "proc id",
			target:     "proc_abc",
			wantProcID: "proc_abc",
			wantApp:    "",
		},
		{
			name:       "app name",
			target:     "web",
			wantProcID: "",
			wantApp:    "web",
		},
		{
			name:       "empty target",
			target:     "",
			wantProcID: "",
			wantApp:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procID, app := shellTarget(tt.target)
			if procID != tt.wantProcID || app != tt.wantApp {
				t.Fatalf("shellTarget(%q) = (%q, %q), want (%q, %q)", tt.target, procID, app, tt.wantProcID, tt.wantApp)
			}
		})
	}
}
