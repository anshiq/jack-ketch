package integrate

import "testing"

func TestHarnessFromClient(t *testing.T) {
	cases := map[string]string{
		"claude-code":            "claude-code",
		"claude-ai":              "claude-desktop",
		"codex-mcp-client":       "codex",
		"gemini-cli-mcp-client":  "gemini",
		"cursor-vscode":          "cursor",
		"Visual Studio Code":     "vscode-copilot",
		"Cline":                  "cline",
		"Roo Code":               "roo",
		"Kilo Code":              "kilo",
		"opencode":               "opencode",
		"goose":                  "goose",
		"Zed":                    "zed",
		"windsurf-client":        "windsurf",
		"github-copilot-cli":     "copilot-cli",
		"JetBrains AI Assistant": "jetbrains-ai",
		"Junie":                  "junie",
		"amp":                    "amp",
		"example-client":         "",
		"continue-client":        "continue",
		"Antigravity":            "antigravity",
		"qwen-code":              "qwen-code",
	}
	for name, want := range cases {
		if got := HarnessFromClient(name, ""); got != want {
			t.Errorf("HarnessFromClient(%q) = %q, want %q", name, got, want)
		}
	}
}
