---
title: "Quick start"
description: "Connect your agents, open the web UI and run a supervised dev server in five minutes."
weight: 20
---

{{< steps >}}
### Connect your agents

```sh
jack-ketch integrate install --detected
```

This connects every detected MCP client. For each one it prints the diff, asks before writing, backs up the previous file to `<file>.jack-ketch.bak` and writes atomically. Restart the agent afterwards if it asks you to.

To connect a single agent:

```sh
jack-ketch integrate install claude-code
jack-ketch integrate verify claude-code
```

`verify` spawns the written entry and initializes the MCP server, so you know the agent can really start it.

### Install the skills (optional but recommended)

```sh
jack-ketch integrate skills install --detected
```

The skills teach an agent the tool patterns: re-attach before starting, wait instead of polling, save a run only after you agree. For Claude Code you can also add the guardrail hook:

```sh
jack-ketch integrate hooks install claude-code
```

### Open the web UI

```sh
jack-ketch web
```

This starts the daemon if needed, prints a token URL and opens your browser. Processes, logs, config, integrations and credentials all live there.

### Ask your agent to run your app

Open your project in the agent and say something like:

> Start the api in dev mode, wait until it is ready, hit /health and tell me what the logs say.

Behind the scenes the agent calls `list_processes`, `list_apps`, `start_process`, `wait_for_log` and `probe`. The process keeps running after the session ends. A new session re-attaches with `list_processes`.

### Save what worked

When a hand-built run succeeds, the result carries a `save_suggestion`. Your agent asks you, and on a yes calls `save_run_config` so next time it is just `start_process(app="api", mode="dev")`. Nothing is saved without your agreement.
{{< /steps >}}

## What you can run

The daemon scans a project's run-defining files and generates a config on first sight, so common stacks work without writing YAML. Built-in profiles recognize Next.js, Spring Boot, Django, Node, Python and Go, each with readiness patterns. See [Project config](../../guides/project-config/).

## Next

- [How it works](../how-it-works/) explains the daemon, bridge and project identity.
- [Waits and timeouts](../../guides/waits-and-timeouts/) explains `clamped=true`.
- [Agent integrations](../../integrations/) lists every client's entry and caveats.
