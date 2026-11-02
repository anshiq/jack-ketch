---
title: "Running processes"
description: "The tool patterns an agent uses to start, watch, restart and stop work, and which tool fits which situation."
weight: 10
---

An agent drives jack-ketch through MCP tools. The rule of thumb is: pull, don't push. Logs are never streamed into the conversation; the agent asks for what it needs.

## The loop

{{< steps >}}
### Re-attach before starting

`list_processes` shows everything earlier sessions started, including recently exited processes. `list_apps` shows declared apps and their modes. Starting a duplicate on the same port is the most common mistake this avoids.

### Start and wait for readiness

```text
start_process(app="api", mode="dev", timeout_ms=30000)
```

With `timeout_ms` the call blocks until the app is ready. Without it, `start_process` returns a `process_id` immediately, and you follow with `wait_for_log(process_id, ready=true)`.

For a one-shot command such as a build, test or migration, pass a raw command and read the exit code:

```text
start_process(command="go", args=["test", "./..."], timeout_ms=120000)
```

### Read logs on demand

```text
get_logs(process_id, stream="stderr", lines=100)
get_logs(process_id, contains="status=500")
search_logs(query="panic|fatal", regex=true, level="error")
```

`get_logs` reads one process. `search_logs` searches every process in the project, history and live buffers. Never poll `get_logs` in a loop. Block with `wait_for_log` instead.

### Restart after a change

```text
restart_process(process_id)
wait_for_log(process_id, ready=true)
```

The `process_id` and its log history are kept.

### Stop and clean up

`stop_process` sends SIGTERM to the whole process group, then SIGKILL after the grace period (5 seconds by default). `remove_process` frees the buffers of a terminated process.
{{< /steps >}}

## Which tool for which situation

| Situation | Tool |
|---|---|
| Lost a `process_id` | `list_processes`, never restart blindly |
| Is it up or crashed? | `process_status`: state, pid, exit code, health, restart policy, and a diagnosis when failing |
| Wrong `PATH`, venv or port | `get_process_env`, secrets masked by default |
| Process wants input | `send_stdin(process_id, "y\n")` |
| Reload config or interrupt | `signal_process(process_id, "SIGHUP")` or `"SIGINT"` |
| React to crashes without polling | `subscribe_events`, drain with `get_events`, release with `unsubscribe_events` |
| Auto-recover an ad-hoc process | `set_restart_policy(process_id, "on-failure")` |
| Run a command inside the app's environment | `open_shell(app="api")`, then `send_stdin` and `get_logs` |

## What belongs under supervision

Declare long-lived development processes only: dev servers, watchers, backends, workers, plus app-tied builds, tests and migrations. Each gets readiness detection, log buffering with search, health checks and restart supervision.

Do not declare `docker`, `docker compose`, `kubectl`, core utilities like `ls` or `grep`, package managers or version-control commands as apps. They have no readiness line and no health endpoint, and supervising them pollutes `search_logs`. Run them in your own shell.

## Make your app easy to supervise

- Print a stable readiness line, byte for byte as the framework does, and never as JSON.
- Send logs below `WARNING` to stdout and `WARNING` and above to stderr.
- Write one event per line: a message followed by stable `key=value` pairs such as `method=GET path=/health status=200 duration_ms=3 request_id=ab12f9`. Then `contains="status=500"` and `contains="request_id=ab12f9"` just work.
- Run Python with `PYTHONUNBUFFERED=1`, or readiness lines sit in a buffer.
- Take configuration from the environment and shut down cleanly on SIGTERM.
- Never log secrets.
