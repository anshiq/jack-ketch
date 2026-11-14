# Troubleshooting

`jack-ketch doctor` explains itself; this page maps every check to an
action. `doctor --json` for tooling, `doctor --fix` repairs permissions where
safe.

| doctor check | Meaning | Action |
|---|---|---|
| daemon-socket | Missing or not mode `0600` | `jack-ketch daemon start`; `doctor --fix` for mode; check `XDG_RUNTIME_DIR` ownership |
| runtime-dir / data-dir / config-dir | Missing or not mode `0700` | start the daemon once to create them; `doctor --fix`; never run as root |
| token-file | Web/HTTP token not mode `0600` | `doctor --fix` |
| daemon-reachable | Socket exists but refuses | read `jackketchd.log` under `$XDG_STATE_HOME/jack-ketch/`; `jack-ketch daemon restart` |
| daemon-version | Daemon version differs from the CLI | `jack-ketch daemon restart` (the daemon is upgraded with the CLI). Restarting stops every managed process; the command lists them and asks first (`--yes` skips the question and is required when not run from a terminal) |
| daemon-binary | `jackketchd` not found next to `jack-ketch` | reinstall so both binaries sit in the same directory |
| web-listener | Off (fine) or configured but unreachable | `jack-ketch web` |
| integration:\<agent\> | Connected / outdated / broken / not connected | `jack-ketch integrate install <agent>`; "not connected" is informational |
| skills | An installed skill is older than the embedded one | `jack-ketch integrate skills install --agent <id>` |
| legacy-dir | A v2 `.jack-ketch` directory is still present | `jack-ketch migrate --dry-run --all-known`, then `migrate --cleanup` |
| systemd | User unit exists but is inactive | `systemctl --user enable --now jackketchd.socket` |

## Common issues

- **Two daemons for one user** (systemd + manual): single user-level flock —
  the second exits with a clear message; `doctor` flags it.
- **Processes stopped after an update**: managed processes are children of
  the daemon, so `daemon stop`, `daemon restart` and finishing an update stop
  them all. After installing a new version, the web UI banner,
  `jack-ketch daemon status`, `jack-ketch web` and the MCP `update_notice`
  say how many processes a restart will stop; restart when it suits you, then
  start the apps again (`start_stack` brings a whole stack back). Apps with
  `autostart: true` start again when their project is next used.
- **Processes from before a daemon crash**: on boot the daemon marks runs
  whose pid is gone as exited (`daemon-restart`); processes that are still
  alive are adopted.
- **Stale workspaces**: `jack-ketch project gc` after moves/deletes.
- **The same app is listed in two projects**: a folder inside another
  managed workspace was registered before the enclosing project defined its
  apps, so both configs describe it. The Projects and Apps pages show a
  "lives inside" banner; `jack-ketch project nested` lists the pairs and
  `jack-ketch project merge <child-id> --into <parent-id>` (add `--dry-run`
  to preview) folds the child into the parent, moves its credential
  profiles and forgets the child. Stop the child's processes first. New
  folders inside a workspace whose config already defines their apps attach
  to that workspace automatically.
- **"already running in project … with the same command and workdir"**:
  another project is running the same command in the same folder (usually the
  duplicate above). Stop it or merge the projects; the web UI offers "Start
  anyway", the API takes `force: true` on ProcessService/Start.
- **Blank page**: the web UI shows the "daemon not running" screen when the
  daemon is down; start it and reload.
- **Invalid YAML save**: the last good revision stays active; the web UI and
  CLI show file/line/column from `config.invalid`.
- **"Tool call timed out after ~60 s"**: waits are capped per call for the
  calling client (50 s for most; 600 s for Claude Code, Codex, Gemini CLI,
  Goose and Cline; 290 s for Roo Code). When a result has `clamped=true` and
  `still_running=true`, call the wait tool again. Connect through
  `integrate install` so the entry passes `--harness <id>` and the cap is
  raised to the client's real limit.
- **Claude Desktop does not know my project**: it has no working folder. Ask
  Claude to call `set_workspace` with the project path once per conversation,
  or pass `workspace=` on a call.
- **"Config is read-only"**: the file is managed by Nix/home-manager/dotfiles.
  `integrate install` refuses to touch it; run `integrate show <agent>` and
  add the entry to your declarative config instead.
- **Skills appear twice**: clients share skill folders (for example
  `~/.agents/skills` is read by Cursor, VS Code and Gemini CLI, while Claude
  Code reads `~/.claude/skills`). Remove one copy with
  `jack-ketch integrate skills remove --agent <id>`, or keep both if the
  duplication is only visible, not harmful.
