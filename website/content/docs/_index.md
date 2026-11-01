---
title: "Documentation"
description: "Everything you need to run jack-ketch, connect your agents and trust what they do on your machine."
weight: 1
---

jack-ketch is a per-user background daemon that starts, watches and signals long-running dev processes for AI coding agents. An agent kicks off a dev server, walks away, and later asks whether it is up, what it logged and why it failed, instead of blocking a session on a foreground command.

It ships as three small binaries, a web UI and an MCP server that works with {{< agent-count >}} agents.

## Start here

{{< steps >}}
### Install
One script on Linux and macOS, one on Windows. It verifies the SHA-256 checksum. See [Installation](getting-started/installation/).

### Connect your agents
`jack-ketch integrate install --detected` finds your MCP clients, shows the diff and asks before writing. See the [quick start](getting-started/quick-start/).

### Ask your agent to run something
It calls `start_process`, waits for readiness, reads logs and probes the endpoint. See [how it works](getting-started/how-it-works/).
{{< /steps >}}

## Find your way

- **New to jack-ketch?** Read [How it works](getting-started/how-it-works/) for the daemon, bridge and projects.
- **Wiring up an agent?** See [Agent integrations](integrations/) for the exact entry each of the {{< agent-count >}} clients gets.
- **Running a real project?** The [guides](guides/) cover modes, env layers, health checks, runtime checks and credentials.
- **Looking something up?** Use the [MCP tool reference](reference/mcp-tools/), the [CLI reference](reference/cli/) or press <kbd>/</kbd> to search.
- **Something broke?** [Troubleshooting](reference/troubleshooting/) maps every `jack-ketch doctor` check to an action.
