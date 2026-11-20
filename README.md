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

Managed processes are children of `jackketchd`: they outlive agent sessions,
but stopping, restarting or updating the daemon stops them all.

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
place `jack-ketch` and `jackketchd` in `~/.local/bin`
(`%LOCALAPPDATA%\jack-ketch\bin` on Windows). Pin a release or choose another
directory with `-v/--version` and `-d/--dir` (or `VERSION` and `INSTALL_DIR`).
You can also install the Nix flake under `packaging/nix/`.

Updating while the daemon runs is safe: the new version takes effect when the
daemon restarts, and restarting stops every managed process. The installer
warns when the daemon is running, and the daemon notices its binary was
replaced: the web UI shows a banner, `jack-ketch web` and
`jack-ketch daemon status` print a notice, and agents see `update_notice` in
`list_processes` and `get_project_info`, each saying how many processes a
restart will stop. `jack-ketch daemon restart` (and `daemon stop`) list them
and ask first; `--yes` skips the question and is required when not run from a
terminal.

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

## Modes, saved runs and runtime help

- **Modes.** An app can run several ways (`dev`, `prod`, `headless`, `test`,
  `staging`...). Each mode overrides only what differs; agents start one with
  `start_process(app="api", mode="prod")`.
- **Ordered env.** `env_sources` mixes dotenv files and inline vars in one
  ordered list. Later entries win, and `get_process_env` and the web Env tab
  show which source set each key.
- **Save what worked, after asking.** When a hand-built run succeeds, results
  carry `save_suggestion`. The agent asks you, then `save_run_config` stores
  the command, workdir and env as a mode. If you say no, it is not suggested
  again. The web UI offers the same through "Save as mode".
- **Failures explained.** `diagnosis` (and `diagnose_process`) says what broke
  and who has to act:
  - **you:** missing env keys, with hints from `.env.example` and the file to
    add them to;
  - **the agent, after your approval:** installs, containers, migrations,
    with the exact commands;
  - **the agent directly:** code errors.
- **See it working.** `probe` sends a request to a running process and returns
  the response plus the log lines that request produced. Useful probes can be
  saved as `checks` and re-run with `run_checks`.
- **Authenticated probes without sharing secrets.** Add test credentials (JWT,
  cookies, API keys, tenant ids, or a login recipe that fetches and refreshes
  a token) on the web UI's Credentials page. `probe` attaches the project's
  default profile, or `credential=<name>`, so the agent calls your API as a
  real user and sees the logs the request produced, without ever seeing the
  values. `credential_proxy` opens a short-lived local proxy for curl,
  Playwright or e2e runs. Values are encrypted at rest (file key or OS
  keychain) and scrubbed from every result and log.
- **Verify a change in one call.** `verify_change` restarts the process (or,
  with `restart=false`, waits for a hot-reloading dev server to rebuild),
  waits until it is live while watching `fail_pattern`, runs the saved checks,
  and reports only the errors the change introduced, with `verdict` pass or
  fail.
- **Errors, not log pages.** `get_errors` groups what a process printed into
  compile errors, panics, exceptions, test failures, browser errors, lint
  errors and error log lines, each with a count and the file, line and column
  (project files first). Pass `baseline=<cursor>` to mark which groups are new.
- **Read only what is new.** `get_logs`, `wait_for_log` and `get_errors`
  return a `cursor`; pass it back as `since` to skip what you already read.
  `wait_for_log(since="now")` ignores lines printed before the call, so an old
  "compiled" line cannot match after an edit, and `fail_contains` /
  `fail_pattern` end the wait early with `failed=true`. `get_logs(compact=true)`
  folds repeated lines and dependency stack frames.
- **Test runs as results.** `wait_for_exit` on a go test, pytest, jest,
  vitest, cargo, dotnet, rspec, mocha or phpunit run returns `tests` with
  passed, failed, skipped, total and the failing test names.
- **Whole stacks.** `start_stack(apps=["web"])` starts `web` and everything
  it `depends_on`, in order, waiting for each to be ready and reusing apps that
  are already running.
- **Frontend errors.** `browser_console(process_id)` opens a local proxy in
  front of a dev server. Pages loaded through it report `console.error`,
  uncaught exceptions, unhandled rejections and failed requests into the
  process's logs as `[browser] error: ...` lines with source file and line, so
  `get_errors` and `verify_change` see them. Hot-module reload and WebSockets
  keep working.
- **Restart on change.** An app's `watch:` (a path, a list, or
  `{paths, ignore, debounce}`) restarts its running or crashed processes when
  watched files change, ignoring `.git`, `node_modules`, build output and
  editor temp files. `reload: restart` restarts running processes when the
  app's config changes; the default `manual` marks them stale.
- **Git hygiene.** jack-ketch lists the files it uses that git would track:
  its `.jack-ketch/` dir, env files that saved apps load, and integration
  files. It offers to add them to `.gitignore` (`update_gitignore`).

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
```

Requires Go (see `go.mod` for the minimum version).

## License

MIT — see [LICENSE](LICENSE).
