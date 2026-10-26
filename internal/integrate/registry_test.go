package integrate

import (
	"os"
	"path/filepath"
	"testing"
)

func setTestEnv(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "xdg"))
	t.Setenv("APPDATA", filepath.Join(base, "appdata"))
	t.Setenv("CODEX_HOME", filepath.Join(base, "codex"))
	t.Setenv("COPILOT_HOME", filepath.Join(base, "copilot"))
	t.Setenv("CLINE_MCP_SETTINGS_PATH", filepath.Join(base, "cline", "cline_mcp_settings.json"))
	t.Setenv("JACK_KETCH_BIN", "/opt/jack-ketch/bin/jack-ketch")
	return base
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRegistry(t *testing.T) {
	setTestEnv(t)
	ids := map[string]bool{}
	for _, h := range builtinHarnesses {
		if ids[h.ID] {
			t.Errorf("duplicate harness id %s", h.ID)
		}
		ids[h.ID] = true
		if h.Tier == 1 {
			if len(h.ScopeList()) == 0 {
				t.Errorf("%s: no scopes", h.ID)
			}
			if h.Dialect == "" {
				t.Errorf("%s: empty dialect", h.ID)
			}
			if h.RestartHint == "" {
				t.Errorf("%s: empty restart hint", h.ID)
			}
		}
		if h.Tier == 2 && h.ManualSteps == "" {
			t.Errorf("%s: tier 2 without manual steps", h.ID)
		}
	}
	if h, ok := Lookup("claude"); !ok || h.ID != "claude-code" {
		t.Errorf("Lookup(claude) = %+v, %v", h.ID, ok)
	}
	if CanonicalID("vscode") != "vscode-copilot" {
		t.Errorf("CanonicalID(vscode) = %s", CanonicalID("vscode"))
	}
}
