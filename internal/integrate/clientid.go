package integrate

import (
	"os"
	"strings"
	"unicode"
)

type clientRule struct {
	tokens []string
	word   bool
	id     string
}

var clientRules = []clientRule{
	{[]string{"claude-code", "claude code", "claudecode"}, false, "claude-code"},
	{[]string{"claude-ai", "claude desktop", "claude-desktop"}, false, "claude-desktop"},
	{[]string{"antigravity"}, false, "antigravity"},
	{[]string{"codex"}, false, "codex"},
	{[]string{"qwen"}, false, "qwen-code"},
	{[]string{"gemini"}, false, "gemini"},
	{[]string{"cursor"}, false, "cursor"},
	{[]string{"windsurf", "cascade", "codeium", "devin"}, false, "windsurf"},
	{[]string{"roo"}, true, "roo"},
	{[]string{"cline"}, false, "cline"},
	{[]string{"kilo"}, false, "kilo"},
	{[]string{"continue"}, false, "continue"},
	{[]string{"visual studio code", "vscode", "vs code"}, false, "vscode-copilot"},
	{[]string{"copilot"}, false, "copilot-cli"},
	{[]string{"zed"}, true, "zed"},
	{[]string{"opencode"}, false, "opencode"},
	{[]string{"goose"}, false, "goose"},
	{[]string{"kiro"}, false, "kiro"},
	{[]string{"amp"}, true, "amp"},
	{[]string{"crush"}, false, "crush"},
	{[]string{"junie"}, false, "junie"},
	{[]string{"jetbrains", "intellij", "pycharm", "webstorm", "goland", "rider", "phpstorm", "rubymine", "clion"}, false, "jetbrains-ai"},
	{[]string{"augment", "auggie"}, false, "augment"},
	{[]string{"factory", "droid"}, false, "factory"},
	{[]string{"lm studio", "lmstudio", "lm-studio"}, false, "lmstudio"},
	{[]string{"trae"}, true, "trae"},
	{[]string{"warp"}, true, "warp"},
	{[]string{"visual studio"}, false, "visual-studio"},
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		out[w] = true
	}
	return out
}

func HarnessFromClient(name, title string) string {
	s := strings.ToLower(strings.TrimSpace(name + " " + title))
	if strings.TrimSpace(s) == "" {
		return ""
	}
	ws := words(s)
	for _, r := range clientRules {
		for _, t := range r.tokens {
			if (r.word && ws[t]) || (!r.word && strings.Contains(s, t)) {
				return r.id
			}
		}
	}
	return ""
}

var envRules = []struct {
	key, value, id string
}{
	{"CLAUDECODE", "", "claude-code"},
	{"CLAUDE_CODE_ENTRYPOINT", "", "claude-code"},
	{"CODEX_THREAD_ID", "", "codex"},
	{"GEMINI_CLI", "", "gemini"},
	{"OPENCODE_CLIENT", "", "opencode"},
	{"CURSOR_AGENT", "", "cursor"},
	{"CURSOR_TRACE_ID", "", "cursor"},
	{"TERM_PROGRAM", "cursor", "cursor"},
}

func HarnessFromEnv() string {
	for _, r := range envRules {
		v := os.Getenv(r.key)
		if v == "" {
			continue
		}
		if r.value == "" || strings.EqualFold(v, r.value) {
			return r.id
		}
	}
	return ""
}
