//go:build !windows

package integrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readClaudeSettings(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	return doc
}

func pretoolUseGroups(t *testing.T, doc map[string]any) []any {
	t.Helper()
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	groups, _ := hooks["PreToolUse"].([]any)
	return groups
}

func writeClaudeSettings(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeHookInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JACK_KETCH_BIN", "/usr/local/bin/jack-ketch")

	c, err := ApplyClaudeHook(ScopeUser, "", false)
	if err != nil {
		t.Fatalf("ApplyClaudeHook: %v", err)
	}
	if !c.Changed {
		t.Fatal("first install should report Changed")
	}
	groups := pretoolUseGroups(t, readClaudeSettings(t, home))
	if len(groups) != 1 {
		t.Fatalf("want 1 PreToolUse group, got %d", len(groups))
	}
	g, ok := groups[0].(map[string]any)
	if !ok || g["matcher"] != "Bash" {
		t.Fatalf("group = %#v", groups[0])
	}
	inner, _ := g["hooks"].([]any)
	if len(inner) != 1 {
		t.Fatalf("want 1 hook command, got %d", len(inner))
	}
	command, _ := inner[0].(map[string]any)["command"].(string)
	if !strings.HasSuffix(command, claudeHookMarker) {
		t.Fatalf("command %q does not end with %q", command, claudeHookMarker)
	}
	if !strings.Contains(command, "/usr/local/bin/jack-ketch") {
		t.Fatalf("command %q does not reference the resolved binary", command)
	}
}

func TestClaudeHookSecondInstallIsNoop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JACK_KETCH_BIN", "/usr/local/bin/jack-ketch")

	if _, err := ApplyClaudeHook(ScopeUser, "", false); err != nil {
		t.Fatal(err)
	}
	c, err := ApplyClaudeHook(ScopeUser, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Changed {
		t.Fatal("second install should report Changed == false")
	}
	if groups := pretoolUseGroups(t, readClaudeSettings(t, home)); len(groups) != 1 {
		t.Fatalf("want 1 group after reinstall, got %d", len(groups))
	}
}

func TestClaudeHookKeepsUnrelatedGroups(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JACK_KETCH_BIN", "/usr/local/bin/jack-ketch")
	writeClaudeSettings(t, home, `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Write", "hooks": [{"type": "command", "command": "other-tool check"}]}
    ]
  }
}`)

	if _, err := ApplyClaudeHook(ScopeUser, "", false); err != nil {
		t.Fatal(err)
	}
	groups := pretoolUseGroups(t, readClaudeSettings(t, home))
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(groups))
	}
	first, _ := groups[0].(map[string]any)
	if first["matcher"] != "Write" {
		t.Fatalf("unrelated group not preserved: %#v", first)
	}
}

func TestClaudeHookRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JACK_KETCH_BIN", "/usr/local/bin/jack-ketch")
	writeClaudeSettings(t, home, `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Write", "hooks": [{"type": "command", "command": "other-tool check"}]}
    ]
  }
}`)

	if _, err := ApplyClaudeHook(ScopeUser, "", false); err != nil {
		t.Fatal(err)
	}
	c, err := ApplyClaudeHook(ScopeUser, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Changed {
		t.Fatal("remove should report Changed")
	}
	groups := pretoolUseGroups(t, readClaudeSettings(t, home))
	if len(groups) != 1 {
		t.Fatalf("want the unrelated group to remain, got %d groups", len(groups))
	}
	if g, _ := groups[0].(map[string]any); g["matcher"] != "Write" {
		t.Fatalf("wrong group left behind: %#v", groups[0])
	}

	if _, err := ApplyClaudeHook(ScopeUser, "", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), claudeHookMarker) {
		t.Fatalf("marker still present after remove: %s", data)
	}
}

func TestClaudeHookRemoveDropsEmptyPreToolUse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JACK_KETCH_BIN", "/usr/local/bin/jack-ketch")

	if _, err := ApplyClaudeHook(ScopeUser, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyClaudeHook(ScopeUser, "", true); err != nil {
		t.Fatal(err)
	}
	doc := readClaudeSettings(t, home)
	if groups := pretoolUseGroups(t, doc); len(groups) != 0 {
		t.Fatalf("PreToolUse should be gone, got %#v", groups)
	}
	if hooks, _ := doc["hooks"].(map[string]any); len(hooks) != 0 {
		t.Fatalf("empty hooks object should be removed, got %#v", hooks)
	}
}
