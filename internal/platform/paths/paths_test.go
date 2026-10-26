package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUser(t *testing.T) {
	p := User()
	if p.Runtime == "" {
		t.Error("Runtime should not be empty")
	}
	if p.Data == "" {
		t.Error("Data should not be empty")
	}
	if p.Config == "" {
		t.Error("Config should not be empty")
	}
	if p.State == "" {
		t.Error("State should not be empty")
	}
	if p.Cache == "" {
		t.Error("Cache should not be empty")
	}
}

func TestUser_DirsExist(t *testing.T) {
	p := User()
	// Just verify the paths look reasonable, not that they exist.
	if !filepath.IsAbs(p.Runtime) {
		t.Errorf("Runtime should be absolute, got %q", p.Runtime)
	}
	if !filepath.IsAbs(p.Data) {
		t.Errorf("Data should be absolute, got %q", p.Data)
	}
}

func TestPaths_Methods(t *testing.T) {
	p := &Paths{
		Runtime: "/run/jack-ketch",
		Data:    "/var/lib/jack-ketch",
		Config:  "/etc/jack-ketch",
		State:   "/var/lib/jack-ketch-state",
		Cache:   "/var/cache/jack-ketch",
	}

	tests := []struct {
		name string
		want string
		got  string
	}{
		{"SocketPath", "/run/jack-ketch/jackketchd.sock", p.SocketPath()},
		{"PIDPath", "/run/jack-ketch/jackketchd.pid", p.PIDPath()},
		{"LockPath", "/run/jack-ketch/jackketchd.lock", p.LockPath()},
		{"LogPath", "/var/lib/jack-ketch-state/jackketchd.log", p.LogPath()},
		{"WorkspaceDir", "/var/lib/jack-ketch/logs/ws_456", p.WorkspaceDir("ws_456")},
		{"ShimDir", "/run/jack-ketch/shims/inst_789", p.ShimDir("inst_789")},
		{"TrashDir", "/var/lib/jack-ketch/trash", p.TrashDir()},
		{"BackupsDir", "/var/lib/jack-ketch/backups", p.BackupsDir()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestPaths_EnsureDirs(t *testing.T) {
	p := &Paths{
		Runtime: t.TempDir() + "/runtime",
		Data:    t.TempDir() + "/data",
	}
	if err := p.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	for _, d := range []string{p.Runtime, p.Data} {
		if _, err := os.Stat(d); os.IsNotExist(err) {
			t.Errorf("expected dir %q to exist", d)
		}
	}
}
