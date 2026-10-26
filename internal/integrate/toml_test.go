package integrate

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func tomlSpec() ServerSpec {
	return ServerSpec{Command: "/opt/jack-ketch/bin/jack-ketch", Args: []string{"serve", "--harness", "codex"}}
}

func isOurHeaderLine(l string) bool {
	return strings.TrimSpace(l) == "[mcp_servers.jack-ketch]"
}

func TestTOMLUpsertEmpty(t *testing.T) {
	out, err := tomlUpsert(nil, codexBlock(tomlSpec()))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "[mcp_servers.jack-ketch]") {
		t.Fatalf("missing header: %s", s)
	}
	if !strings.Contains(s, `command = "/opt/jack-ketch/bin/jack-ketch"`) {
		t.Fatalf("missing command: %s", s)
	}
	if !strings.Contains(s, "tool_timeout_sec = 660") {
		t.Fatalf("missing timeout: %s", s)
	}
}

func TestTOMLPreservesOtherTable(t *testing.T) {
	src := []byte("[mcp_servers.other]\ncommand = \"x\"\n")
	out, err := tomlUpsert(src, codexBlock(tomlSpec()))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "[mcp_servers.other]") {
		t.Fatalf("other table lost: %s", s)
	}
	if !strings.Contains(s, "[mcp_servers.jack-ketch]") {
		t.Fatalf("our block missing: %s", s)
	}
}

func TestTOMLReplacesOursAndEnv(t *testing.T) {
	spec := tomlSpec()
	spec.Env = map[string]string{"JACK_KETCH_BIN": "/opt/jack-ketch/bin/jack-ketch"}
	src := []byte("[mcp_servers.other]\ncommand = \"x\"\n\n[mcp_servers.jack-ketch]\ncommand = \"old\"\n\n[mcp_servers.jack-ketch.env]\nFOO = \"bar\"\n")
	out, err := tomlUpsert(src, codexBlock(spec))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(out), "\n")
	headers := 0
	envHeaders := 0
	for _, l := range lines {
		if isOurHeaderLine(l) {
			headers++
		}
		if strings.TrimSpace(l) == "[mcp_servers.jack-ketch.env]" {
			envHeaders++
		}
	}
	if headers != 1 || envHeaders != 1 {
		t.Fatalf("headers=%d envHeaders=%d\n%s", headers, envHeaders, out)
	}
	if strings.Contains(string(out), "old") || strings.Contains(string(out), "FOO") {
		t.Fatalf("stale content kept: %s", out)
	}
	if !strings.Contains(string(out), "JACK_KETCH_BIN") {
		t.Fatalf("env block missing: %s", out)
	}
}

func TestTOMLRemove(t *testing.T) {
	src := []byte("[mcp_servers.other]\ncommand = \"x\"\n\n[mcp_servers.jack-ketch]\ncommand = \"old\"\n\n[mcp_servers.jack-ketch.env]\nFOO = \"bar\"\n")
	out, err := tomlRemove(src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "jack-ketch") {
		t.Fatalf("block not removed: %s", out)
	}
	if !strings.Contains(string(out), "[mcp_servers.other]") {
		t.Fatalf("other table lost: %s", out)
	}
}

func TestTOMLUnmanagedInline(t *testing.T) {
	src := []byte("[mcp_servers]\njack-ketch = {command = \"x\"}\n")
	if _, err := tomlUpsert(src, codexBlock(tomlSpec())); !errors.Is(err, errUnmanagedTOML) {
		t.Fatalf("err = %v", err)
	}
}

func TestTOMLIdempotent(t *testing.T) {
	out1, err := tomlUpsert(nil, codexBlock(tomlSpec()))
	if err != nil {
		t.Fatal(err)
	}
	out2, err := tomlUpsert(out1, codexBlock(tomlSpec()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out1, out2) {
		t.Fatalf("not idempotent:\n%s\n---\n%s", out1, out2)
	}
}

func TestTOMLEntryParsesEscapes(t *testing.T) {
	spec := ServerSpec{Command: "/bin/jack-ketch", Args: []string{`a"b`, "serve"}}
	block := codexBlock(spec)
	e := tomlEntry(strings.Split(block, "\n"))
	if e.Command != spec.Command {
		t.Fatalf("command = %q", e.Command)
	}
	if len(e.Args) != 2 || e.Args[0] != `a"b` || e.Args[1] != "serve" {
		t.Fatalf("args = %#v", e.Args)
	}
}
