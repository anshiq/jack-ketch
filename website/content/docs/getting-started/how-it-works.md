---
title: "How it works"
description: "The daemon, the MCP bridge, project identity and where state lives."
weight: 30
---

## Three components

| Component | Role |
|---|---|
| **`jackketchd`** | The always-on daemon, one per OS user. Owns the project registry, process supervision, the log pipeline with FTS5 search and a global event bus, all backed by a single-writer SQLite `state.db`. |
| **`jack-ketch`** | The CLI and MCP bridge. `jack-ketch serve` is a thin client that connects to the daemon lazily on the first tool call. Bare `jack-ketch` prints the command help; `jack-ketch web` opens the web UI. |
| **`jack-ketch-shim`** | A tiny, dependency-free process shim (at most 4 MB RSS) designed to stay put under a running process across a daemon upgrade. It is not wired into the upgrade path yet. |

```
agent ──MCP──▶ jack-ketch serve ──unix socket──▶ jackketchd ──▶ your processes
                                                     ▲
web UI ──────────── authenticated loopback TCP ──────┘
```

## Processes belong to the daemon

Managed processes are children of `jackketchd`. They outlive agent sessions, which is what lets a later session re-attach, but they stop when the daemon stops. That is why the first thing an agent does is `list_processes`.

> [!WARNING]
> Because processes are the daemon's children, stopping or restarting the daemon can stop them. Run `jack-ketch ps` afterwards and restart anything that went away before you ask an agent to continue.

## Projects and workspaces

Tools act on the session's project. The daemon binds it automatically from the client's working folder, resolved through a locator chain: path, then device and inode, then git remote, then fingerprints. A project is therefore stable across moves, re-clones and worktrees, and a new session in a known folder just works.

Clients without a project folder, Claude Desktop being the common one, pass `workspace="/abs/path"` on a call or call `set_workspace` once per conversation. `list_projects` shows what jack-ketch knows.

```sh
jack-ketch project ls
jack-ketch project resolve .
jack-ketch project gc
```

## Where state lives

Everything is outside your repository.

```
~/.local/share/jack-ketch/
├── state.db            SQLite (WAL)
├── logs/<workspace>/   FTS5 index and segments
├── trash/
└── backups/

$XDG_RUNTIME_DIR/jack-ketch/
├── jackketchd.sock
├── jackketchd.pid
└── jackketchd.lock
```

Project config is YAML text inside `state.db`, with a revision history, so there is no `.jack-ketch/` folder to commit. jack-ketch will still offer to add the few files it does create to your `.gitignore`.

## Config layering

Built-in defaults, then daemon defaults, then the project, then the workspace overlay. The project config is generated automatically from run-defining files, kept in sync, and never silently overwrites an app you edited: a needed update becomes a pending proposal that you approve or dismiss.

## Read more

- [Architecture](../../reference/architecture/) for the component diagram and API services.
- [Security model](../../reference/security/) for threats and mitigations.
- [Decision records](../../reference/decisions/) for why it is built this way.
