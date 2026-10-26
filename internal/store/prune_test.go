package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPruneAuditAndEvents(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.AppendAudit("", "", "", "", "old_act", "", "ok", 1); err != nil {
		t.Fatal(err)
	}
	if err := db.AppendAudit("", "", "", "", "new_act", "", "ok", 1); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour).Unix()
	if _, err := db.Exec("UPDATE audit SET ts=? WHERE action='old_act'", old); err != nil {
		t.Fatal(err)
	}
	n, err := db.PruneAudit(time.Now().Add(-24 * time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("PruneAudit = %d, %v; want 1", n, err)
	}
	rows, err := db.ListAudit("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Action != "new_act" {
		t.Fatalf("audit rows = %+v", rows)
	}

	oldTS := time.Now().Add(-48 * time.Hour).UnixNano()
	newTS := time.Now().UnixNano()
	if _, err := db.AppendEvent(oldTS, "process.exited", "", "", "", "", "", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendEvent(newTS, "process.started", "", "", "", "", "", "{}"); err != nil {
		t.Fatal(err)
	}
	n, err = db.PruneEvents(time.Now().Add(-24 * time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("PruneEvents = %d, %v; want 1", n, err)
	}
	events, err := db.ListEvents("", "", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "process.started" {
		t.Fatalf("event rows = %+v", events)
	}
}
