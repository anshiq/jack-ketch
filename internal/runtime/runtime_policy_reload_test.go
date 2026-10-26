package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jack-ketch/internal/config"
	"jack-ketch/pkg/api"
)

func TestPolicyHotReload(t *testing.T) {
	rt := newRuntimeWithConfig(t, "")
	restricted := t.TempDir()
	path := filepath.Join(restricted, "jack-ketch.yaml")
	if err := os.WriteFile(path, []byte("runtime:\n  security:\n    mode: restricted\n    allowed_commands: [go]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rt.ReloadConfig(loaded)
	_, err = rt.Start(context.Background(), api.StartRequest{
		Command: "sh",
		Args:    []string{"-c", "true"},
		WorkDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "policy_denied") {
		t.Fatalf("start after restricted reload: err = %v, want policy_denied", err)
	}
}
