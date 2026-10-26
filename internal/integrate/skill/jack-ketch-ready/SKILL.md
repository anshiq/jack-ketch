---
name: jack-ketch-ready
description: Use when scaffolding, creating, or modifying any runnable application (web server, worker, CLI, backend service), or when starting, checking, debugging, verifying, or restarting one, so it works with the jack-ketch MCP process supervisor. Covers the app-side contract (readiness line, clean streams, graceful shutdown, env-only config), which MCP tool to use in which situation (start_process with app modes, wait_for_log, get_logs, search_logs, diagnose_process, probe, run_checks, restart_process, signal_process, open_shell, subscribe_events), and the ask-the-user flows for saving working runs, fixing failures, verifying changes at runtime and keeping jack-ketch files out of git.
metadata:
  version: "9"
---

# jack-ketch-ready

jack-ketch is the only way to run long-lived processes in this project. It owns the process, buffers its stdout/stderr, and exposes it through MCP tools. Logs are never pushed, you pull them. `start_process` returns a `process_id` immediately unless you pass `timeout_ms`, and the process keeps running in the background, even after your session ends.

Write the app so the supervisor can understand it, then drive it with the right tool at the right moment (Tool patterns).


## Tool patterns

Never launch a dev server from your own shell. Never start a second copy to see a change. Never poll `get_logs` in a loop, block with `wait_for_log` instead.

| Situation | Do this |
|---|---|
| Session start, or before starting anything | `list_processes` to re-attach to what earlier sessions started. `list_sessions` shows who else is attached. `list_apps` shows declared apps and their modes |
| Start a server | `start_process(app="<name>", mode="<mode>", timeout_ms=30000)` blocks until ready, or `start_process(app="<name>")` then `wait_for_log(process_id, ready=true)`. Omit `mode` for the app's `default_mode` |
| App runs several ways (dev, prod, headless, test, staging) | Each way is a mode of the app with its own command and env; pick it with `mode=`. Never rebuild a command line when a saved mode exists |
| Run a one-shot command (build, test, migration) | `start_process(command=..., timeout_ms=120000)` blocks until exit, read `wait.exit_code` |
| Lost a `process_id` | `list_processes`, never restart blindly |
| Is it up or crashed | `process_status(process_id)`: state, pid, exit code, `health`, `restart_policy`, plus `diagnosis` when failing |
| Startup hung | `wait_for_log` timed out with process running means buffered output or a mismatched readiness line, check stdout. Process exited means startup crash, read the `diagnosis` |
| Something is failing | Read `diagnosis` from the start/wait/status result, or call `diagnose_process(process_id)`; then `get_logs(stream="stderr", lines=50)` narrowed with `contains=` or `pattern=` |
| A run worked and was started by hand (raw command, or app with extra env) | The result carries `save_suggestion`: ask the user, then `save_run_config` (or `action="dismiss"`) |
| Show a fix or feature working | `probe(process_id, url="/path", method=..., body=...)`: response + the logs that request produced |
| Re-verify after a change | `run_checks(app=..., mode=...)` runs the saved checks in one call |
| Find an error across processes | `search_logs` searches the whole workspace (history + live) with optional `stream=`, `level=`, `context=`; `get_logs` reads one process |
| Trace one request | `get_logs(contains="request_id=<id>")` |
| Slow paths | `get_logs(pattern="duration_ms=[0-9]{4,}", contains="path=...")` |
| Client has no project folder | `set_workspace("/abs/path")` once per conversation, or pass `workspace=` on calls; `list_projects` lists known projects |
| Code or config changed | `restart_process(process_id)`, then `wait_for_log(ready=true)`. Same `process_id`, history kept |
| Need the exit result | `wait_for_exit(process_id, timeout_ms=...)` |
| Wrong PATH, venv, or port | `get_process_env(process_id)`, redacted by default |
| Run a command inside the app's environment | `open_shell(app="api")`, `send_stdin("...")`, `get_logs`, then `stop_process` on the shell |
| Process wants input | `send_stdin(process_id, "y\n")` |
| Ctrl+C or config reload | `signal_process(process_id, "SIGINT")` or `"SIGHUP"` |
| React to crashes without polling | `subscribe_events(process_id, types=["process.crashed","process.exited"])`, drain with `get_events`, release with `unsubscribe_events` |
| Ad-hoc process should auto-recover | `set_restart_policy(process_id, "on-failure")` |
| Done with it | `stop_process`, then `remove_process` to free its buffers |
| Change app declarations | `get_project_info`, then `validate_config`, `plan_config`, `apply_config` (config lives in daemon DB, see jack-ketch-project-config). To save how a process was just run, use `save_run_config` instead of hand-writing YAML |
| `gitignore.missing` in `get_project_info` or `save_run_config` | Ask the user; yes: `update_gitignore`, no: `update_gitignore(action="dismiss")` |
| Runtime itself looks slow or lossy | `runtime_stats` |
| Pending auto-detect proposal | surface `get_project_info.pendingProposal` to the user, then `resolve_config_proposal` approve/dismiss (never auto-approve) |

`start_process` without `timeout_ms` and `restart_process` never block, `wait_for_log` and `wait_for_exit` block up to `timeout_ms`. Every wait is capped per call for your client: when a result has `clamped=true` and `still_running=true`, call the wait tool again to keep waiting. With `timeout_ms`, `start_process` fills `wait`: `ready` for a matched readiness line, `exited` plus `exit_code` for a finished process, `timeout` when neither happened and the process is still running. Confirm errors on stderr before stdout. A vague bug report means gather input first: failing action, endpoint, expected versus actual, then reproduce against the port and filter on the ids that run produces. One targeted filter beats a blind dump.

Clients that expose MCP prompts and resources also offer `/start_app`, `/diagnose`, `/verify` and `/status`, plus the resource `jack-ketch://processes/{process_id}/logs` with the last 200 lines.

## Save runs that work (always ask first)

When a start, wait or status result carries `save_suggestion`, the run succeeded (ready, stable for a while, or a one-shot that exited 0) and is not saved yet.

1. Ask the user the `ask` question and show the `yaml` preview. Offer to rename the app or mode and to keep it only for this checkout (`scope="workspace"`).
2. Yes: `save_run_config(process_id, app, mode, description=...)`. The command, workdir, env file and env vars are stored as an ordered `env_sources` list (later wins). Add `checks=[...]` with probe results worth repeating.
3. No: `save_run_config(process_id, action="dismiss")` so it is not suggested again.
4. Next time: `start_process(app=..., mode=...)`. A different way of running the same app (prod, headless, test) is a new mode on the same app, not a new app.

Never save without a yes. Saving does not touch repository files.

## When a process fails

Start, wait and status results carry `diagnosis` (or call `diagnose_process`). It names a `category`, the `evidence` lines, who has to act (`actor`), plain `steps` and `fixes`.

| actor | What you do |
|---|---|
| `user` (missing_env, invalid_credentials, missing keys/certs, remote service) | Do not invent values or edit secrets. Show the user the steps and `missing_env`: each key, the file to add it to (`add_to`), the hint and example value from `.env.example`. Wait, then `restart_process`. |
| `agent_with_approval` (missing_dependency, missing_tool, service_unreachable, migration_pending, toolchain_version) | Ask permission showing the exact `fixes` commands (e.g. `pnpm install`, `docker run ... postgres:16`, `python manage.py migrate`). Check for reusable containers first. Run installs in your own shell; run migrations as a one-shot `start_process`. Then `restart_process` and `wait_for_log(ready=true)`. |
| `agent` (code_error, port_in_use, out_of_memory) | Fix it yourself, then `restart_process`. For port_in_use reuse the running copy named in the summary instead of starting another. |

Once it runs, the result carries `save_suggestion`: offer to save the working setup.

## Show the change working (ask after tests pass)

After you finish a fix or feature and its tests pass, ask the user whether they want to see it working at runtime. If yes:

1. Reuse or start the affected app (`list_processes`, `start_process(app, mode)` or `restart_process`), `wait_for_log(ready=true)`.
2. Exercise the changed behaviour with 2-5 concrete interactions including an edge case: `probe` for HTTP (path, method, headers, body, `expect_status`, `expect_contains`), a one-shot `start_process(command=..., timeout_ms=...)` for CLIs and scripts, trigger + `wait_for_log` for workers and jobs.
3. Report expected vs actual with the key log lines from each probe. On failure call `diagnose_process`.
4. Offer to save the useful probes as checks: `save_run_config(process_id, app, mode, checks=[probe.check, ...])`. Later `run_checks(app, mode)` repeats them in one call.

`probe` runs GET/HEAD/OPTIONS against localhost directly. Other methods, or any non-local target, need the user's approval: ask, then pass `confirmed=true`. Never point probes at production data.

## Keep jack-ketch files out of git

`get_project_info` and `save_run_config` report `gitignore.missing`: jack-ketch's `.jack-ketch/` state dir, env files that saved apps load (usually secrets), and project-scope integration files. Ask the user whether to add them to `.gitignore`. Yes: `update_gitignore`. No: `update_gitignore(action="dismiss")`. If some are already committed, tell the user that `git rm --cached` is needed to untrack them; do not run it without their OK.

## Verify a new or changed app

1. `start_process(app="<name>")`.
2. `wait_for_log(process_id, ready=true, timeout_ms=30000)`.
3. `probe(process_id, url="/health")` (or curl the port).
4. `signal_process(process_id, "SIGINT")`.
5. `process_status(process_id)` shows exit 0.
6. `get_process_env(process_id)` shows `PORT` and friends from the right layers.

On failure: read `diagnosis` (or `diagnose_process`), then `get_logs(stream="stderr", lines=50)`, fix, `restart_process`, `wait_for_log(ready=true)` again.

## Behaviors to know

- `stop_process` sends SIGTERM to the whole process group, then SIGKILL after `stop_grace`. A manual stop never restarts the app.
- Supervised apps auto-restart per `restart_policy`. An exhausted budget shows `crashed`, a manual `restart_process` resets it.
- Event subscriptions are capped at 8 per client and 256 events each. `get_events` drains immediately and reports `dropped`.
- Processes are shared per project. Re-attach with `list_processes` instead of starting again.
- Throwaway processes (a shell, a scratch server) can pass `lifetime: "session"` to `start_process`: they are stopped about 30 seconds after your session ends (the default, `persistent`, outlives the session).

## Purpose

jack-ketch supervises long-lived development processes only: dev servers, watchers, backends, workers, plus app-tied one-shots (build, test, migration) that need readiness detection, log buffering, health checks, and restart supervision. It is not a generic remote-exec bridge.

## Avoid

Writing log files, multiline banners, ANSI colors, progress output, dumping env, hardcoded ports, swallowing SIGTERM, printing secrets.

Never run through this MCP — use your own shell or dedicated tools instead:

- Container and orchestration CLIs: `docker`, `docker compose`, `podman`, `kubectl`, `helm`, image builds/pushes, registry logins.
- Core-utils and file inspection: `ls`, `cat`, `head`, `tail`, `grep`, `rg`, `find`, `echo`, `wc`, `ps`, `kill`, `chmod`, `chown`, `cp`, `mv`, `rm`, `tar`, ad-hoc `curl`/`wget` probing (curling the app `/health` endpoint to verify readiness is fine).
- Host package and service managers: `apt`, `brew`, `systemctl`, `service`, `systemd-run`.
- VCS and local editing ops: `git` status/diff/log, file reads/writes.

Such commands have no readiness line, no health endpoint, and no restart semantics. Running them here wastes log buffers, pollutes `search_logs`, and orphans process groups.

Out of scope: pure libraries and one-shot scripts with no long-lived process, though env-only config still applies. Dockerized workflows are out of scope: supervise the dev process directly (e.g. `go run .`, `npm run dev`), not the container around it.
