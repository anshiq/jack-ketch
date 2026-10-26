package integrate

import (
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const ServerName = "jack-ketch"

func p(all string) PathSpec { return PathSpec{All: all} }

func pw(all, windows string) PathSpec { return PathSpec{All: all, Windows: windows} }

func hostStorage(rel string) []PathSpec {
	hosts := []string{"Code", "Code - Insiders", "VSCodium", "Cursor", "Windsurf"}
	out := make([]PathSpec, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, p("{appdata}/"+h+"/User/globalStorage/"+rel))
	}
	return out
}

func waits(installed, detected int) Limits {
	return Limits{InstalledMaxWaitMS: installed, DetectedMaxWaitMS: detected}
}

func commandLine(s ServerSpec) []string { return append([]string{s.Command}, s.Args...) }

var builtinHarnesses = []Harness{
	{
		ID: "claude-code", DisplayName: "Claude Code", Tier: 1,
		DocsURL:     "https://code.claude.com/docs/en/mcp",
		Binaries:    []string{"claude"},
		DetectPaths: []PathSpec{p("{home}/.claude"), p("{home}/.claude.json")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{home}/.claude.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/.mcp.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}, Portable: true, Note: "Also read by VS Code and GitHub Copilot CLI. Claude Code asks once before trusting project servers."},
			ScopeLocal:   {Paths: []PathSpec{p("{home}/.claude.json")}, Format: FormatJSON, KeyPath: []string{"projects", "{project}", "mcpServers"}},
		},
		Dialect:     DialectClaude,
		Limits:      waits(600000, 600000),
		Skills:      &SkillSpec{Install: p("{home}/.claude/skills"), Reads: []PathSpec{p("{home}/.claude/skills")}, ProjectDir: ".claude/skills"},
		RestartHint: "Run /mcp in Claude Code (or restart it) to connect jack-ketch.",
		CLIAdd: func(s ServerSpec) string {
			return "claude mcp add-json --scope user jack-ketch " + shellQuote(jsonString(jsonEntry(DialectClaude, s)))
		},
	},
	{
		ID: "claude-desktop", DisplayName: "Claude Desktop", Tier: 1,
		DocsURL:     "https://modelcontextprotocol.io/docs/develop/connect-local-servers",
		DetectPaths: []PathSpec{p("{appdata}/Claude"), {Darwin: "/Applications/Claude.app"}},
		Scopes: map[Scope]Target{
			ScopeUser: {Paths: []PathSpec{p("{appdata}/Claude/claude_desktop_config.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}},
		},
		Dialect:     DialectPlain,
		Limits:      waits(50000, 50000),
		RestartHint: "Quit Claude Desktop completely and reopen it. Claude Desktop has no project folder: ask Claude to call set_workspace with your project path once per conversation.",
	},
	{
		ID: "codex", DisplayName: "Codex", Tier: 1,
		DocsURL:     "https://developers.openai.com/codex/mcp",
		Binaries:    []string{"codex"},
		DetectPaths: []PathSpec{p("{codex_home}")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{codex_home}/config.toml")}, Format: FormatTOML},
			ScopeProject: {Paths: []PathSpec{p("{project}/.codex/config.toml")}, Format: FormatTOML, Portable: true, Note: "Codex reads project config only in trusted projects."},
		},
		Dialect:     DialectCodex,
		Limits:      waits(600000, 50000),
		Skills:      &SkillSpec{Install: p("{home}/.agents/skills"), Reads: []PathSpec{p("{home}/.agents/skills")}, ProjectDir: ".agents/skills"},
		RestartHint: "Start a new Codex session; /mcp lists jack-ketch.",
		CLIAdd: func(s ServerSpec) string {
			return "codex mcp add jack-ketch -- " + shellJoin(commandLine(s))
		},
	},
	{
		ID: "gemini", DisplayName: "Gemini CLI", Tier: 1,
		DocsURL:     "https://geminicli.com/docs/tools/mcp-server",
		Binaries:    []string{"gemini"},
		DetectPaths: []PathSpec{p("{home}/.gemini")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{home}/.gemini/settings.json")}, Format: FormatJSONC, KeyPath: []string{"mcpServers"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/.gemini/settings.json")}, Format: FormatJSONC, KeyPath: []string{"mcpServers"}, Portable: true},
		},
		Dialect:     DialectGemini,
		Limits:      waits(600000, 540000),
		Skills:      &SkillSpec{Install: p("{home}/.agents/skills"), Reads: []PathSpec{p("{home}/.gemini/skills"), p("{home}/.agents/skills")}, ProjectDir: ".agents/skills"},
		RestartHint: "Restart Gemini CLI or run /mcp refresh.",
		CLIAdd: func(s ServerSpec) string {
			return "gemini mcp add -s user --timeout 660000 jack-ketch " + shellJoin(commandLine(s))
		},
	},
	{
		ID: "qwen-code", DisplayName: "Qwen Code", Tier: 1,
		DocsURL:     "https://github.com/QwenLM/qwen-code",
		Binaries:    []string{"qwen"},
		DetectPaths: []PathSpec{p("{home}/.qwen")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{home}/.qwen/settings.json")}, Format: FormatJSONC, KeyPath: []string{"mcpServers"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/.qwen/settings.json")}, Format: FormatJSONC, KeyPath: []string{"mcpServers"}, Portable: true},
		},
		Dialect:     DialectGemini,
		Limits:      waits(600000, 540000),
		RestartHint: "Restart Qwen Code.",
	},
	{
		ID: "cursor", DisplayName: "Cursor", Tier: 1,
		DocsURL:     "https://cursor.com/docs/context/mcp",
		Binaries:    []string{"cursor", "cursor-agent"},
		DetectPaths: []PathSpec{p("{home}/.cursor")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{home}/.cursor/mcp.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/.cursor/mcp.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}, Portable: true},
		},
		Dialect:     DialectTypedStdio,
		Limits:      waits(50000, 50000),
		Skills:      &SkillSpec{Install: p("{home}/.agents/skills"), Reads: []PathSpec{p("{home}/.agents/skills"), p("{home}/.cursor/skills"), p("{home}/.claude/skills"), p("{home}/.codex/skills")}, ProjectDir: ".agents/skills"},
		RestartHint: "Cursor reloads mcp.json automatically; enable jack-ketch under Settings → MCP if it is off.",
		Deeplink: func(s ServerSpec) string {
			cfg := jsonString(jsonEntry(DialectTypedStdio, s))
			return "cursor://anysphere.cursor-deeplink/mcp/install?name=jack-ketch&config=" + url.QueryEscape(base64.StdEncoding.EncodeToString([]byte(cfg)))
		},
	},
	{
		ID: "vscode-copilot", DisplayName: "VS Code (GitHub Copilot)", Tier: 1,
		DocsURL:     "https://code.visualstudio.com/docs/copilot/customization/mcp-servers",
		Binaries:    []string{"code", "code-insiders", "codium"},
		DetectPaths: []PathSpec{p("{appdata}/Code/User"), p("{appdata}/Code - Insiders/User"), p("{appdata}/VSCodium/User")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{appdata}/Code/User/mcp.json"), p("{appdata}/Code - Insiders/User/mcp.json"), p("{appdata}/VSCodium/User/mcp.json")}, Format: FormatJSONC, KeyPath: []string{"servers"}, Fanout: true},
			ScopeProject: {Paths: []PathSpec{p("{project}/.vscode/mcp.json")}, Format: FormatJSONC, KeyPath: []string{"servers"}, Portable: true},
		},
		Dialect:     DialectVSCode,
		Limits:      waits(50000, 50000),
		Skills:      &SkillSpec{Install: p("{home}/.agents/skills"), Reads: []PathSpec{p("{home}/.copilot/skills"), p("{home}/.claude/skills"), p("{home}/.agents/skills")}, ProjectDir: ".agents/skills"},
		RestartHint: "Run “MCP: List Servers”, pick jack-ketch and choose Start (or reload the window).",
		CLIAdd: func(s ServerSpec) string {
			return "code --add-mcp " + shellQuote(jsonString(orderedObject{{"name", ServerName}, {"type", "stdio"}, {"command", s.Command}, {"args", s.Args}}))
		},
	},
	{
		ID: "copilot-cli", DisplayName: "GitHub Copilot CLI", Tier: 1,
		DocsURL:     "https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers",
		Binaries:    []string{"copilot"},
		DetectPaths: []PathSpec{p("{copilot_home}")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{copilot_home}/mcp-config.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/.github/mcp.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}, Portable: true, Note: "Copilot CLI loads project servers only in trusted folders."},
		},
		Dialect:     DialectCopilot,
		Limits:      waits(50000, 50000),
		Skills:      &SkillSpec{Install: p("{copilot_home}/skills"), Reads: []PathSpec{p("{copilot_home}/skills")}, ProjectDir: ".github/skills"},
		RestartHint: "Restart Copilot CLI; /mcp shows jack-ketch.",
		CLIAdd: func(s ServerSpec) string {
			return "copilot mcp add jack-ketch -- " + shellJoin(commandLine(s))
		},
	},
	{
		ID: "windsurf", DisplayName: "Windsurf", Tier: 1,
		DocsURL:     "https://docs.windsurf.com/plugins/cascade/mcp",
		Binaries:    []string{"windsurf"},
		DetectPaths: []PathSpec{pw("{xdg_config}/devin", "{appdata}/devin"), p("{home}/.codeium/windsurf")},
		Scopes: map[Scope]Target{
			ScopeUser: {Paths: []PathSpec{pw("{xdg_config}/devin/mcp_config.json", "{appdata}/devin/mcp_config.json"), p("{home}/.codeium/windsurf/mcp_config.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}},
		},
		Dialect:     DialectPlain,
		Limits:      waits(50000, 50000),
		RestartHint: "Open Cascade → MCP servers and press Refresh.",
	},
	{
		ID: "zed", DisplayName: "Zed", Tier: 1,
		DocsURL:     "https://zed.dev/docs/ai/mcp",
		Binaries:    []string{"zed", "zeditor"},
		DetectPaths: []PathSpec{pw("{xdg_config}/zed", "{appdata}/Zed")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{pw("{xdg_config}/zed/settings.json", "{appdata}/Zed/settings.json")}, Format: FormatJSONC, KeyPath: []string{"context_servers"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/.zed/settings.json")}, Format: FormatJSONC, KeyPath: []string{"context_servers"}, Portable: true},
		},
		Dialect:     DialectZed,
		Limits:      waits(50000, 50000),
		RestartHint: "Zed reloads settings automatically; check Agent Panel → Settings → MCP Servers.",
	},
	{
		ID: "cline", DisplayName: "Cline", Tier: 1,
		DocsURL:     "https://docs.cline.bot/mcp/configuring-mcp-servers",
		Binaries:    []string{"cline"},
		Extensions:  []string{"saoudrizwan.claude-dev"},
		DetectPaths: []PathSpec{p("{home}/.cline")},
		Scopes: map[Scope]Target{
			ScopeUser: {Paths: append(hostStorage("saoudrizwan.claude-dev/settings/cline_mcp_settings.json"), p("{cline_settings}")), Format: FormatJSON, KeyPath: []string{"mcpServers"}, Fanout: true, RequireExist: true},
		},
		Dialect:     DialectCline,
		Limits:      waits(600000, 50000),
		ManualSteps: "Open Cline once (MCP Servers → Configure → Configure MCP Servers) so it creates cline_mcp_settings.json, then install again.",
		RestartHint: "Cline reloads the file automatically; use Restart Server in the MCP Servers panel if needed.",
	},
	{
		ID: "roo", DisplayName: "Roo Code", Tier: 1,
		DocsURL:    "https://docs.roocode.com/features/mcp/using-mcp-in-roo",
		Extensions: []string{"rooveterinaryinc.roo-cline"},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: hostStorage("rooveterinaryinc.roo-cline/settings/mcp_settings.json"), Format: FormatJSON, KeyPath: []string{"mcpServers"}, Fanout: true, RequireExist: true},
			ScopeProject: {Paths: []PathSpec{p("{project}/.roo/mcp.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}, Portable: true},
		},
		Dialect:     DialectRoo,
		Limits:      waits(290000, 50000),
		ManualSteps: "Open Roo Code's MCP settings once (Edit Global MCP) so it creates mcp_settings.json, or use project scope.",
		RestartHint: "Roo Code reloads MCP settings automatically.",
	},
	{
		ID: "kilo", DisplayName: "Kilo Code", Tier: 1,
		DocsURL:     "https://kilo.ai/docs/features/mcp/using-mcp-in-kilo-code",
		Binaries:    []string{"kilo"},
		Extensions:  []string{"kilocode.kilo-code"},
		DetectPaths: []PathSpec{p("{xdg_config}/kilo")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{xdg_config}/kilo/kilo.json"), p("{xdg_config}/kilo/kilo.jsonc")}, Format: FormatJSONC, KeyPath: []string{"mcp"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/kilo.json"), p("{project}/kilo.jsonc"), p("{project}/.kilo/kilo.jsonc")}, Format: FormatJSONC, KeyPath: []string{"mcp"}, Portable: true},
		},
		Dialect:     DialectKilo,
		Limits:      waits(50000, 50000),
		RestartHint: "Restart Kilo Code.",
	},
	{
		ID: "opencode", DisplayName: "opencode", Tier: 1,
		DocsURL:     "https://opencode.ai/docs/mcp-servers/",
		Binaries:    []string{"opencode"},
		DetectPaths: []PathSpec{p("{xdg_config}/opencode")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{xdg_config}/opencode/opencode.json"), p("{xdg_config}/opencode/opencode.jsonc")}, Format: FormatJSONC, KeyPath: []string{"mcp"}, NewFile: "{\n  \"$schema\": \"https://opencode.ai/config.json\"\n}\n"},
			ScopeProject: {Paths: []PathSpec{p("{project}/opencode.json"), p("{project}/opencode.jsonc"), p("{project}/.opencode/opencode.json")}, Format: FormatJSONC, KeyPath: []string{"mcp"}, Portable: true, NewFile: "{\n  \"$schema\": \"https://opencode.ai/config.json\"\n}\n"},
		},
		Dialect:     DialectOpenCode,
		Limits:      waits(50000, 50000),
		Skills:      &SkillSpec{Install: p("{xdg_config}/opencode/skills"), Reads: []PathSpec{p("{xdg_config}/opencode/skills"), p("{home}/.claude/skills"), p("{home}/.agents/skills")}, ProjectDir: ".opencode/skills"},
		RestartHint: "Restart opencode; `opencode mcp list` shows jack-ketch.",
	},
	{
		ID: "goose", DisplayName: "Goose", Tier: 1,
		DocsURL:     "https://block.github.io/goose/docs/getting-started/using-extensions",
		Binaries:    []string{"goose"},
		DetectPaths: []PathSpec{pw("{xdg_config}/goose", "{appdata}/Block/goose/config")},
		Scopes: map[Scope]Target{
			ScopeUser: {Paths: []PathSpec{pw("{xdg_config}/goose/config.yaml", "{appdata}/Block/goose/config/config.yaml")}, Format: FormatGooseYAML, KeyPath: []string{"extensions"}},
		},
		Dialect:     DialectGoose,
		Limits:      waits(600000, 270000),
		RestartHint: "Start a new Goose session to load the extension.",
	},
	{
		ID: "kiro", DisplayName: "Kiro", Tier: 1,
		DocsURL:     "https://kiro.dev/docs/mcp/configuration/",
		Binaries:    []string{"kiro", "kiro-cli"},
		DetectPaths: []PathSpec{p("{home}/.kiro")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{home}/.kiro/settings/mcp.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/.kiro/settings/mcp.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}, Portable: true},
		},
		Dialect:     DialectKiro,
		Limits:      waits(50000, 50000),
		RestartHint: "Kiro reloads MCP settings automatically.",
	},
	{
		ID: "amp", DisplayName: "Amp", Tier: 1,
		DocsURL:     "https://ampcode.com/manual#mcp",
		Binaries:    []string{"amp"},
		DetectPaths: []PathSpec{pw("{xdg_config}/amp", "{appdata}/amp")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{pw("{xdg_config}/amp/settings.json", "{appdata}/amp/settings.json")}, Format: FormatJSONC, KeyPath: []string{"amp.mcpServers"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/.amp/settings.json")}, Format: FormatJSONC, KeyPath: []string{"amp.mcpServers"}, Portable: true, Note: "Amp asks for approval before running workspace servers."},
		},
		Dialect:     DialectPlain,
		Limits:      waits(50000, 50000),
		RestartHint: "Restart Amp.",
	},
	{
		ID: "crush", DisplayName: "Crush", Tier: 1,
		DocsURL:     "https://github.com/charmbracelet/crush",
		Binaries:    []string{"crush"},
		DetectPaths: []PathSpec{p("{xdg_config}/crush")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{xdg_config}/crush/crush.json")}, Format: FormatJSON, KeyPath: []string{"mcp"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/.crush.json"), p("{project}/crush.json")}, Format: FormatJSON, KeyPath: []string{"mcp"}, Portable: true},
		},
		Dialect:     DialectTypedStdio,
		Limits:      waits(50000, 50000),
		RestartHint: "Restart Crush.",
	},
	{
		ID: "junie", DisplayName: "JetBrains Junie", Tier: 1,
		DocsURL:     "https://junie.jetbrains.com/docs/junie-cli-mcp-configuration.html",
		Binaries:    []string{"junie"},
		DetectPaths: []PathSpec{p("{home}/.junie")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{home}/.junie/mcp/mcp.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}},
			ScopeProject: {Paths: []PathSpec{p("{project}/.junie/mcp/mcp.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}, Portable: true},
		},
		Dialect:     DialectPlain,
		Limits:      waits(50000, 50000),
		RestartHint: "Restart Junie or open /mcp.",
	},
	{
		ID: "continue", DisplayName: "Continue", Tier: 1,
		DocsURL:     "https://docs.continue.dev/customize/deep-dives/mcp",
		Binaries:    []string{"cn"},
		Extensions:  []string{"continue.continue"},
		DetectPaths: []PathSpec{p("{home}/.continue")},
		Scopes: map[Scope]Target{
			ScopeUser:    {Paths: []PathSpec{p("{home}/.continue/config.yaml")}, Format: FormatContinueYAML, KeyPath: []string{"mcpServers"}, RequireExist: true},
			ScopeProject: {Paths: []PathSpec{p("{project}/.continue/mcpServers/jack-ketch.yaml")}, Format: FormatContinueBlock, Portable: true},
		},
		Dialect:     DialectContinue,
		Limits:      waits(50000, 50000),
		ManualSteps: "Continue's user config must be ~/.continue/config.yaml (older config.json setups are not edited); use project scope otherwise.",
		RestartHint: "Continue reloads its config automatically.",
	},
	{
		ID: "augment", DisplayName: "Augment (Auggie CLI)", Tier: 1,
		DocsURL:     "https://docs.augmentcode.com/cli/integrations",
		Binaries:    []string{"auggie"},
		DetectPaths: []PathSpec{p("{home}/.augment")},
		Scopes: map[Scope]Target{
			ScopeUser: {Paths: []PathSpec{p("{home}/.augment/settings.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}},
		},
		Dialect:     DialectPlain,
		Limits:      waits(50000, 50000),
		RestartHint: "Restart auggie; /mcp-status shows jack-ketch.",
	},
	{
		ID: "lmstudio", DisplayName: "LM Studio", Tier: 1,
		DocsURL:     "https://lmstudio.ai/docs/app/mcp",
		Binaries:    []string{"lms"},
		DetectPaths: []PathSpec{p("{home}/.lmstudio"), p("{home}/.cache/lm-studio")},
		Scopes: map[Scope]Target{
			ScopeUser: {Paths: []PathSpec{p("{home}/.lmstudio/mcp.json"), p("{home}/.cache/lm-studio/mcp.json")}, Format: FormatJSON, KeyPath: []string{"mcpServers"}},
		},
		Dialect:     DialectPlain,
		Limits:      waits(50000, 50000),
		RestartHint: "LM Studio reloads mcp.json automatically.",
	},
	{
		ID: "antigravity", DisplayName: "Google Antigravity", Tier: 2,
		DocsURL:     "https://developers.google.com/workspace/guides/configure-mcp-servers",
		Binaries:    []string{"antigravity"},
		DetectPaths: []PathSpec{p("{home}/.gemini/antigravity")},
		ManualSteps: "Open Antigravity → Manage MCP Servers → View raw config and add the snippet (the file is ~/.gemini/antigravity/mcp_config.json on most installs).",
	},
	{
		ID: "factory", DisplayName: "Factory Droid", Tier: 2,
		DocsURL:     "https://docs.factory.ai/cli/configuration/mcp",
		Binaries:    []string{"droid"},
		DetectPaths: []PathSpec{p("{home}/.factory")},
		ManualSteps: "Run the command below in a terminal.",
		CLIAdd: func(s ServerSpec) string {
			return "droid mcp add jack-ketch " + shellQuote(shellJoin(commandLine(s)))
		},
	},
	{
		ID: "trae", DisplayName: "Trae", Tier: 2,
		DocsURL:     "https://docs.trae.ai/ide/model-context-protocol",
		DetectPaths: []PathSpec{p("{appdata}/Trae"), p("{home}/.trae")},
		ManualSteps: "Trae → Settings → MCP → Add → Configure manually, then paste the snippet.",
	},
	{
		ID: "warp", DisplayName: "Warp", Tier: 2,
		DocsURL:     "https://docs.warp.dev/knowledge-and-collaboration/mcp",
		Binaries:    []string{"warp", "warp-terminal"},
		ManualSteps: "Warp → Settings → AI → MCP Servers → Add, then paste the snippet.",
		SnippetKey:  []string{},
	},
	{
		ID: "jetbrains-ai", DisplayName: "JetBrains AI Assistant", Tier: 2,
		DocsURL:     "https://www.jetbrains.com/help/ai-assistant/mcp.html",
		ManualSteps: "Settings → Tools → AI Assistant → Model Context Protocol (MCP) → Add → As JSON, then paste the snippet.",
	},
	{
		ID: "visual-studio", DisplayName: "Visual Studio", Tier: 2,
		DocsURL:     "https://learn.microsoft.com/visualstudio/ide/mcp-servers",
		ManualSteps: "Add the snippet to %USERPROFILE%\\.mcp.json (Visual Studio 2022 17.14 or newer).",
	},
	{
		ID: "generic", DisplayName: "Other MCP client", Tier: 2,
		DocsURL:     "https://modelcontextprotocol.io/docs/learn/client-concepts",
		ManualSteps: "Configure a stdio MCP server with the command and arguments below.",
	},
}

type harnessOverride struct {
	ID       string              `yaml:"id"`
	Disabled bool                `yaml:"disabled"`
	Scopes   map[string][]string `yaml:"scopes"`
}

func loadOverrides() map[string]harnessOverride {
	out := map[string]harnessOverride{}
	dir := filepath.Join(currentPathEnv("").xdgConfig, "jack-ketch", "harnesses.d")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var o harnessOverride
		if yaml.Unmarshal(data, &o) != nil || o.ID == "" {
			continue
		}
		out[o.ID] = o
	}
	return out
}

func applyOverride(h Harness, o harnessOverride) Harness {
	if len(o.Scopes) == 0 {
		return h
	}
	scopes := make(map[Scope]Target, len(h.Scopes))
	for k, v := range h.Scopes {
		scopes[k] = v
	}
	for name, paths := range o.Scopes {
		sc, err := ParseScope(name)
		if err != nil {
			continue
		}
		t, ok := scopes[sc]
		if !ok {
			continue
		}
		t.Paths = nil
		for _, path := range paths {
			t.Paths = append(t.Paths, p(path))
		}
		scopes[sc] = t
	}
	h.Scopes = scopes
	return h
}

func Harnesses() []Harness {
	ov := loadOverrides()
	out := make([]Harness, 0, len(builtinHarnesses))
	for _, h := range builtinHarnesses {
		if o, ok := ov[h.ID]; ok {
			if o.Disabled {
				continue
			}
			h = applyOverride(h, o)
		}
		out = append(out, h)
	}
	return out
}

func CanonicalID(id string) string {
	v := strings.ToLower(strings.TrimSpace(id))
	switch v {
	case "claude":
		return "claude-code"
	case "desktop", "claude-app":
		return "claude-desktop"
	case "vscode", "code":
		return "vscode-copilot"
	case "copilot", "gh-copilot":
		return "copilot-cli"
	case "qwen":
		return "qwen-code"
	case "roo-code", "roocode":
		return "roo"
	case "kilocode", "kilo-code":
		return "kilo"
	case "jetbrains":
		return "jetbrains-ai"
	}
	return v
}

func Lookup(id string) (Harness, bool) {
	id = CanonicalID(id)
	for _, h := range Harnesses() {
		if h.ID == id {
			return h, true
		}
	}
	return Harness{}, false
}
