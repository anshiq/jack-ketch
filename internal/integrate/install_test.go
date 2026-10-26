//go:build !windows

package integrate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func outdatedContent(t *testing.T, h Harness, target Target, project string, data []byte, cmd string) []byte {
	t.Helper()
	spec := ServerSpec{Command: cmd, Args: []string{"serve"}}
	keyPath := expandKeyPath(target.KeyPath, project)
	var out []byte
	var err error
	switch target.Format {
	case FormatJSON, FormatJSONC:
		doc, e := parseJSONC(data)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := doc.upsert(keyPath, ServerName, jsonEntry(h.Dialect, spec)); e != nil {
			t.Fatal(e)
		}
		out, err = finishJSON(doc.bytes(), target.Format)
	case FormatTOML:
		out, err = tomlUpsert(data, codexBlock(spec))
	case FormatGooseYAML:
		out, err = yamlUpsertMap(data, "extensions", ServerName, gooseEntry(spec))
	case FormatContinueYAML:
		out, err = yamlUpsertList(data, "mcpServers", ServerName, continueEntry(spec))
	case FormatContinueBlock:
		out, err = continueBlockFile(spec)
	}
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInstallInspectRemove(t *testing.T) {
	base := setTestEnv(t)
	project := filepath.Join(base, "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	env := currentPathEnv("")
	writeFile(t, filepath.Join(base, "cline", "cline_mcp_settings.json"), []byte("{\n  \"mcpServers\": {}\n}\n"))
	writeFile(t, filepath.Join(env.appData, "Code", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "mcp_settings.json"), []byte("{\n  \"mcpServers\": {}\n}\n"))
	writeFile(t, filepath.Join(base, "home", ".continue", "config.yaml"), []byte("name: x\n"))

	for _, h := range Harnesses() {
		for _, sc := range h.ScopeList() {
			opts := Options{ProjectDir: project}
			if _, err := Install(h.ID, sc, opts); err != nil {
				t.Fatalf("%s/%s install: %v", h.ID, sc, err)
			}
			wantCmd := "/opt/jack-ketch/bin/jack-ketch"
			if h.Scopes[sc].Portable {
				wantCmd = "jack-ketch"
			}
			entries, err := Inspect(h.ID, sc, opts)
			if err != nil {
				t.Fatalf("%s/%s inspect: %v", h.ID, sc, err)
			}
			if len(entries) == 0 {
				t.Fatalf("%s/%s: no entries", h.ID, sc)
			}
			for _, st := range entries {
				if !st.Installed || !st.UpToDate {
					t.Errorf("%s/%s %s: installed=%v upToDate=%v problems=%v", h.ID, sc, st.Path, st.Installed, st.UpToDate, st.Problems)
				}
				if st.Command != wantCmd {
					t.Errorf("%s/%s %s: command=%q want %q", h.ID, sc, st.Path, st.Command, wantCmd)
				}
			}
			changes, err := Install(h.ID, sc, opts)
			if err != nil {
				t.Fatalf("%s/%s second install: %v", h.ID, sc, err)
			}
			for _, c := range changes {
				if c.Changed {
					t.Errorf("%s/%s %s: changed on second install", h.ID, sc, c.Path)
				}
			}
			fresh, err := Inspect(h.ID, sc, opts)
			if err != nil {
				t.Fatalf("%s/%s inspect: %v", h.ID, sc, err)
			}
			for _, st := range fresh {
				if !st.Installed {
					continue
				}
				data, err := os.ReadFile(st.Path)
				if err != nil {
					t.Fatal(err)
				}
				before := outdatedContent(t, h, h.Scopes[sc], project, data, st.Command)
				if err := os.WriteFile(st.Path, before, 0o644); err != nil {
					t.Fatal(err)
				}
				changes, err := Install(h.ID, sc, opts)
				if err != nil {
					t.Fatalf("%s/%s outdated install: %v", h.ID, sc, err)
				}
				fixed := false
				for _, c := range changes {
					if c.Path != st.Path || !c.Changed {
						continue
					}
					fixed = true
					if c.Backup == "" {
						t.Errorf("%s/%s %s: no backup written", h.ID, sc, st.Path)
						continue
					}
					saved, err := os.ReadFile(c.Backup)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(saved, before) {
						t.Errorf("%s/%s %s: backup content mismatch", h.ID, sc, st.Path)
					}
				}
				if !fixed {
					t.Errorf("%s/%s %s: outdated entry not rewritten", h.ID, sc, st.Path)
				}
				after, err := Inspect(h.ID, sc, opts)
				if err != nil {
					t.Fatal(err)
				}
				for _, a := range after {
					if a.Path == st.Path && !a.UpToDate {
						t.Errorf("%s/%s %s: still not up to date", h.ID, sc, st.Path)
					}
				}
			}
			if _, err := Remove(h.ID, sc, opts); err != nil {
				t.Fatalf("%s/%s remove: %v", h.ID, sc, err)
			}
			entries, err = Inspect(h.ID, sc, opts)
			if err != nil {
				t.Fatalf("%s/%s inspect after remove: %v", h.ID, sc, err)
			}
			for _, st := range entries {
				if st.Installed {
					t.Errorf("%s/%s %s: still installed", h.ID, sc, st.Path)
				}
			}
		}
	}
}

func TestWriteConfigFollowsSymlink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real.json")
	writeFile(t, real, []byte("{}\n"))
	link := filepath.Join(base, "link.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if err := writeConfig(link, []byte("{\"a\":1}\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"a\":1}\n" {
		t.Fatalf("real file not updated: %s", data)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced")
	}
}

func TestWriteConfigReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	err := writeConfig(filepath.Join(ro, "f.json"), []byte("{}\n"))
	if err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("err = %v", err)
	}
}
