---
name: jack-ketch-ready
description: Use when scaffolding, creating, or modifying any runnable application (web server, worker, CLI, backend service), or when starting, checking, debugging, or restarting one, so it works with the jack-ketch MCP process supervisor. Covers the app-side contract (readiness line, clean streams, graceful shutdown, env-only config) and which MCP tool to use in which situation (start_process, wait_for_log, get_logs, search_logs, restart_process, signal_process, open_shell, subscribe_events).
metadata:
  version: "8"
---

# jack-ketch-ready

jack-ketch is the only way to run long-lived processes in this project. It owns the process, buffers its stdout/stderr, and exposes it through MCP tools. Logs are never pushed, you pull them. `start_process` returns a `process_id` immediately unless you pass `timeout_ms`, and the process keeps running in the background, even after your session ends.

Write the app so the supervisor can understand it, then drive it with the right tool at the right moment (Tool patterns).


## Tool patterns

Never launch a dev server from your own shell. Never start a second copy to see a change. Never poll `get_logs` in a loop, block with `wait_for_log` instead.

| Situation | Do this |
|---|---|
| Session start, or before starting anything | `list_processes` to re-attach to what earlier sessions started. `list_sessions` shows who else is attached. `list_apps` shows declared apps |
| Start a server | `start_process(app="<name>", timeout_ms=30000)` blocks until ready, or `start_process(app="<name>")` then `wait_for_log(process_id, ready=true)` |
| Run a one-shot command (build, test, migration) | `start_process(command=..., timeout_ms=120000)` blocks until exit, read `wait.exit_code` |
| Lost a `process_id` | `list_processes`, never restart blindly |
| Is it up or crashed | `process_status(process_id)`: state, pid, exit code, `health`, `restart_policy` |
| Startup hung | `wait_for_log` timed out with process running means buffered output or a mismatched readiness line, check stdout. Process exited means startup crash, check stderr |
| Something is failing | `get_logs(stream="stderr", lines=50)` first, then narrow with `contains=` or `pattern=` |
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
| Change app declarations | `get_project_info`, then `validate_config`, `plan_config`, `apply_config` (config lives in daemon DB, see jack-ketch-project-config) |
| Runtime itself looks slow or lossy | `runtime_stats` |
| Pending auto-detect proposal | surface `get_project_info.pendingProposal` to the user, then `resolve_config_proposal` approve/dismiss (never auto-approve) |

`start_process` without `timeout_ms` and `restart_process` never block, `wait_for_log` and `wait_for_exit` block up to `timeout_ms`. Every wait is capped per call for your client: when a result has `clamped=true` and `still_running=true`, call the wait tool again to keep waiting. With `timeout_ms`, `start_process` fills `wait`: `ready` for a matched readiness line, `exited` plus `exit_code` for a finished process, `timeout` when neither happened and the process is still running. Confirm errors on stderr before stdout. A vague bug report means gather input first: failing action, endpoint, expected versus actual, then reproduce against the port and filter on the ids that run produces. One targeted filter beats a blind dump.

Clients that expose MCP prompts and resources also offer `/start_app`, `/diagnose` and `/status`, plus the resource `jack-ketch://processes/{process_id}/logs` with the last 200 lines.

## Verify a new or changed app

1. `start_process(app="<name>")`.
2. `wait_for_log(process_id, ready=true, timeout_ms=30000)`.
3. Curl the port or health endpoint.
4. `signal_process(process_id, "SIGINT")`.
5. `process_status(process_id)` shows exit 0.
6. `get_process_env(process_id)` shows `PORT` and friends from the right layers.

On failure: `get_logs(stream="stderr", lines=50)`, fix, `restart_process`, `wait_for_log(ready=true)` again.

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
