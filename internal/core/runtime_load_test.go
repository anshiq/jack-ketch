package core

import (
	"testing"
	"time"
)

func TestRuntimeLoadWithPendingAutoSyncReturns(t *testing.T) {
	db, pr, wsPath := testSetup(t)
	writePkg(t, wsPath, `{"scripts":{"dev":"next dev"}}`)
	done := make(chan error, 1)
	go func() {
		_, err := pr.Runtime()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runtime load: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("deadlock: Runtime() did not return while ensure-config auto-synced")
	}
	if pr.LoadedRuntime() == nil {
		t.Fatal("runtime not loaded")
	}
	data, _, err := db.GetConfig(pr.projectID, "project", "")
	if err != nil || len(data) == 0 {
		t.Fatalf("auto config not written: %q %v", data, err)
	}
}

func TestRuntimeReloadAfterConfigEditReturns(t *testing.T) {
	_, pr, wsPath := testSetup(t)
	writePkg(t, wsPath, `{"scripts":{"dev":"next dev"}}`)
	if _, err := pr.Runtime(); err != nil {
		t.Fatalf("first load: %v", err)
	}
	pr.shutdown()
	writePkg(t, wsPath, `{"scripts":{"dev":"next dev --turbopack"}}`)
	due := make(chan error, 1)
	go func() {
		_, err := pr.Runtime()
		due <- err
	}()
	select {
	case err := <-due:
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("deadlock: Runtime() did not return while auto-syncing an edited config")
	}
}
