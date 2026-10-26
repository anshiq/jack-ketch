package daemon

import (
	"path/filepath"
	"strings"
)

var agentEnvPrefixes = []string{
	"CLAUDECODE", "CLAUDE_CODE_", "CLAUDE_PROJECT_DIR", "CLAUDE_PLUGIN_", "CODEX_", "GEMINI_CLI", "QWEN_",
	"CURSOR_", "VSCODE_", "OPENCODE", "WINDSURF_", "CODEIUM_", "ZED_", "CLINE_", "ROO_", "KILO_", "GOOSE_",
	"COPILOT_", "MCP_", "ELECTRON_RUN_AS_NODE", "TERM_PROGRAM", "LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES",
	"NOTIFY_SOCKET", "JACK_KETCH_PROJECT", "JACK_KETCH_HARNESS", "JACK_KETCH_MAX_WAIT_MS",
	"JACK_KETCH_TOOLSET", "JACK_KETCH_BIN",
}

func SpawnEnv(env []string, cliExe string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		drop := false
		for _, p := range agentEnvPrefixes {
			if strings.HasPrefix(key, p) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	if strings.TrimSuffix(filepath.Base(cliExe), ".exe") == "jack-ketch" {
		out = append(out, "JACK_KETCH_BIN="+cliExe)
	}
	return out
}
