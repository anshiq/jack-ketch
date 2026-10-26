# jack-ketch

Local async process supervisor for AI coding agents — a per-user background
daemon that starts, watches and signals long-running dev processes (servers,
builds, log tails) on your behalf, so an agent can kick off work, walk away,
and come back to check on it instead of blocking a session on a foreground
command.

## Components

- **jackketchd** — the always-on daemon (one per OS user). Owns the project
  registry, process supervision, the log pipeline (with FTS5 search) and a
  global event bus, all backed by a single-writer SQLite `state.db`.
- **jack-ketch** — the CLI / MCP bridge agents talk to. Bare `jack-ketch`
  prints the command help, `jack-ketch web` opens the web UI.
- **jack-ketch-shim** — a tiny (<= 4 MB RSS), dependency-free process shim
  that stays put under a running process across a jackketchd upgrade.

See [docs/architecture.md](docs/architecture.md) for the full component
diagram, [docs/api.md](docs/api.md) for the daemon API, and
[docs/security.md](docs/security.md) for the security model.

## Quick start

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/anshiq/jack-ketch/master/scripts/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/anshiq/jack-ketch/master/scripts/install.ps1 | iex
```

The installers resolve the latest release, verify its SHA-256 checksum and
place `jack-ketch`, `jackketchd` and `jack-ketch-shim` in `~/.local/bin`
(`%LOCALAPPDATA%\jack-ketch\bin` on Windows). Pin a release or choose another
directory with `-v/--version` and `-d/--dir` (or `VERSION` and `INSTALL_DIR`).
You can also install the Nix flake under `packaging/nix/`.

Then connect your agents and start the web UI:

```sh
jack-ketch integrate install --detected
jack-ketch web
```

`integrate install --detected` connects every detected MCP client: it prints
the diff and asks before writing. `jack-ketch web` starts the daemon on
demand and opens the web UI at a token URL. From the web UI you can watch
processes, read and search logs, edit the project config and manage
integrations.

Useful follow-ups:

```sh
jack-ketch integrate list              # detection + connection status
jack-ketch integrate verify claude-code
jack-ketch integrate skills install --detected
jack-ketch integrate hooks install claude-code   # guardrail hook
```

## Supported agents

| Agent | Connect | Scopes |
|---|---|---|
| Claude Code | `jack-ketch integrate install claude-code` | user, project, local |
| Claude Desktop | `jack-ketch integrate install claude-desktop` | user |
| Codex | `jack-ketch integrate install codex` | user, project |
| Gemini CLI | `jack-ketch integrate install gemini` | user, project |
| Qwen Code | `jack-ketch integrate install qwen-code` | user, project |
| Cursor | `jack-ketch integrate install cursor` | user, project |
| VS Code (GitHub Copilot) | `jack-ketch integrate install vscode-copilot` | user, project |
| GitHub Copilot CLI | `jack-ketch integrate install copilot-cli` | user, project |
| Windsurf | `jack-ketch integrate install windsurf` | user |
| Zed | `jack-ketch integrate install zed` | user, project |
| Cline | `jack-ketch integrate install cline` | user |
| Roo Code | `jack-ketch integrate install roo` | user, project |
| Kilo Code | `jack-ketch integrate install kilo` | user, project |
| opencode | `jack-ketch integrate install opencode` | user, project |
| Goose | `jack-ketch integrate install goose` | user |
| Kiro | `jack-ketch integrate install kiro` | user, project |
| Amp | `jack-ketch integrate install amp` | user, project |
| Crush | `jack-ketch integrate install crush` | user, project |
| JetBrains Junie | `jack-ketch integrate install junie` | user, project |
| Continue | `jack-ketch integrate install continue` | user, project |
| Augment (Auggie CLI) | `jack-ketch integrate install augment` | user |
| LM Studio | `jack-ketch integrate install lmstudio` | user |

Tier-2 clients (Google Antigravity, Factory Droid, Trae, Warp, JetBrains AI
Assistant, Visual Studio, and any other MCP client) get a manual snippet from
`jack-ketch integrate show <agent>`. Per-agent config paths, the exact entry
written, restart hints, wait caps and caveats live in
[docs/integrations.md](docs/integrations.md).

## How waits work across agents

Clients differ in how long one MCP tool call may block. jack-ketch caps
every wait per call for the calling client: configs written by `integrate
install` pass `--harness <id>` (Claude Code, Codex, Gemini CLI, Goose and
Cline get 600 s; Roo Code 290 s; everyone else 50 s), while clients the daemon
only recognises by name default to 50 s. When a wait returns
`clamped=true` and `still_running=true`, call the wait tool again to keep
waiting. Clients that support MCP progress notifications get periodic
progress while a wait is pending. `get_project_info` shows the cap for the
current client.

## Projects and workspaces

Tools act on the session's project. The daemon binds it automatically from
the client's working folder (resolved through the locator chain: path,
device/inode, git remote), so a new session in a known folder just works.
Clients without a project folder — Claude Desktop is the common one — pass
`workspace="/abs/path"` on a call, or call `set_workspace` once per
conversation to bind the session. `list_projects` shows the projects and
workspace paths jack-ketch knows about.

## Learn more

- [docs/integrations.md](docs/integrations.md) — per-agent setup, entries, caveats
- [docs/troubleshooting.md](docs/troubleshooting.md) — `doctor` output mapped to actions
- [docs/api.md](docs/api.md) — the daemon API in detail

## Building from source

```sh
go build ./cmd/jack-ketch
go build ./cmd/jackketchd
go build ./cmd/jack-ketch-shim
```

Requires Go (see `go.mod` for the minimum version).

## License

MIT — see [LICENSE](LICENSE).
