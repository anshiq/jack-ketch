package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Verify tables exist by querying schema_migrations.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if count != schemaMigration {
		t.Fatalf("expected %d migrations, got %d", schemaMigration, count)
	}
}

func TestOpen_CreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "state.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	db.Close()
	if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
		t.Fatal("expected directory to be created")
	}
}

func TestOpen_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p, err := db.CreateProject("demo")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := db.CreateWorkspaceFull(p.ID, dir, 1, 2, "", false, ""); err != nil {
		t.Fatalf("CreateWorkspaceFull: %v", err)
	}
	db.Close()

	// Reopen: migrations must not duplicate, data must survive.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer db2.Close()
	var count int
	if err := db2.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if count != schemaMigration {
		t.Fatalf("expected %d migrations after reopen, got %d", schemaMigration, count)
	}
	got, err := db2.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "demo" {
		t.Fatalf("name = %q", got.Name)
	}
}

func TestULIDMonotonicPrefix(t *testing.T) {
	a := newULID()
	b := newULID()
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("ULID length = %d/%d, want 26", len(a), len(b))
	}
	if a == b {
		t.Fatal("ULIDs must be unique")
	}
}

func TestResolveMovedAndGit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	p, _ := db.CreateProject("p")
	ws, _ := db.CreateWorkspaceFull(p.ID, "/nonexistent/old", 111, 222, "/git/common", false, "")
	if err := db.AddFingerprint(p.ID, "root_commit", "abc123", false); err != nil {
		t.Fatalf("AddFingerprint: %v", err)
	}
	if _, err := db.FindWorkspaceByDevIno(111, 222); err != nil {
		t.Fatalf("FindByDevIno: %v", err)
	}
	sibs, err := db.FindWorkspacesByGitCommon("/git/common")
	if err != nil || len(sibs) != 1 {
		t.Fatalf("FindByGitCommon = %v, %v", len(sibs), err)
	}
	if err := db.UpdateWorkspacePath(ws.ID, "/new/path", 111, 222, "/git/common"); err != nil {
		t.Fatalf("UpdatePath: %v", err)
	}
	got, _ := db.GetWorkspace(ws.ID)
	if got.Path != "/new/path" {
		t.Fatalf("path = %q", got.Path)
	}
	matched, err := db.GetProjectByFingerprints([]Fingerprint{{Kind: "root_commit", Value: "abc123"}})
	if err != nil || len(matched) != 1 {
		t.Fatalf("fingerprint match = %v, %v", len(matched), err)
	}
}

func TestForgottenProjectWorkspacesDoNotResolve(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, _ := db.CreateProject("gone")
	if _, err := db.CreateWorkspaceFull(p.ID, "/work/gone", 5, 6, "/git/gone", false, ""); err != nil {
		t.Fatal(err)
	}
	if ws, err := db.ResolveWorkspace("/work/gone"); err != nil || ws == nil {
		t.Fatalf("live project must resolve: %v", err)
	}
	if err := db.ForgetProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if ws, _ := db.ResolveWorkspace("/work/gone"); ws != nil {
		t.Errorf("forgotten project's path still resolves to %s", ws.ID)
	}
	if ws, _ := db.FindWorkspaceByDevIno(5, 6); ws != nil {
		t.Errorf("forgotten project's inode still resolves to %s", ws.ID)
	}
	if sibs, _ := db.FindWorkspacesByGitCommon("/git/gone"); len(sibs) != 0 {
		t.Errorf("forgotten project's git common dir still resolves: %d", len(sibs))
	}
}

func TestMoveCredentials(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	from, _ := db.CreateProject("from")
	to, _ := db.CreateProject("to")
	put := func(pid, id, name string, def bool, apps string) {
		t.Helper()
		if err := db.PutCredential(&CredentialRow{ID: id, ProjectID: pid, Name: name, IsDefault: def, AppsJSON: apps, FieldsJSON: "[]", RecipeJSON: "{}", JarJSON: "[]"}); err != nil {
			t.Fatal(err)
		}
	}
	put(to.ID, "c_to", "shared", true, "[]")
	put(from.ID, "c1", "shared", false, "[]")
	put(from.ID, "c2", "admin", true, `["old"]`)
	moved, skipped, err := db.MoveCredentials(from.ID, to.ID, map[string]string{"old": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if moved != 1 || skipped != 1 {
		t.Fatalf("moved=%d skipped=%d", moved, skipped)
	}
	got, err := db.GetCredential(to.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if got.IsDefault {
		t.Error("moved profile must not steal the parent's default")
	}
	if got.AppsJSON != `["new"]` {
		t.Errorf("apps = %s", got.AppsJSON)
	}
}

func TestWorkspacePathIsReusableAfterForgetOrMissing(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old, _ := db.CreateProject("old")
	oldWS, err := db.CreateWorkspace(old.ID, "/work/reuse")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ForgetProject(old.ID); err != nil {
		t.Fatal(err)
	}
	next, _ := db.CreateProject("next")
	nextWS, err := db.CreateWorkspace(next.ID, "/work/reuse")
	if err != nil {
		t.Fatalf("forgotten project must not block reuse of its folder: %v", err)
	}
	if got, _ := db.ResolveWorkspace("/work/reuse"); got == nil || got.ID != nextWS.ID {
		t.Fatalf("path resolves to %v, want %s", got, nextWS.ID)
	}
	if kept, err := db.GetWorkspace(oldWS.ID); err != nil || kept == nil {
		t.Fatalf("history row must be kept: %v", err)
	}
	if err := db.MarkWorkspaceMissing(nextWS.ID); err != nil {
		t.Fatal(err)
	}
	third, _ := db.CreateProject("third")
	if _, err := db.CreateWorkspace(third.ID, "/work/reuse"); err != nil {
		t.Fatalf("missing workspace must not block reuse of its folder: %v", err)
	}
}
