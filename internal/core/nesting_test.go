package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	iruntime "jack-ketch/internal/runtime"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

const nestParentYAML = `version: 3
apps:
  api:
    type: spring-boot
    workdir: svc/api
    command: [./mvnw, spring-boot:run]
    default_mode: default
    modes:
      default:
        description: direnv run
        command: [direnv, exec, ., ./mvnw, spring-boot:run]
  web:
    type: node
    workdir: svc/web
    command: [npm, run, dev]
`

const nestChildYAML = `version: 3
apps:
  api:
    type: spring-boot
    workdir: ""
    command: [./mvnw, spring-boot:run]
    description: child description
    modes:
      debug:
        command: [./mvnw, spring-boot:run, -Ddebug]
  worker:
    type: generic
    workdir: jobs
    command: [./run-worker.sh]
  web:
    type: node
    workdir: ../web
    command: [bun, run, dev]
`

func nestFixture(t *testing.T) (*Engine, *store.DB, string, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := NewWithOptions(db, Options{DataDir: dir})
	t.Cleanup(func() { e.Close() })

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	apiDir := filepath.Join(root, "svc", "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "svc", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	parent, _ := db.CreateProject("parent")
	pws, err := db.CreateWorkspace(parent.ID, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.PutConfigTx(parent.ID, "project", "", nestParentYAML, "sha", true, "", "test", "", "seed"); err != nil {
		t.Fatal(err)
	}
	child, _ := db.CreateProject("child")
	if _, err := db.CreateWorkspace(child.ID, apiDir); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PutConfigTx(child.ID, "project", "", nestChildYAML, "sha2", true, "", "test", "", "seed"); err != nil {
		t.Fatal(err)
	}
	return e, db, parent.ID, child.ID, pws.ID, apiDir
}

func TestNestedProjectsDetectsOverlap(t *testing.T) {
	e, _, parentID, childID, _, _ := nestFixture(t)
	list, err := e.NestedProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("nested = %d, want 1", len(list))
	}
	n := list[0]
	if n.Child.ProjectID != childID || n.Parent.ProjectID != parentID {
		t.Fatalf("wrong pair: %+v", n)
	}
	status := map[string]string{}
	for _, a := range n.Apps {
		status[a.Name] = a.Status
	}
	if status["api"] != "duplicate" || status["worker"] != "unique" || status["web"] != "conflict" {
		t.Errorf("statuses = %v", status)
	}
}

func TestNestedProjectsIgnoresPlainContainment(t *testing.T) {
	e, db, _, childID, _, apiDir := nestFixture(t)
	if _, err := db.PutConfigTx(childID, "project", "", "version: 3\napps: {}\n", "x", true, "", "t", "", "empty"); err != nil {
		t.Fatal(err)
	}
	parents, _ := db.ListProjects()
	for _, p := range parents {
		if p.Name == "parent" {
			if _, err := db.PutConfigTx(p.ID, "project", "", "version: 3\napps:\n  other:\n    workdir: elsewhere\n    command: [x]\n", "y", true, "", "t", "", "narrow"); err != nil {
				t.Fatal(err)
			}
		}
	}
	list, err := e.NestedProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("folder containment without an app in it must not be flagged: %+v (%s)", list, apiDir)
	}
}

func TestMergeNestedProject(t *testing.T) {
	e, db, parentID, childID, _, apiDir := nestFixture(t)
	if err := db.PutCredential(&store.CredentialRow{ID: "cred1", ProjectID: childID, Name: "admin", AppsJSON: `["api"]`, FieldsJSON: "[]", RecipeJSON: "{}", JarJSON: "[]"}); err != nil {
		t.Fatal(err)
	}

	preview, err := e.PreviewNestedMerge(childID, parentID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Applied || preview.Credentials != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	if still, _ := db.GetProject(childID); still == nil || still.DeletedAt != 0 {
		t.Fatal("preview must not forget the child")
	}

	rep, err := e.MergeNestedProject(childID, parentID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Applied || rep.Moved != 1 {
		t.Fatalf("report = %+v", rep)
	}

	yamlOut, _, _ := db.GetConfig(parentID, "project", "")
	out := string(yamlOut)
	for _, want := range []string{"worker:", "workdir: svc/api/jobs", "debug:", "direnv", "description: child description"} {
		if !strings.Contains(out, want) {
			t.Errorf("merged config misses %q\n%s", want, out)
		}
	}
	if strings.Count(out, "api:") != 1 {
		t.Errorf("api must stay a single app\n%s", out)
	}

	live, _ := db.ListProjects()
	for _, p := range live {
		if p.ID == childID {
			t.Error("child project must be forgotten")
		}
	}
	creds, _ := db.ListCredentials(parentID)
	if len(creds) != 1 || creds[0].Name != "admin" {
		t.Errorf("credentials not moved: %+v", creds)
	}

	pid, _, err := e.ResolveWorkspace(apiDir)
	if err != nil {
		t.Fatal(err)
	}
	if string(pid) != parentID {
		t.Errorf("after merge the folder resolves to %s, want parent %s", pid, parentID)
	}
	if again, _ := e.NestedProjects(); len(again) != 0 {
		t.Errorf("nested still reported after merge: %+v", again)
	}
}

func TestMergeRejectsUnrelatedProjects(t *testing.T) {
	e, db, parentID, _, _, _ := nestFixture(t)
	other, _ := db.CreateProject("other")
	if _, err := db.CreateWorkspace(other.ID, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.MergeNestedProject(other.ID, parentID, ""); err == nil {
		t.Fatal("merging a project that is not inside the parent must fail")
	}
}

func TestStartGuardBlocksSameCommandInOtherWorkspace(t *testing.T) {
	e, db, _, childID, parentWS, apiDir := nestFixture(t)
	childWS, err := db.ListWorkspaces(childID)
	if err != nil || len(childWS) != 1 {
		t.Fatal("child workspace missing")
	}
	_, parentRT, err := e.RuntimeFor(parentWS)
	if err != nil {
		t.Fatal(err)
	}
	_, childRT, err := e.RuntimeFor(childWS[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	req := api.StartRequest{Command: "sleep", Args: []string{"30"}, WorkDir: apiDir}
	first, err := parentRT.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parentRT.Stop(context.Background(), first.ProcessID) })

	if _, err := childRT.Start(context.Background(), req); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("duplicate start must be refused, got %v", err)
	}

	other := api.StartRequest{Command: "sleep", Args: []string{"31"}, WorkDir: apiDir}
	res, err := childRT.Start(context.Background(), other)
	if err != nil {
		t.Fatalf("a different command is not a duplicate: %v", err)
	}
	t.Cleanup(func() { _ = childRT.Stop(context.Background(), res.ProcessID) })

	forced, err := childRT.Start(iruntime.WithForce(context.Background()), req)
	if err != nil {
		t.Fatalf("force must bypass the guard: %v", err)
	}
	t.Cleanup(func() { _ = childRT.Stop(context.Background(), forced.ProcessID) })
}
