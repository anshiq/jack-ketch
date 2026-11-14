//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jack-ketch/internal/store"
)

func writeExe(t *testing.T, path, version string) {
	t.Helper()
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte("#!/bin/sh\necho "+version+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateStatusDetectsReplacedBinary(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := NewWithOptions(db, Options{DataDir: dir, Version: "v1.0.0"})
	t.Cleanup(func() { e.Close() })

	exe := filepath.Join(dir, "jackketchd")
	writeExe(t, exe, "v1.0.0")
	st, _ := os.Stat(exe)
	e.exe.path, e.exe.bootMod, e.exe.bootSize, e.exe.bootIno = exe, st.ModTime(), st.Size(), fileIno(st)

	if got := e.UpdateStatus(); got.Pending {
		t.Fatalf("unchanged binary reported pending: %+v", got)
	}

	time.Sleep(10 * time.Millisecond)
	writeExe(t, exe, "v1.1.0")
	got := e.UpdateStatus()
	if !got.Pending || got.InstalledVersion != "v1.1.0" || got.RunningVersion != "v1.0.0" {
		t.Fatalf("replaced binary not detected: %+v", got)
	}
	if !strings.Contains(got.Notice, "v1.1.0") || !strings.Contains(got.Notice, "no managed processes") {
		t.Fatalf("unexpected notice %q", got.Notice)
	}

	writeExe(t, exe, "v1.0.0")
	if got := e.UpdateStatus(); got.Pending {
		t.Fatalf("same version reinstalled should not be pending: %+v", got)
	}
}

func TestUpdateNoticeCountsProcesses(t *testing.T) {
	if n := UpdateNotice("v1", "v2", 3); !strings.Contains(n, "stops all 3 running managed processes") {
		t.Fatalf("notice = %q", n)
	}
	if n := UpdateNotice("v1", "", 1); !strings.Contains(n, "A new jack-ketch version") || !strings.Contains(n, "the 1 running managed process") {
		t.Fatalf("notice = %q", n)
	}
}
