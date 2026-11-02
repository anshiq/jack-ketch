---
title: "MCP tools"
description: "All tools the jack-ketch MCP server exposes, grouped by what they do."
weight: 10
---

jack-ketch exposes {{< tool-count >}} tools over MCP through `jack-ketch serve` (stdio, or `--http ADDR`). Tools marked **read-only** never change state. `--toolset core` exposes a reduced set for clients with tight tool limits.

Every wait tool is capped per call for the calling client. When a result has `clamped=true` and `still_running=true`, call it again. See [Waits and timeouts](../../guides/waits-and-timeouts/).

Clients that support MCP prompts and resources also get `/start_app`, `/diagnose`, `/verify` and `/status` prompts, and a `jack-ketch://processes/{process_id}/logs` resource with the last 200 lines.

{{< tools >}}

## Common parameters

| Parameter | Used by | Meaning |
|---|---|---|
| `process_id` | Most process tools | The id returned by `start_process` |
| `workspace` | Most tools | Absolute project path, for clients without a working folder. Omit to use the session's project |
| `timeout_ms` | `start_process`, `wait_for_log`, `wait_for_exit` | How long to wait, clamped to the client's cap |
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

## `search_logs`

```text
search_logs(query="timeout", stream="stderr", context_lines=2)
search_logs(query="panic|fatal", regex=true, process_ids=["p_1", "p_2"])
```

All words in `query` must match unless `regex=true`. Searches the whole project by default, with `max_rows` defaulting to 200 and capped at 1000.

## `probe`

See [Runtime verification](../../guides/runtime-verification/) for every parameter. `GET`, `HEAD` and `OPTIONS` to localhost run immediately. Everything else needs `confirmed=true` after you approve.
