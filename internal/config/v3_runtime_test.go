package config

import (
	"testing"
	"time"
)

func TestResolveRuntimeSectionOverlay(t *testing.T) {
	ws := t.TempDir()
	src := &MemorySource{
		ProjectYAML: []byte("runtime:\n  stop_grace: 9s\n  security:\n    mode: restricted\n    allowed_commands: [go]\n"),
		Overlays: map[string][]byte{
			"ws_1": []byte("runtime:\n  stop_grace: 3s\n"),
		},
	}
	res, errs := Resolve(src, "proj_1", "ws_1", ws, nil)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	loaded := res.ToLoaded(ws)
	if got := loaded.Config.Runtime.StopGrace.Time(); got != 3*time.Second {
		t.Fatalf("stop_grace = %v, want 3s", got)
	}
	if got := loaded.Config.Runtime.Security.Mode(); got != "restricted" {
		t.Fatalf("security mode = %q, want restricted", got)
	}
	if got := loaded.Config.Runtime.Security.AllowedCommands; len(got) != 1 || got[0] != "go" {
		t.Fatalf("allowed_commands = %v, want [go]", got)
	}
}
