---
title: "Diagnosing failures"
description: "How jack-ketch classifies a failing process and decides who has to act: you, the agent, or the agent after your approval."
weight: 40
---

When a process fails to start or keeps erroring, start, wait and status results carry a `diagnosis`. `diagnose_process(process_id)` returns the same analysis on demand.

A diagnosis has a `category`, `evidence` lines from the logs, plain `steps`, concrete `fixes` and, most importantly, an `actor`: who has to do something next.

## Who acts

| Actor | Typical categories | What happens |
|---|---|---|
| **`user`** | `missing_env`, `invalid_credentials`, missing keys or certs, a remote service | The agent does not invent values or edit secrets. It shows you the steps and a `missing_env` list: each key, the file to add it to, a hint and an example from `.env.example`. You add the value, then the agent restarts. |
| **`agent_with_approval`** | `missing_dependency`, `missing_tool`, `service_unreachable`, `migration_pending`, toolchain version | The agent asks permission, showing the exact commands (`pnpm install`, `docker run ... postgres:16`, `python manage.py migrate`). Approved installs run in its own shell, migrations run as a one-shot `start_process`, then it restarts. |
| **`agent`** | `code_error`, `port_in_use`, `out_of_memory` | The agent fixes it itself and restarts. For `port_in_use` it reuses the copy named in the summary instead of starting another. |

## Example

```json
{
  "category": "missing_env",
  "actor": "user",
  "summary": "api exited at startup: DATABASE_URL is not set",
  "missing_env": [
    { "key": "DATABASE_URL", "add_to": ".env", "hint": "Postgres connection string", "example": "postgres://localhost:5432/app" }
  ]
}
```

## Outcome and saving

Once a process runs, jack-ketch classifies the outcome: a server that reached readiness or stayed up, or a task that exited `0`. A successful run that is not yet saved carries a `save_suggestion`. The agent asks you; on a yes it calls `save_run_config`, on a no it dismisses the suggestion for good. Saving never touches repository files.

> [!NOTE]
> Fixes that change your machine always need your approval, and anything involving secrets is yours to supply. This is intentional: the diagnosis tells the agent where its authority ends.
