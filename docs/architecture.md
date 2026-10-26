# jack-ketch v3 Architecture

## Overview

jack-ketch v3 transforms the system from a per-project, session-scoped MCP supervisor into a **per-user background platform**: an always-on daemon, a central project registry, a versioned API, and a web UI.

## Components

```
┌─────────────────────────────────────────────────────┐
│                   jackketchd (1 per OS user)            │
│                                                     │
│  ┌─────────────────────────────────────────────┐    │
│  │              Engine (multi-tenant)           │    │
│  │  ┌──────────┐  ┌──────────┐  ┌───────────┐ │    │
│  │  │ Project  │  │ Project  │  │ Project   │ │    │
│  │  │ Runtime  │  │ Runtime  │  │ Runtime   │ │    │
│  │  │ (ws A)   │  │ (ws B)   │  │ (ws C)    │ │    │
│  │  └──────────┘  └──────────┘  └───────────┘ │    │
│  └─────────────────────────────────────────────┘    │
│  ┌──────────────┐  ┌──────────────┐                │
│  │ Session      │  │ Project      │                │
│  │ Registry     │  │ Registry     │                │
│  └──────────────┘  └──────────────┘                │
│  ┌──────────────┐  ┌──────────────┐                │
│  │ LogPipeline  │  │ EventBus     │                │
│  │ (segments +  │  │ (global)     │                │
│  │  FTS5 index) │  │              │                │
│  └──────────────┘  └──────────────┘                │
│  ┌──────────────┐                                   │
│  │ state.db     │  SQLite (WAL), single writer     │
│  └──────────────┘                                   │
└─────────────────────────────────────────────────────┘
            │
     ┌──────┼──────┐
     │      │      │
      ▼      ▼      ▼
    CLI    MCP    Web     (CLI/MCP use pkg/client; the web UI talks to the daemon API)
   agent-  bridge web UI
   runtime serve
```

## Key Design Decisions

1. **Per-user daemon** — one daemon hosts all projects
2. **Daemon-owned processes** — managed processes are children of `jackketchd`: they outlive agent sessions (so a later session re-attaches), but they stop when the daemon stops. `jack-ketch-shim` exists for a future upgrade-survival path but is not wired yet
3. **MCP bridge** — `jack-ketch serve` (stdio or `--http`) is a thin client: it connects to the daemon lazily on the first tool call, binds the session to a workspace (`--project`, the working directory, or `set_workspace`), and re-registers automatically if the daemon restarts
4. **Locator chain** — workspace resolution via path, dev/inode, git
5. **Connect-RPC** — versioned API over Unix sockets
6. **Central storage** — state.db under `~/.local/share/jack-ketch`
7. **Config layering** — built-in → daemon defaults → project → workspace (YAML text in state.db project_configs, auto-generated, self-syncing)

## Directory Layout

```
~/.local/share/jack-ketch/
├── state.db              SQLite (WAL)
├── trash/
│   └── projects/<legacy yaml imports>
├── logs/<ws_*/>
│   ├── index.db          SQLite FTS5
│   └── segments/
├── trash/
└── backups/

$XDG_RUNTIME_DIR/jack-ketch/
├── jackketchd.sock
├── jackketchd.pid
├── jackketchd.lock
├── shims/<instance_id>/
└── jackketchd.log (in $XDG_STATE_HOME)
```

## API Services

| Service | RPCs | Purpose |
|---------|------|---------|
| SystemService | GetVersion, Health, GetStats, Shutdown | Daemon info |
| ProjectService | Resolve, ListProjects, LinkWorkspace, ForgetProject, GC | Project identity |
| ConfigService | GetConfig, Plan, Apply, ListRevisions, Rollback, ResolveProposal, WatchConfig | DB-backed config with auto-sync |
| ProcessService | Start, Stop, Restart, Signal, List, WatchProcesses, Attach | Process lifecycle |
| LogService | GetLogs, SearchLogs, TailLogs, ExportLogs | Log access |
| EventService | WatchEvents, ListEvents | Event streaming |
| SessionService | Register, Heartbeat, ListSessions | Session management |
| IntegrationService | ListHarnesses, InstallMCP, InstallSkills | Integrations |
| AuditService | ListAudit | Audit trail |

## Migration Path

v2 → v3 migration is automatic:
1. Resolve → new project + workspace
2. Import legacy `logs.db` and `audit.log`
3. Adopt v2 daemon processes via the legacy adopt path
4. v3 daemon reconnects all shims on boot

## Performance Budgets

| Metric | Budget |
|--------|--------|
| jack-ketch serve cold start | < 150 ms |
| Workspace resolve (warm) | < 5 ms |
| StartProcess RPC | < 30 ms p99 |
| Ingest throughput | ≥ 100k lines/s |
| Aggregate ingest | ≥ 300k lines/s |

## Security Model

- UDS socket: `0700` directory, `0600` socket, `SO_PEERCRED` uid check
- TCP listener: off by default, bearer token, strict Host check
- Secrets in env redacted; reveal is policy-gated and audited
- DB/log files `0600`; data dir `0700`
