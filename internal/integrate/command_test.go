//go:build !windows

package integrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveServerCommandPortable(t *testing.T) {
	setTestEnv(t)
	cmd, err := ResolveServerCommand(true)
	if err != nil || cmd != "jack-ketch" {
		t.Fatalf("cmd=%q err=%v", cmd, err)
	}
}

func TestResolveServerCommandEnvWins(t *testing.T) {
	setTestEnv(t)
	cmd, err := ResolveServerCommand(false)
	if err != nil || cmd != "/opt/jack-ketch/bin/jack-ketch" {
		t.Fatalf("cmd=%q err=%v", cmd, err)
	}
}

func TestResolveServerCommandFromPath(t *testing.T) {
	setTestEnv(t)
	t.Setenv("JACK_KETCH_BIN", "")
	dir := t.TempDir()
	bin := filepath.Join(dir, "jack-ketch")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	cmd, err := ResolveServerCommand(false)
	if err != nil {
		t.Fatal(err)
	}
	if cmd != bin {
		t.Fatalf("cmd = %q, want %q", cmd, bin)
	}
}
