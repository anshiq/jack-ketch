package project

import (
	"os"
	"path/filepath"
	"testing"
)

type nestFake struct {
	*fakeStore
	dirs map[ID][]string
}

func (n *nestFake) ProjectAppDirs(pid ID, wsPath string) ([]string, error) {
	return n.dirs[pid], nil
}

func newNestFake(t *testing.T) (*nestFake, string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &nestFake{fakeStore: newFakeStore(), dirs: map[ID][]string{}}
	f.proj["proj_parent"] = &Project{ID: "proj_parent", Name: "parent"}
	f.ws["ws_parent"] = &Workspace{ID: "ws_parent", ProjectID: "proj_parent", Path: root, Confirmed: true}
	return f, root, child
}

func TestResolveAttachesToEnclosingProjectThatDefinesTheApp(t *testing.T) {
	f, _, child := newNestFake(t)
	f.dirs["proj_parent"] = []string{child}
	res, err := NewResolver(f).Resolve(child)
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectID != "proj_parent" || res.WorkspaceID != "ws_parent" {
		t.Fatalf("resolved %s/%s, want parent", res.ProjectID, res.WorkspaceID)
	}
	if res.AttachedSubpath != filepath.Join("services", "api") {
		t.Errorf("subpath = %q", res.AttachedSubpath)
	}
	if res.NewlyCreated {
		t.Error("attach must not create a project")
	}
	if len(f.ws) != 1 {
		t.Errorf("workspaces = %d, want 1", len(f.ws))
	}
}

func TestResolveAttachesWhenAppSitsBelowTheFolder(t *testing.T) {
	f, root, _ := newNestFake(t)
	mid := filepath.Join(root, "services")
	f.dirs["proj_parent"] = []string{filepath.Join(mid, "api")}
	res, err := NewResolver(f).Resolve(mid)
	if err != nil {
		t.Fatal(err)
	}
	if res.WorkspaceID != "ws_parent" || res.AttachedSubpath != "services" {
		t.Fatalf("got %s subpath %q", res.WorkspaceID, res.AttachedSubpath)
	}
}

func TestResolveDoesNotAttachWithoutAppCoverage(t *testing.T) {
	f, root, child := newNestFake(t)
	f.dirs["proj_parent"] = []string{root, filepath.Join(root, "other")}
	res, err := NewResolver(f).Resolve(child)
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectID == "proj_parent" || !res.NewlyCreated || res.AttachedSubpath != "" {
		t.Fatalf("unrelated nesting must stay independent: %+v", res)
	}
}

func TestResolveExistingNestedWorkspaceWins(t *testing.T) {
	f, _, child := newNestFake(t)
	f.dirs["proj_parent"] = []string{child}
	f.proj["proj_child"] = &Project{ID: "proj_child", Name: "child"}
	f.ws["ws_child"] = &Workspace{ID: "ws_child", ProjectID: "proj_child", Path: child, Confirmed: true}
	res, err := NewResolver(f).Resolve(child)
	if err != nil {
		t.Fatal(err)
	}
	if res.WorkspaceID != "ws_child" || res.AttachedSubpath != "" {
		t.Fatalf("an already registered folder keeps its project: %+v", res)
	}
}

func TestResolvePicksNearestEnclosingWorkspace(t *testing.T) {
	f, root, child := newNestFake(t)
	mid := filepath.Join(root, "services")
	f.proj["proj_mid"] = &Project{ID: "proj_mid", Name: "mid"}
	f.ws["ws_mid"] = &Workspace{ID: "ws_mid", ProjectID: "proj_mid", Path: mid, Confirmed: true}
	f.dirs["proj_parent"] = []string{child}
	f.dirs["proj_mid"] = []string{child}
	res, err := NewResolver(f).Resolve(child)
	if err != nil {
		t.Fatal(err)
	}
	if res.WorkspaceID != "ws_mid" || res.AttachedSubpath != "api" {
		t.Fatalf("got %s subpath %q", res.WorkspaceID, res.AttachedSubpath)
	}
}

func TestWithin(t *testing.T) {
	cases := []struct {
		root, p string
		want    bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b", "/a/b/c", true},
		{"/a/b", "/a/bc", false},
		{"/a/b", "/a", false},
		{"/a/b", "/x", false},
	}
	for _, c := range cases {
		if got := Within(c.root, c.p); got != c.want {
			t.Errorf("Within(%q,%q) = %v want %v", c.root, c.p, got, c.want)
		}
	}
}
