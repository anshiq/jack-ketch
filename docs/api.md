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
| GetVersion | `{daemonVersion, apiVersion, minClientVersion, update}`; clients below minimum get an upgrade error. `update` is the same object GetUpdateStatus returns |
| GetUpdateStatus | `{}` → `{pending, runningVersion, installedVersion?, executable, runningProcesses, processes[{processId, app, command, status, workspaceId, workspacePath}], notice?}`. `pending` is true when the `jackketchd` binary on disk was replaced (and reports a different `--version`) since the daemon started; `notice` is a ready-to-show sentence saying that restarting finishes the update and stops the running managed processes |
| Health | liveness + uptime |
| GetStats | projects, processes (loaded runtimes), sessions |
| GetRuntimeStats | `{workspaceId}` required; runtime health for one workspace: process counts by status, log-pipeline drops, uptime |
| GetSettings / UpdateSettings | `log_level`, `idle_exit`, `retention` (event history: `7d`/`30d`/`90d`; audit history is always kept 90 days), `suggest_saving_runs` (offer to save successful raw runs as app modes; nothing is saved without the user) |
| ListDirectory | `{dir?, prefix?, baseDir?, dirsOnly?, limit?}` → `{dir, entries:[{name, isDir}], truncated, error?}`; read-only path autocomplete for the web UI. Relative `dir` resolves against `baseDir` (default home), `~` expands to home, dotfiles are included, directories sort first, and results are capped (default 100, max 500). File contents are never read. |
| EnableTCP | `{addr}`; starts the authenticated loopback listener used by the web UI, creates the token file if needed; loopback addresses only; `{addr, started}` (idempotent: an existing listener is returned with `started: false`) |
| Shutdown | `{}` → `{draining: true}`: stops every managed process gracefully, then the daemon. The old `keepProcesses` field is ignored |

## ProjectService

Resolve (path → project/workspace via the locator chain), ListProjects,
GetProject, UpdateProject (rename), ListWorkspaces, LinkWorkspace,
ForgetProject (soft delete, 30-day trash), GC (missing paths),
ListNestedProjects, MergeNestedProject.

Resolve attaches a folder to the nearest enclosing workspace whose project
config already defines an app inside that folder, instead of creating a
second project; the response carries `attachedSubpath` (the folder relative to
that workspace). Workspaces of forgotten projects no longer resolve, and their
folder can be registered again.

ListNestedProjects returns every project whose folder sits inside another
live workspace whose config defines apps there: `{nested: [{child, parent,
apps: [{name, workdir, status, parentApp}], running}]}` where `status` is
`duplicate` (same workdir and command), `conflict` (same name and workdir,
different command) or `unique` (only in the child).

MergeNestedProject `{childProjectId, parentProjectId, dryRun}` copies the
child's apps, modes and checks into the parent config (parent values win,
unique apps are added with a workdir relative to the parent), moves the
child's credential profiles (a name that exists in the parent stays behind),
unlinks the child's workspaces and forgets the child project. It refuses
while the child has running processes. `dryRun` returns the same report
without changing anything.

## ConfigService

GetConfig (raw layers + resolved apps + provenance + config path),
GetSchema (JSON Schema for editors), Validate (line/col errors),
Plan (terraform-style diff: added/removed/restart-required/applied-live),
Apply (`base_revision` optimistic concurrency → `stale_revision` on
conflict), ListRevisions, GetRevision, Rollback, WatchConfig (stream).

| RPC | Notes |
|---|---|
| SaveRun | `{process_id, action?: save\|dismiss, app?, mode?, scope?: project\|workspace, description?, set_default?, overwrite?, env_sources?, checks?}`; writes the process's command, workdir and env (as ordered `env_sources`) as `apps.<app>.modes.<mode>`; revisions get source `saved`; `dismiss` stops suggesting that run. Returns `{saved, app, mode, start_with, yaml, gitignore?}` |
| GitignoreStatus | `{workspaceId}` → `{repo, missing:[{path, kind, reason}], tracked, suggestion}` for jack-ketch files git would track (`.jack-ketch/`, env files loaded by saved apps, project-scope integration files) |
| UpdateGitignore | `{workspaceId, entries?, action?: add\|dismiss}`; appends under a `# jack-ketch` header, or remembers the user's no |

## ProcessService

Start (returns immediately; refuses with `failed_precondition` when another project is already running the same command in the same folder, unless `force: true`), Stop, Restart, Signal, SendStdin, Remove,
Get, List (`workspaceId` or `allWorkspaces`), WatchProcesses (stream),
WaitForExit (the result carries `tests: {framework, passed, failed,
skipped, total, failures}` when the output contains a go test, pytest, jest,
vitest, cargo, dotnet, rspec, mocha or phpunit summary), SetRestartPolicy, GetEnv (redacted unless policy allows),
OpenShell (defaults `lifetime: session`), Attach (server-stream output +
SendStdin input; full bidi with codegen), GetResourceUsage,
WatchResourceUsage. Start takes `mode` with `app`.

| RPC | Notes |
|---|---|
| Diagnose | `{processId}` → `{category, actor: user\|agent\|agent_with_approval, summary, issues[{evidence, steps, fixes[{command, needs_approval}]}], missing_env[{key, hint, example, add_to}], service?, guidance}` |
| Outcome | `{processId}` → `{success, kind: server\|task, reason: ready\|stable\|exit_0\|ran_until_stopped, ready_line, uptime_ms}` |
| SaveSuggestion | `{processId}` → `{suggestion}` (null when the run failed, is already saved, was dismissed, or suggestions are off) |
| Probe | `api.ProbeRequest` → status, headers, body and the log lines the process printed during the request; `{needsConfirmation, reason}` for non-GET/HEAD/OPTIONS or non-loopback targets until `confirmed: true`. `credential` selects a credential profile (empty = the project default, `none` = no credentials); the result carries `credential`, `credential_status` (`ok`, `expiring`, `expired`, `empty`, `rejected`), `credential_refreshed` and `credential_hint`, and every secret value is scrubbed from the body, headers, error, URL, check and log lines |
| RunChecks | `{process_id \| workspaceId+app+mode, names?}` → per-check results with logs, `passed`, `failed`; a check's `credential` is attached, but never to a non-local host |
| GetErrors | `{process_id, since?, baseline?, limit?}` → `{process_id, instance_id, groups[{fingerprint, kind, message, count, first_seen, last_seen, first_id, stream, locations[{file, line, column, in_project}], sample, new}], total_errors, new_groups, scanned_from, cursor, truncated}`. `kind` is `compile_error`, `panic`, `exception`, `test_failure`, `browser_error`, `lint_error` or `log_error`. `since` defaults to `instance` (current run); `baseline` is a log cursor, and groups already printed before it get `new: false`. Default limit 20 |
| StartStack | `{workspaceId, apps?, modes?{app: mode}, timeoutMs?, noReuse?}` → `{started[{app, processId, instanceId, mode, ready, reused}], order, pulledIn?, failed?, failedProcessId?, error?}`. `depends_on` dependencies are added and started first (cycles and unknown apps are rejected); apps already running in the requested mode are reused; empty `apps` means every declared app; each app is awaited until ready (`timeoutMs` per app, default 30 s, max 600 s) and the call stops at the first failure |
| StartBrowserProxy | `{processId, url?, port?, ttlMinutes?, warnings?}` → `{id, processId, workspaceId, upstream, url, warnings, createdAt, expiresAt, requests, reports, pagesInstrumented}`. Opens `127.0.0.1:<random>` in front of the process's dev server (local hosts only), injects `/__jack-ketch/console.js` into HTML pages and appends browser `console.error` (and `console.warn` with `warnings`), uncaught exceptions, unhandled rejections and 5xx/network failures to the process's stderr as `[browser] error: ...` lines. WebSockets pass through. Default 60 minutes, max 12 hours; a second start for the same process returns the open proxy. Removing the process closes its proxies |
| ListBrowserProxies / StopBrowserProxy | `{workspaceId?}` → `{proxies}`; `{workspaceId, id}` → `{stopped}` |

## CredentialService

Per-project credential profiles for authenticated runtime probes. Every
call takes `workspaceId` (the project is derived from it). Calls marked
**web** are refused on the agent socket and only accepted on the
authenticated web listener, so an agent connected through MCP can use a
profile but never read or change one.

| RPC | Notes |
|---|---|
| List | `{workspaceId}` → `{profiles[{id, name, description, environment, default, apps, fields[{name, kind, target, secret, value?, has_value, expires_at}], recipe?, cookies[{name, host, path, expires}], capture_cookies, status, expires_at, last_used_at, last_refresh_at, last_error}], hint?}`. Values of secret fields and cookie values are never returned |
| Upsert **web** | `{workspaceId, profile:{id?, name, description?, environment?, default?, apps?, fields[{name, kind: bearer\|basic\|header\|cookie\|query\|variable, target?, secret, value, keep?}], recipe?, capture_cookies?}}`. An empty `value` with `keep: true` keeps the stored secret. One default per project. Secrets are encrypted at rest |
| Delete **web** | `{workspaceId, id}` |
| Reveal **web** | `{workspaceId, id, field}` (`cookie:<name>` for a jar cookie) → `{value}`; audited without the value and denied when restricted mode sets `reveal_secrets: false` |
| ClearJar **web** | `{workspaceId, id}` empties the captured cookies |
| Refresh | `{workspaceId, id, processId \| upstream, port?}` runs the profile's login recipe now and returns the updated profile |
| ListProxies / StartProxy / StopProxy | Short-lived loopback proxies that attach a profile to every forwarded request. `StartProxy {workspaceId, profile, processId \| upstream, port?, ttl_minutes?, label?, confirmed}` answers `{needsConfirmation, reason}` until `confirmed: true`; default 30 minutes, at most 8 hours |
| GetKeystore | `{backend, active, keychain_available, keychain_tool, keychain_error, key_path}` |
| SetKeystore **web** | `{backend: auto\|file\|keychain}` moves the encryption key between the 0600 key file and the OS keychain without re-encrypting anything |
| ResetKey **web** | `{confirm: "reset"}` creates a new key and clears every stored secret (use when the key is lost) |

## LogService

GetLogs (MCP-compatible tail; `source` may be `segments`; `since` takes a
cursor, `instance` or `all` and the result carries `cursor` for the next
call; `compact: true` folds repeated lines (`count`, `last_id`) and
dependency stack frames (`folded`), reporting `compacted_lines`), SearchLogs
(FTS over the workspace index + bounded ring scan; `truncatedScan` flag),
TailLogs (stream: backlog + live), WaitForLog (`since`: `instance`
(default), `now` or a cursor; `fail_contains`/`fail_pattern` end the wait
with `failed: true`; results carry `cursor` and `start_cursor`), ClearLogs, ExportLogs
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
