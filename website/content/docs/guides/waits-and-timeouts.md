---
title: "Waits and timeouts"
description: "Why a wait returns early with clamped=true, how per-agent caps work and how to get the full limit."
weight: 20
---

Clients differ in how long a single MCP tool call may block. jack-ketch caps every wait per call for the calling client, so a long wait never turns into a client-side timeout error.

## The caps

| Client | Cap per call |
|---|---|
| Claude Code, Codex, Gemini CLI, Goose, Cline | 600 s |
| Roo Code | 290 s |
| Every other client | 50 s |

Configs written by `jack-ketch integrate install` pass `--harness <id>`, which is how the daemon knows the right cap. Clients it only recognizes by name default to 50 s. `get_project_info` shows the cap for the current client, and `jack-ketch serve --max-wait-ms` overrides it.

## Clamped waits

When a wait is cut short, the result has `clamped=true` and `still_running=true`. That means the process is fine; the call just reached its cap. Call the wait tool again.

```text
r = wait_for_log(process_id, ready=true, timeout_ms=300000)
r.clamped and r.still_running  ->  wait_for_log(process_id, ready=true, since=r.cursor, timeout_ms=300000)
```

Passing `since=r.cursor` continues exactly where the previous call stopped, so no line printed in between is skipped.

## Waiting for a rebuild

`wait_for_log` matches lines of the current run, including ones already printed. That is right after a start, but after an edit that a hot-reloading dev server rebuilds without restarting, an old "compiled" line would match at once. Pass `since="now"`, and add `fail_pattern` so a failed rebuild ends the wait with `failed=true` instead of a timeout:

```text
wait_for_log(process_id, contains="compiled successfully", since="now", fail_pattern="(?i)error|failed to compile")
```

This applies to `wait_for_log`, `wait_for_exit` and `start_process` with `timeout_ms`.

Clients that support MCP progress notifications receive periodic progress while a wait is pending.

## Troubleshooting a slow start

| Symptom | Likely cause |
|---|---|
| `wait_for_log` times out while the process is running | Output is buffered, or the readiness line does not match. Check stdout, or set `readiness:` on the app |
| Process exited during the wait | A startup crash. The result carries a `diagnosis` |
| "Tool call timed out after ~60 s" | The entry was written without `--harness`. Reconnect with `integrate install` |

> [!TIP]
> Prefer one `start_process(..., timeout_ms=...)` over a start followed by manual polling. It returns the readiness result and the diagnosis in a single call.
