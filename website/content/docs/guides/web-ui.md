---
title: "Web UI"
description: "The browser interface for processes, logs, config, integrations and credentials."
weight: 80
---

```sh
jack-ketch web
```

This starts the daemon on demand, prints the web UI's URL and opens your browser. The URL carries a token. The listener is bound to loopback only.

## Pages

| Page | What it shows |
|---|---|
| Overview | A summary of what is running across your projects |
| Processes | Every managed process, with start, stop, restart and signal actions, for one process or a selection. A process page adds logs, environment, resource usage and a terminal |
| Apps | The apps and modes declared for the project |
| Logs | Tail, filter and search captured output, and export a view or a single process. Redaction is on by default |
| Events | Process lifecycle events, with history kept per the retention setting |
| Audit | Every mutating call and policy denial, attributed to a session and agent, with secrets redacted |
| Config | Edit the project YAML with validation, review revisions and roll back, and handle pending auto-detect proposals |
| Credentials | Create credential profiles and login recipes, manage the key store, start and stop proxies |
| Integrations | Detect agents, preview and install MCP entries and skills, and see each harness's connection state |
| Projects and Sessions | Known projects and workspaces, and the agents currently attached |
| Settings | Log level, idle exit, event retention (7, 30 or 90 days) and run-saving suggestions |

The process view also offers **Save as mode** for a hand-built run that worked.

Path fields across the UI autocomplete as you type. The directory listing is read-only and never reads file contents.

## Security of the listener

- The TCP listener is off by default. `jack-ketch web` enables it for loopback only.
- A bearer token is required and compared in constant time. It is never placed in a cookie.
- Requests need a `Host` of `127.0.0.1` or `localhost` and a matching `Origin`. There is no permissive CORS.
- Operations on credentials such as saving, revealing and key changes are accepted here and refused on the agent socket.

If the page is blank, the daemon is probably down. The UI shows a "daemon not running" screen when it cannot connect. Start it with `jack-ketch daemon start` and reload.
