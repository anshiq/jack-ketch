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
r.clamped and r.still_running  ->  wait_for_log(process_id, ready=true, timeout_ms=300000)
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
