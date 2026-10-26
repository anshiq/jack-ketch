# jack-ketch v3 API reference

Versioned contract (`jackketch.v1`) defined in `api/proto` and served
with Connect-compatible RPC over the Unix socket
(`$XDG_RUNTIME_DIR/jack-ketch/jackketchd.sock`), and optionally over
authenticated loopback TCP for the web UI.

One schema produces the SDKs: `buf generate` emits the Go bindings
(`gen/jackketch/...`, incl. connect-go clients) and the TypeScript
bindings (`ui/src/gen`). Generated code is committed; `make proto`
(checks generate+lint+breaking) is CI-gated. The daemon serves
Connect-compatible paths with JSON bodies today; the Connect-protocol
transport (headers/h2) is a swap that keeps these paths.

Every endpoint is `POST /api/jackketch.v1.<Service>/<Method>` with a
JSON body. Debug with curl:

```sh
curl --unix-socket "$XDG_RUNTIME_DIR/jack-ketch/jackketchd.sock" \
  -H 'Content-Type: application/json' -d '{}' \
  http://jackketchd/jackketch.v1.SystemService/GetVersion
```

Streams (TailLogs, WatchProcesses, WatchEvents, WatchConfig) are
newline-delimited JSON: a snapshot first, then deltas, each with an opaque
`cursor`. Reconnect with `resume_cursor`; a 15s `heartbeat` keeps proxies
alive; slow consumers get `Gap{dropped}` instead of backpressure (§5.4).

## SystemService

| RPC | Notes |
|---|---|
| GetVersion | `{daemonVersion, apiVersion, minClientVersion, shimProtocol}`; clients below minimum get an upgrade error |
| Health | liveness + uptime |
| GetStats | projects, processes (loaded runtimes), sessions |
| GetRuntimeStats | `{workspaceId}` required; runtime health for one workspace: process counts by status, log-pipeline drops, uptime |
| GetSettings / UpdateSettings | `log_level`, `idle_exit`, `retention` (event history: `7d`/`30d`/`90d`; audit history is always kept 90 days), `learn_raw_starts` |
| EnableTCP | `{addr}`; starts the authenticated loopback listener used by the web UI, creates the token file if needed; loopback addresses only; `{addr, started}` (idempotent: an existing listener is returned with `started: false`) |
| Shutdown | `{keepProcesses}` (default true): drain; false stops everything first |

## ProjectService

Resolve (path → project/workspace via the locator chain), ListProjects,
GetProject, UpdateProject (rename), ListWorkspaces, LinkWorkspace,
ForgetProject (soft delete, 30-day trash), GC (missing paths).

## ConfigService

GetConfig (raw layers + resolved apps + provenance + config path),
GetSchema (JSON Schema for editors), Validate (line/col errors),
Plan (terraform-style diff: added/removed/restart-required/applied-live),
Apply (`base_revision` optimistic concurrency → `stale_revision` on
conflict), ListRevisions, GetRevision, Rollback, WatchConfig (stream).

## ProcessService

Start (returns immediately), Stop, Restart, Signal, SendStdin, Remove,
Get, List (`workspaceId` or `allWorkspaces`), WatchProcesses (stream),
WaitForExit, SetRestartPolicy, GetEnv (redacted unless policy allows),
OpenShell (defaults `lifetime: session`), Attach (server-stream output +
SendStdin input; full bidi with codegen), GetResourceUsage,
WatchResourceUsage.

## LogService

GetLogs (MCP-compatible tail; `source` may be `segments`), SearchLogs
(FTS over the workspace index + bounded ring scan; `truncatedScan` flag),
TailLogs (stream: backlog + live), WaitForLog, ClearLogs, ExportLogs
(stream chunks), GetLogStats (index lag).

## EventService

WatchEvents (stream, `since` cursor), ListEvents (paged DB history: optional
`workspaceId`, `processId`, `types`, `since` event id, `before` cursor),
Subscribe/Drain/Unsubscribe (MCP-compat pull model). Subscribe and Drain
accept `workspaceId` and `since` (an event id to backfill from), plus
`processId` and `types`. Events carry `projectId`/`workspaceId`; response
keys are camelCase. History retention follows the `retention` setting
(default 30d).

## SessionService

Register, Heartbeat (stream held for the session lifetime; drop = close),
Ping (unary heartbeat for bridges), ListSessions, GetSession, Close, and
**Update**: patch `sessionId` plus any of `harness`, `harnessVersion`,
`clientName`, `workspaceId` (binds the session to a workspace). Missing
fields are left unchanged.

## IntegrationService

v2 shapes (all requests accept `harness`, `scope` (`user`/`project`/`local`,
default `user`), `projectPath` or `workspaceId`, and `action`
(`install`/`remove`):

| RPC | Request → response |
|---|---|
| ListHarnesses | `{}` (or `projectPath`/`workspaceId`) → `{harnesses: [...]}` with per-harness `detected`, `version`, `evidence`, `scopes` (per-scope `status`, `entries` with paths/problems), `manualSteps`, `restartHint`, `maxWaitMs`, `connected` (live sessions) |
| PreviewInstall | `{harness, scope, projectPath?, action?}` → `{changes: [Change], diff}` (unified diff text) |
| InstallMCP / RemoveMCP | same request → `{changes, path, backup, changed, restartHint}` |
| GetSnippet | same request → `{harness, scope, format, path, text, cli, deeplink, steps}` |
| Verify | same request → `{ok, checks: [{name, ok, detail, path}]}` (spawns the entry and initialises the MCP server) |
| ListSkills | `{}` → `{skills: [{name, version, description}]}` (embedded) |
| SkillStatus / InstallSkills / RemoveSkills | `{harness, scope, projectPath?}` → `{skills: [{name, want, have, dir, visible, outdated}]}` / `{removed: [dir]}` |
| CheckUpdates | `{}` → `{updates: [{harness, name, have, want, dir}], at}` |

`Change` is `{harness, scope, path, existed, changed, diff, backup,
restartHint, note}`. Install/remove write atomically and back up to
`<file>.jack-ketch.bak`; project-scope entries use the portable
`jack-ketch` command.

## AuditService

ListAudit (filterable, newest-first, secrets redacted). Every mutating RPC
plus every policy denial is recorded with session + harness attribution
(`args` included); history is kept 90 days.

## Long-poll routes

`ProcessService/WaitForExit` and `LogService/WaitForLog` are long-poll
requests: they block server-side up to the client's per-call cap and return
`clamped`/`still_running` when the wait was cut short, so callers repeat the
call. `start_process` uses the same clamping for `timeout_ms`. The MCP bridge
serves these over whichever transport the client speaks (stdio or HTTP).

## Errors

HTTP status + `{"error", "code"}`; structured details
(`policy_denied` + rule, `stale_revision`, `not_found`)
travel as `ErrorInfo` with codegen. Handshake header on every call:
`X-Jack-Ketch-Client: <kind>/<version>`.

## Casing

Requests accept camelCase; snake_case aliases are accepted for
MCP compatibility (`process_id` ↔ `processId`, `timeout_ms` ↔ `timeoutMs`,
`backlog_lines` ↔ `backlog`). Some responses remain snake_case for
MCP compatibility (`process_id` in List/Get, `cpuNanos`); the web UI
normalises to camelCase (`id`, `cpuPercent`). New fields are camelCase.
