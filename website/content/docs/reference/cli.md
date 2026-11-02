---
title: "CLI"
description: "Every jack-ketch command, with what it is for."
weight: 20
---

Bare `jack-ketch` prints the command help. `jack-ketch web` opens the web UI.

## Commands

| Command | Purpose |
|---|---|
| `jack-ketch web` | Start the daemon on demand, print the token URL and open the web UI |
| `jack-ketch serve` | Run the MCP server over stdio, or `--http ADDR` |
| `jack-ketch integrate` | Detect, install, verify and remove MCP client config and skills |
| `jack-ketch doctor` | Diagnose the daemon, socket, permissions and integrations |
| `jack-ketch daemon` | Manage the daemon, autostart and its TCP listener |
| `jack-ketch ps` | List managed processes |
| `jack-ketch start`, `stop`, `restart` | Control a process from the shell |
| `jack-ketch logs` | Show process logs |
| `jack-ketch run <cmd> [args...]` | Start a process in the foreground and tail its output |
| `jack-ketch shell <app\|proc>` | Interactive shell inside a process's environment |
| `jack-ketch repl` | Interactive process manager |
| `jack-ketch config` | `show`, `export` and `edit` the project YAML |
| `jack-ketch project` | `ls`, `show`, `resolve`, `rename`, `link`, `unlink`, `forget`, `gc` |
| `jack-ketch sessions` | List connected agent sessions |
| `jack-ketch migrate` | Import legacy v2 state. See [Migrating to v3](../migrate-v3/) |
| `jack-ketch version` | Print the version |

## `serve`

| Flag | Meaning |
|---|---|
| `--harness <id>` | The client this entry identifies as. Raises the wait cap to that client's real limit |
| `--http <addr>` | Serve over streamable HTTP, for example `:7341` |
| `--token`, `--token-file` | Bearer token for `--http` |
| `--allow-remote` | Allow binding a non-loopback interface with `--http` |
| `--project <path\|id>` | Workspace to serve. Default is the current project |
| `--max-wait-ms <n>` | Cap for wait tools. `0` uses the per-harness limit |
| `--toolset full\|core` | Which tool set to expose |
| `--check` | Verify daemon connectivity and the tool list, print one JSON line and exit |

## `integrate`

```sh
jack-ketch integrate list                      # detection and connection status
jack-ketch integrate install <agent>           # diff first, then write
jack-ketch integrate install --detected        # every detected agent
jack-ketch integrate show <agent>              # the snippet, without writing
jack-ketch integrate remove <agent>
jack-ketch integrate verify <agent>            # spawn the entry, list tools
jack-ketch integrate skills install --agent <agent>
jack-ketch integrate hooks install claude-code
```

`--scope user|project|local` picks where the entry is written, for agents that support it. See [Agent integrations](../../integrations/).

## `daemon`

```sh
jack-ketch daemon start       # start in the background
jack-ketch daemon status
jack-ketch daemon logs        # tail the daemon log
jack-ketch daemon restart
jack-ketch daemon stop        # graceful shutdown of its processes
jack-ketch daemon install     # autostart: systemd user socket, LaunchAgent or logon task
jack-ketch daemon uninstall
```

## `doctor`

```sh
jack-ketch doctor            # human-readable
jack-ketch doctor --json     # for tooling
jack-ketch doctor --fix      # repair permissions where safe
```

Every check and the action that resolves it is in [Troubleshooting](../troubleshooting/).

## Environment

| Variable | Effect |
|---|---|
| `XDG_RUNTIME_DIR` | Where the socket, pid and lock files live |
| `XDG_STATE_HOME` | Where `jackketchd.log` is written |
| `JACK_KETCH_NO_KEYCHAIN=1` | Force the file backend for the credential key |
| `CODEX_HOME` | Codex config and instruction file location |
