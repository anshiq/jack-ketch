---
title: "MCP tools"
description: "All tools the jack-ketch MCP server exposes, grouped by what they do."
weight: 10
---

jack-ketch exposes {{< tool-count >}} tools over MCP through `jack-ketch serve` (stdio, or `--http ADDR`). Tools marked **read-only** never change state. `--toolset core` exposes a reduced set for clients with tight tool limits.

If `get_project_info` carries `nested`, the project overlaps with another one that defines the same apps; do not start apps from both, and ask the user to merge them (Projects page, or `jack-ketch project merge`).

If `list_processes` or `get_project_info` carries `update_notice`, a new jack-ketch version is installed and restarting the daemon to finish it stops every managed process; tell the user before anything restarts the daemon.

Every wait tool is capped per call for the calling client. When a result has `clamped=true` and `still_running=true`, call it again. See [Waits and timeouts](../../guides/waits-and-timeouts/).

Clients that support MCP prompts and resources also get `/start_app`, `/diagnose`, `/verify` and `/status` prompts, and a `jack-ketch://processes/{process_id}/logs` resource with the last 200 lines.

{{< tools >}}

## Common parameters

| Parameter | Used by | Meaning |
|---|---|---|
| `process_id` | Most process tools | The id returned by `start_process` |
| `workspace` | Most tools | Absolute project path, for clients without a working folder. Omit to use the session's project |
| `timeout_ms` | `start_process`, `wait_for_log`, `wait_for_exit`, `verify_change`, `start_stack` | How long to wait, clamped to the client's cap |
| `since` | `get_logs`, `wait_for_log`, `get_errors` | A `cursor` from an earlier result to read only newer lines, `instance` for the current run, `now` (waits only) or `all` |
| `app`, `mode` | `start_process`, `run_checks`, `open_shell` | A declared app and the way to run it |

## `start_process`

```text
start_process(app="api", mode="dev", timeout_ms=30000)
start_process(command="npm", args=["test"], timeout_ms=120000)
```

Prefer `app=` from `list_apps`; otherwise pass `command` and `args`. Without `timeout_ms` it returns a `process_id` immediately. With it, the result includes a `wait` object: `ready` for a matched readiness line, or `exited` with an `exit_code`.

## `get_logs`

```text
get_logs(process_id, stream="stderr", lines=100, contains="status=500", level="error")
get_logs(process_id, pattern="duration_ms=[0-9]{4,}")
```

Bounded by `lines` (default 100). Filter by `stream` (`stdout` or `stderr`), `contains` (substring), `pattern` (regex) and `level` (`debug`, `info`, `warn`, `error`). Start with `stream=stderr` when diagnosing.

Every result carries a `cursor`. Pass it back as `since` to read only the lines printed after that call instead of the same tail again; `since="instance"` limits the read to the current run. `compact=true` folds repeated lines into one entry with `count` and collapses dependency stack frames, reporting `compacted_lines`.

```text
get_logs(process_id, since="1842")
get_logs(process_id, compact=true, lines=200)
```

## `wait_for_log`

```text
wait_for_log(process_id, ready=true)
wait_for_log(process_id, contains="compiled successfully", since="now", fail_pattern="(?i)error|failed to compile")
```

By default it matches lines of the current run, including ones already printed. After editing code that a hot-reloading dev server rebuilds without restarting, pass `since="now"` so an old line cannot match. `fail_contains` / `fail_pattern` end the wait early with `failed=true`. Results carry `cursor` (where the scan stopped) and `start_cursor`; after `clamped=true`, call again with `since=<cursor>` so no line is skipped.

## `get_errors`

```text
get_errors(process_id)
get_errors(process_id, baseline="1842")
```

Groups the errors a process printed instead of returning raw lines. Each group has `kind` (`compile_error`, `panic`, `exception`, `test_failure`, `browser_error`, `lint_error`, `log_error`), `message`, `count`, a short `sample` and `locations` with file, line and column; files inside the project come first with `in_project=true`. It understands tsc, go, rustc, C#, vite/webpack, Python tracebacks, Node/Java/.NET exceptions, Go panics, pytest, jest, vitest, go test and eslint output. It scans the current run by default (`since`); `baseline` is a cursor taken before your change, and groups already printed before it get `new=false`.

## `verify_change`

```text
verify_change(process_id)
verify_change(process_id, restart=false, wait_contains="compiled successfully", fail_pattern="(?i)error")
```

One call after an edit: restarts the process (or, with `restart=false`, waits for a hot-reloading server to print `wait_contains` / `wait_pattern`), waits until it is live (the readiness line, or staying up for `settle_ms` when the app has none), runs the app's saved checks, and lists `new_errors` that appeared since the call. `verdict` is `pass` or `fail` with a one-line `summary` and per-step results; a failed run includes the `diagnosis`.

## `start_stack`

```text
start_stack(apps=["web"], modes={"api": "dev"})
```

Starts the listed apps and everything they `depends_on`, dependencies first, waiting for each to be ready (`timeout_ms` per app, default 60000). Apps already running in the requested mode are reused, so calling it again after fixing a failure picks up where it stopped. Empty `apps` starts every declared app. On failure the result names the app and includes its diagnosis.

## `wait_for_exit`

For test runs (go test, pytest, jest, vitest, cargo, dotnet, rspec, mocha, phpunit) the result carries `tests` with `passed`, `failed`, `skipped`, `total` and the failing test names. Call `get_errors` for their file and line.

## `browser_console`

```text
browser_console(process_id="p_web")
browser_console(action="list")
browser_console(action="stop", id="bp_...")
```

Opens `http://127.0.0.1:<port>` in front of a dev server running on this machine. Pages loaded through it report `console.error` (and `console.warn` with `warnings=true`), uncaught exceptions, unhandled promise rejections and failed (5xx or network) requests into that process's logs as `[browser] error: ...` lines with the source file and line, so `get_errors`, `wait_for_log` and `verify_change` see them. Hot-module reload and other WebSockets pass through. The proxy stays open for `ttl_minutes` (default 60, max 720) and closes when the process is removed.

## `search_logs`

```text
search_logs(query="timeout", stream="stderr", context_lines=2)
search_logs(query="panic|fatal", regex=true, process_ids=["p_1", "p_2"])
```

All words in `query` must match unless `regex=true`. Searches the whole project by default, with `max_rows` defaulting to 200 and capped at 1000.

## `probe`

See [Runtime verification](../../guides/runtime-verification/) for every parameter. `GET`, `HEAD` and `OPTIONS` to localhost run immediately. Everything else needs `confirmed=true` after you approve.
