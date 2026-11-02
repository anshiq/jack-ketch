package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jack-ketch/pkg/client"
)

type dirEntryJSON struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

type dirListingJSON struct {
	Dir       string         `json:"dir"`
	Entries   []dirEntryJSON `json:"entries"`
	Truncated bool           `json:"truncated"`
	Error     string         `json:"error"`
}

func listDirectory(t *testing.T, cl *client.Client, ctx context.Context, req map[string]any) dirListingJSON {
	t.Helper()
	var out dirListingJSON
	if err := cl.Call(ctx, "SystemService", "ListDirectory", req, &out); err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}
	return out
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entryNames(entries []dirEntryJSON) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

func TestListDirectoryIncludesDotfiles(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".env"), "A=1\n")
	writeFile(t, filepath.Join(root, ".env.local"), "B=2\n")
	writeFile(t, filepath.Join(root, ".gitignore"), ".env\n")
	writeFile(t, filepath.Join(root, "README.md"), "# app\n")
	writeFile(t, filepath.Join(root, "config", "app.yaml"), "app: {}\n")
	writeFile(t, filepath.Join(root, "src", "main.go"), "package main\n")

	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got := listDirectory(t, cl, ctx, map[string]any{"baseDir": root})
	want := []string{"config", "src", ".env", ".env.local", ".gitignore", "README.md"}
	if names := entryNames(got.Entries); !equalStrings(names, want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	if got.Truncated {
		t.Fatal("unexpected truncation")
	}
	if !got.Entries[0].IsDir || got.Entries[2].IsDir {
		t.Fatalf("directory flags = %+v", got.Entries)
	}
	if got.Error != "" {
		t.Fatalf("error = %q", got.Error)
	}

	filtered := listDirectory(t, cl, ctx, map[string]any{"baseDir": root, "prefix": ".ENV"})
	if names := entryNames(filtered.Entries); !equalStrings(names, []string{".env", ".env.local"}) {
		t.Fatalf("dotfile filter entries = %v", names)
	}

	dirs := listDirectory(t, cl, ctx, map[string]any{"baseDir": root, "dirsOnly": true})
	if names := entryNames(dirs.Entries); !equalStrings(names, []string{"config", "src"}) {
		t.Fatalf("dirsOnly entries = %v", names)
	}
}

func TestListDirectoryResolvesRelativeAndAbsolute(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "main.go"), "package main\n")

	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rel := listDirectory(t, cl, ctx, map[string]any{"baseDir": root, "dir": "src"})
	if names := entryNames(rel.Entries); !equalStrings(names, []string{"main.go"}) {
		t.Fatalf("relative entries = %v", names)
	}
	if rel.Dir != filepath.Join(root, "src") {
		t.Fatalf("resolved dir = %q, want %q", rel.Dir, filepath.Join(root, "src"))
	}

	abs := listDirectory(t, cl, ctx, map[string]any{"dir": filepath.Join(root, "src")})
	if names := entryNames(abs.Entries); !equalStrings(names, []string{"main.go"}) {
		t.Fatalf("absolute entries = %v", names)
	}
}

func TestListDirectoryTruncatesAndReportsMissing(t *testing.T) {
	eng, done := testEngine(t)
	defer done()
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		writeFile(t, filepath.Join(root, name), "x")
	}

	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got := listDirectory(t, cl, ctx, map[string]any{"baseDir": root, "limit": 2})
	if !got.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if names := entryNames(got.Entries); !equalStrings(names, []string{"a.txt", "b.txt"}) {
		t.Fatalf("truncated entries = %v", names)
	}

	missing := listDirectory(t, cl, ctx, map[string]any{"baseDir": root, "dir": "not-there"})
	if len(missing.Entries) != 0 {
		t.Fatalf("missing entries = %v", missing.Entries)
	}
	if missing.Error == "" {
		t.Fatal("missing directory must carry an error")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
