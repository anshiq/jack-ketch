//go:build !windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveJackKetchdCommandSibling(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "jack-ketch")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ResolveJackKetchdCommand(exe)
	if len(got) != 3 || got[0] != exe || got[1] != "jackketchd" || got[2] != "--foreground" {
		t.Fatalf("got %v, want [%s jackketchd --foreground]", got, exe)
	}
	cand := filepath.Join(dir, "jackketchd")
	if err := os.WriteFile(cand, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got = ResolveJackKetchdCommand(exe)
	if len(got) != 2 || got[0] != cand || got[1] != "--foreground" {
		t.Fatalf("got %v, want [%s --foreground]", got, cand)
	}
}
