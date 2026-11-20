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
| Processes | Every managed process, with start, stop, restart and signal actions, for one process or a selection. A process page adds logs, errors, environment, resource usage and a terminal |
| Apps | The apps and modes declared for the project, with badges for apps that restart on file changes (`watch`) or on config changes (`reload: restart`) |
| Ports | Every TCP port listening as you: which managed process holds it, which app declares it, and conflicts where a declared port is taken by something else. Managed holders can be stopped; other holders can be sent SIGTERM after a confirmation |
| Logs | Tail, filter and search captured output, and export a view or a single process. Redaction is on by default |
| Timeline | One time axis per process with restarts, crashes, automatic restarts, config changes, agent actions and error-rate bars. It points out when a process started logging errors and what happened just before |
| Events | Process lifecycle events, with history kept per the retention setting |
| Audit | Every mutating call and policy denial, attributed to a session and agent, with secrets redacted |
| Config | Edit the project YAML with validation, review revisions and roll back, and handle pending auto-detect proposals |
| Credentials | Create credential profiles and login recipes, manage the key store, start and stop proxies |
| Integrations | Detect agents, preview and install MCP entries and skills, and see each harness's connection state |
| Projects and Sessions | Known projects and workspaces, and the agents currently attached |
| Settings | Log level, idle exit, event retention (7, 30 or 90 days), run-saving suggestions, and shutting the daemon down (which stops every managed process) |

The process view also offers **Save as mode** for a hand-built run that worked.

The **Errors** tab on a process groups what it printed into build errors, exceptions and panics, test failures, lint errors, browser errors and error log lines, each with a count and its file and line (files in your project are highlighted). It refreshes while the process runs, can scan the current run or everything retained, and **Copy for agent** copies a compact list ready to paste. **Capture browser errors** opens a local proxy in front of a dev server; pages loaded through it report console errors, uncaught exceptions, unhandled promise rejections and failed requests into the process logs, and hot reload keeps working.

When a new jack-ketch version has been installed while the daemon runs, a banner at the top says which version is waiting and how many running processes a restart would stop, lists them on demand, and copies `jack-ketch daemon restart` for you to run when you are ready. Dismissing it hides it until the next update.

Path fields across the UI autocomplete as you type. The directory listing is read-only and never reads file contents.

## Security of the listener

- The TCP listener is off by default. `jack-ketch web` enables it for loopback only.
- A bearer token is required and compared in constant time. It is never placed in a cookie.
- Requests need a `Host` of `127.0.0.1` or `localhost` and a matching `Origin`. There is no permissive CORS.
- Operations on credentials such as saving, revealing and key changes are accepted here and refused on the agent socket.

If the page is blank, the daemon is probably down. The UI shows a "daemon not running" screen when it cannot connect. Start it with `jack-ketch daemon start` and reload.
