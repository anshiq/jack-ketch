# Integrations

How jack-ketch connects to MCP clients, one section per agent. Everything
here is driven by `jack-ketch integrate`:

```sh
jack-ketch integrate list                     # detection + connection status
jack-ketch integrate install <agent>          # diff first, then write
jack-ketch integrate install --detected       # every detected Tier-1 agent
jack-ketch integrate show <agent>             # the snippet, without writing
jack-ketch integrate remove <agent>
jack-ketch integrate verify <agent>           # spawn the entry, list tools
jack-ketch integrate skills install --agent <agent>
jack-ketch integrate hooks install claude-code
```

Installs are diffed before writing, the previous file is backed up to
`<file>.jack-ketch.bak`, and writes are atomic. `--scope` selects user,
project or local where the agent supports it; project entries are portable
(they use the bare `jack-ketch` command, so it must be on `PATH`), user
entries use the absolute path of the installed binary.

## The entry written

Every harness gets the same MCP server under the name `jack-ketch`, with
`--harness <id>` so the daemon knows which client is calling (this raises the
per-call wait cap for clients that support it):

```json
"jack-ketch": {
  "command": "/usr/local/bin/jack-ketch",
  "args": ["serve", "--harness", "cursor"]
}
```

Dialects differ — some clients want `type: "stdio"`, some a `timeout`
(in milliseconds or seconds), opencode and Kilo Code want a single `command`
array, Codex wants TOML, Goose and Continue want YAML. The exact shapes are
shown per agent below.

## Claude Code (`claude-code`)

- **Scopes** — user (`~/.claude.json`, `mcpServers`), project (`.mcp.json`,
  `mcpServers`), local (`~/.claude.json` → `projects.<project>.mcpServers`).
- **Entry** (Claude dialect, 660 s timeout):

  ```json
  "jack-ketch": {
    "type": "stdio",
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "claude-code"],
    "timeout": 660000
  }
  ```

- **Waits** — 600 s installed and detected; the longest of any client.
- **Skills** — installed to `~/.claude/skills` (project: `.claude/skills`).
- **Restart** — run `/mcp` in Claude Code (or restart it) to connect.
- **Caveats** — `.mcp.json` is also read by VS Code and GitHub Copilot CLI,
  so a project install covers all three; Claude Code asks once before trusting
  project servers. `jack-ketch integrate hooks install claude-code` adds a
  PreToolUse guardrail that denies `npm run dev`-style commands and points at
  `start_process` instead.

## Claude Desktop (`claude-desktop`)

- **Scope** — user only: `<appdata>/Claude/claude_desktop_config.json`
  (`mcpServers`).
- **Entry** (plain):

  ```json
  "jack-ketch": {
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "claude-desktop"]
  }
  ```

- **Waits** — 50 s.
- **Restart** — quit Claude Desktop completely and reopen it.
- **Caveats** — no project folder: ask Claude to call `set_workspace` with
  your project path once per conversation, or pass `workspace=` per call.

## Codex (`codex`)

- **Scopes** — user (`~/.codex/config.toml`, or `$CODEX_HOME`), project
  (`.codex/config.toml`).
- **Entry** (TOML block):

  ```toml
  [mcp_servers.jack-ketch]
  command = "/usr/local/bin/jack-ketch"
  args = ["serve", "--harness", "codex"]
  startup_timeout_sec = 30
  tool_timeout_sec = 660
  ```

- **Waits** — 600 s installed, 50 s detected.
- **Skills** — installed to `~/.agents/skills` (project: `.agents/skills`).
- **Restart** — start a new Codex session; `/mcp` lists jack-ketch.
- **Caveats** — Codex reads project config only in trusted projects.

## Gemini CLI (`gemini`)

- **Scopes** — user (`~/.gemini/settings.json`), project
  (`.gemini/settings.json`). JSONC.
- **Entry** (Gemini dialect, 660 s timeout):

  ```json
  "jack-ketch": {
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "gemini"],
    "timeout": 660000
  }
  ```

- **Waits** — 600 s installed, 540 s detected.
- **Skills** — installed to `~/.agents/skills` (reads `~/.gemini/skills` too).
- **Restart** — restart Gemini CLI or run `/mcp refresh`.

## Qwen Code (`qwen-code`)

- **Scopes** — user (`~/.qwen/settings.json`), project
  (`.qwen/settings.json`). JSONC.
- **Entry** — same shape as Gemini (`command`, `args`, `timeout: 660000`),
  `--harness qwen-code`.
- **Waits** — 600 s installed, 540 s detected.
- **Restart** — restart Qwen Code.

## Cursor (`cursor`)

- **Scopes** — user (`~/.cursor/mcp.json`), project (`.cursor/mcp.json`).
- **Entry** (typed stdio):

  ```json
  "jack-ketch": {
    "type": "stdio",
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "cursor"]
  }
  ```

- **Waits** — 50 s.
- **Skills** — installed to `~/.agents/skills` (project: `.agents/skills`).
- **Restart** — Cursor reloads `mcp.json` automatically; if the server shows
  as stopped, enable it under Settings → MCP.
- **Deeplink** — `jack-ketch integrate show cursor` prints one.

## VS Code, GitHub Copilot (`vscode-copilot`)

- **Scopes** — user (`<appdata>/Code/User/mcp.json`, plus Insiders and
  VSCodium variants, `servers`), project (`.vscode/mcp.json`, `servers`).
  JSONC.
- **Entry** (typed stdio):

  ```json
  "jack-ketch": {
    "type": "stdio",
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "vscode-copilot"]
  }
  ```

- **Waits** — 50 s.
- **Skills** — installed to `~/.agents/skills` (project: `.agents/skills`).
- **Restart** — run “MCP: List Servers”, pick jack-ketch and choose Start
  (or reload the window).

## GitHub Copilot CLI (`copilot-cli`)

- **Scopes** — user (`~/.copilot/mcp-config.json`, or `$COPILOT_HOME`),
  project (`.github/mcp.json`).
- **Entry** (Copilot dialect):

  ```json
  "jack-ketch": {
    "type": "local",
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "copilot-cli"],
    "env": {},
    "tools": ["*"]
  }
  ```

- **Waits** — 50 s.
- **Skills** — installed to `~/.copilot/skills` (project: `.github/skills`).
- **Restart** — restart Copilot CLI; `/mcp` shows jack-ketch.
- **Caveats** — project servers load only in trusted folders.

## Windsurf (`windsurf`)

- **Scope** — user only. Windsurf migrated its config to
  `~/.config/devin/mcp_config.json` (`%APPDATA%/devin/...` on Windows); older
  installs use `~/.codeium/windsurf/mcp_config.json`. Both are written when
  present (`mcpServers`).
- **Entry** (plain):

  ```json
  "jack-ketch": {
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "windsurf"]
  }
  ```

- **Waits** — 50 s.
- **Restart** — open Cascade → MCP servers and press Refresh.

## Zed (`zed`)

- **Scopes** — user (`~/.config/zed/settings.json`, or
  `%APPDATA%/Zed/settings.json`), project (`.zed/settings.json`). JSONC,
  under `context_servers`.
- **Entry** (Zed dialect):

  ```json
  "jack-ketch": {
    "source": "custom",
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "zed"],
    "env": {}
  }
  ```

- **Waits** — 50 s.
- **Restart** — Zed reloads settings automatically; check Agent Panel →
  Settings → MCP Servers.

## Cline (`cline`)

- **Scope** — user only, and the file must already exist: open Cline once
  (MCP Servers → Configure) so it creates `cline_mcp_settings.json`, then
  install again. Paths: each VS Code-family host's
  `User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json`
  plus `~/.cline/data/settings/cline_mcp_settings.json` (or
  `$CLINE_MCP_SETTINGS_PATH`).
- **Entry** (Cline dialect, `timeout` in seconds):

  ```json
  "jack-ketch": {
    "type": "stdio",
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "cline"],
    "env": {},
    "disabled": false,
    "autoApprove": [],
    "timeout": 660
  }
  ```

- **Waits** — 600 s installed, 50 s detected.
- **Restart** — Cline reloads the file automatically; use Restart Server in
  the MCP Servers panel if needed.

## Roo Code (`roo`)

- **Scopes** — user (each host's
  `User/globalStorage/rooveterinaryinc.roo-cline/settings/mcp_settings.json`,
  must already exist), project (`.roo/mcp.json`).
- **Entry** (Roo dialect, `timeout` in seconds):

  ```json
  "jack-ketch": {
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "roo"],
    "env": {},
    "disabled": false,
    "alwaysAllow": [],
    "timeout": 300
  }
  ```

- **Waits** — 290 s installed, 50 s detected (Roo clamps tool calls near
  5 minutes).
- **Restart** — Roo Code reloads MCP settings automatically.

## Kilo Code (`kilo`)

- **Scopes** — user (`~/.config/kilo/kilo.json` or `.jsonc`), project
  (`kilo.json`, `kilo.jsonc`, `.kilo/kilo.jsonc`). JSONC, under `mcp`.
- **Entry** (Kilo dialect, single `command` array):

  ```json
  "jack-ketch": {
    "type": "local",
    "command": ["/usr/local/bin/jack-ketch", "serve", "--harness", "kilo"],
    "environment": {},
    "enabled": true,
    "timeout": 30000
  }
  ```

- **Waits** — 50 s.
- **Restart** — restart Kilo Code.

## opencode (`opencode`)

- **Scopes** — user (`~/.config/opencode/opencode.json` or `.jsonc`),
  project (`opencode.json`, `opencode.jsonc`, `.opencode/opencode.json`).
  JSONC, under `mcp`. A missing file is created with the opencode `$schema`
  line.
- **Entry** (opencode dialect, single `command` array):

  ```json
  "jack-ketch": {
    "type": "local",
    "command": ["/usr/local/bin/jack-ketch", "serve", "--harness", "opencode"],
    "enabled": true,
    "timeout": 30000
  }
  ```

- **Waits** — 50 s.
- **Skills** — installed to `~/.config/opencode/skills` (project:
  `.opencode/skills`).
- **Restart** — restart opencode; `opencode mcp list` shows jack-ketch.

## Goose (`goose`)

- **Scope** — user only: `~/.config/goose/config.yaml` (or
  `<appdata>/Block/goose/config/config.yaml`), under `extensions`.
- **Entry** (Goose YAML):

  ```yaml
  extensions:
    jack-ketch:
      type: stdio
      name: jack-ketch
      enabled: true
      cmd: /usr/local/bin/jack-ketch
      args: [serve, --harness, goose]
      envs: {}
      env_keys: []
      timeout: 660
  ```

- **Waits** — 600 s installed, 270 s detected.
- **Restart** — start a new Goose session to load the extension.

## Kiro (`kiro`)

- **Scopes** — user (`~/.kiro/settings/mcp.json`), project
  (`.kiro/settings/mcp.json`).
- **Entry** (Kiro dialect):

  ```json
  "jack-ketch": {
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "kiro"],
    "env": {},
    "disabled": false,
    "autoApprove": []
  }
  ```

- **Waits** — 50 s.
- **Restart** — Kiro reloads MCP settings automatically.

## Amp (`amp`)

- **Scopes** — user (`~/.config/amp/settings.json`, or
  `<appdata>/amp/settings.json`), project (`.amp/settings.json`). JSONC,
  under `amp.mcpServers`.
- **Entry** (plain):

  ```json
  "jack-ketch": {
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "amp"]
  }
  ```

- **Waits** — 50 s.
- **Restart** — restart Amp.
- **Caveats** — Amp asks for approval before running workspace servers.

## Crush (`crush`)

- **Scopes** — user (`~/.config/crush/crush.json`, under `mcp`), project
  (`.crush.json` or `crush.json`).
- **Entry** (typed stdio):

  ```json
  "jack-ketch": {
    "type": "stdio",
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "crush"]
  }
  ```

- **Waits** — 50 s.
- **Restart** — restart Crush.

## JetBrains Junie (`junie`)

- **Scopes** — user (`~/.junie/mcp/mcp.json`), project
  (`.junie/mcp/mcp.json`).
- **Entry** (plain):

  ```json
  "jack-ketch": {
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "junie"]
  }
  ```

- **Waits** — 50 s.
- **Restart** — restart Junie or open `/mcp`.

## Continue (`continue`)

- **Scopes** — user (`~/.continue/config.yaml`, must already exist; older
  `config.json` setups are not edited), project
  (`.continue/mcpServers/jack-ketch.yaml`, a per-server block file).
- **Entry** (Continue YAML, user config):

  ```yaml
  mcpServers:
    - name: jack-ketch
      command: /usr/local/bin/jack-ketch
      args: [serve, --harness, continue]
  ```

  The project file is the same shape with the `name`/`version`/`schema`
  header.

- **Waits** — 50 s.
- **Restart** — Continue reloads its config automatically.

## Augment (Auggie CLI) (`augment`)

- **Scope** — user only: `~/.augment/settings.json` (`mcpServers`).
- **Entry** (plain):

  ```json
  "jack-ketch": {
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "augment"]
  }
  ```

- **Waits** — 50 s.
- **Restart** — restart auggie; `/mcp-status` shows jack-ketch.

## LM Studio (`lmstudio`)

- **Scope** — user only: `~/.lmstudio/mcp.json` or
  `~/.cache/lm-studio/mcp.json` (`mcpServers`); both are written when present.
- **Entry** (plain):

  ```json
  "jack-ketch": {
    "command": "/usr/local/bin/jack-ketch",
    "args": ["serve", "--harness", "lmstudio"]
  }
  ```

- **Waits** — 50 s.
- **Restart** — LM Studio reloads `mcp.json` automatically.

## Tier-2 clients

These are detected but configured manually — `jack-ketch integrate show
<agent>` prints a ready-to-paste snippet (plain `command`/`args` JSON unless
noted). `--all` on `integrate list` shows them even when not detected; waits
default to 50 s.

| Agent | How it is set up |
|---|---|
| Google Antigravity (`antigravity`) | Antigravity → Manage MCP Servers → View raw config; usually `~/.gemini/antigravity/mcp_config.json` |
| Factory Droid (`factory`) | Run `droid mcp add jack-ketch '<command> serve --harness factory'` (printed by `integrate show`) |
| Trae (`trae`) | Settings → MCP → Add → Configure manually, paste the snippet |
| Warp (`warp`) | Settings → AI → MCP Servers → Add; Warp wants the bare object (no `mcpServers` wrapper) |
| JetBrains AI Assistant (`jetbrains-ai`) | Settings → Tools → AI Assistant → MCP → Add → As JSON |
| Visual Studio (`visual-studio`) | `%USERPROFILE%\.mcp.json` (VS 2022 17.14+) |
| Any other MCP client (`generic`) | Configure a stdio server with the printed command and args |

## Read-only configs (Nix, home-manager, dotfile managers)

If a config file is managed declaratively (Nix, home-manager, chezmoi),
`integrate install` refuses to write it and tells you. Use `jack-ketch
integrate show <agent>` to print the entry and add it to your Nix
configuration. `integrate verify <agent>` still works; nothing is written.

## HTTP transport

Instead of stdio, clients that support streamable HTTP can talk to one
long-lived listener:

```sh
jack-ketch serve --http 127.0.0.1:7341
```

A bare `:7341` binds loopback; other interfaces require `--allow-remote`. The
bearer token comes from `--token`, `--token-file`,
`JACK_KETCH_HTTP_TOKEN`, or the web UI token file
(`$XDG_CONFIG_HOME/jack-ketch/token` on the default setup). Pick the
project per request with the `X-Jack-Ketch-Workspace` header or a
`?workspace=` query parameter; without either, the listener uses its start
directory. Tool calls are rate-limited. The web UI uses the same listener
(`jack-ketch web` starts it on port 7350 by default).

## Claude Code plugin

jack-ketch is also packaged as a Claude Code plugin (bundled MCP server
plus the three Agent Skills):

```text
/plugin marketplace add anshiq/jack-ketch
/plugin install jack-ketch@jack-ketch
```

This requires `jack-ketch` on `PATH`. The plugin starts `jack-ketch
serve --harness claude-code`, so waits are capped at the 600 s Claude Code
limit. The plugin bundles the `jack-ketch-ready`,
`jack-ketch-project-config` and `jack-ketch-logging` skills.
