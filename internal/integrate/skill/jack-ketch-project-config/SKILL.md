---
name: jack-ketch-project-config
description: Project config reference declaring how runnable apps are started, supervised, and env-configured (apps, modes such as dev/prod/headless/test, ordered env_sources of env files and inline vars, saved runtime checks, runtime env layers, health checks, restart, security).
metadata:
  version: "5"
---

# project config reference

The project config declares how runnable apps are started, supervised, and env-configured. It is stored in the daemon database (state.db), edited via the web Config page, `apply_config` or `jack-ketch config edit` — never via files in the repository. Use `jack-ketch config export` to print the current YAML. The `runtime:` section — env layers, `stop_grace`, buffer sizes, security, TTLs — is honoured in daemon mode exactly as below.

## Scope (what apps belong here)

Declare long-lived development processes only: dev servers, watchers, backends, workers, plus app-tied build/test/migration commands. Each app gets readiness detection, log buffering with FTS search, health checks, and restart supervision.

What to avoid: never declare `docker`, `docker compose`, `podman`, `kubectl`, core-utils (`ls`, `cat`, `grep`, `find`, `echo`, `ps`), host package managers, or VCS/file ops as apps. They have no readiness line or health endpoint, and supervising them wastes buffers and pollutes `search_logs`. Run those in your own shell instead. In `restricted` mode, keep them out of `allowed_commands` — allowlist project toolchains (`go`, `npm`, `python`, wrappers) rather than container or system CLIs.

## Full annotated template

```yaml
runtime:
  # Base env layer: "login" (default) captures the login-shell env once via
  # $SHELL -lic 'env -0' (~3s timeout, silent fallback to os.Environ()).
  # This is what makes nvm/pyenv/uv shims and their PATH entries reach managed
  # processes. Use "none" to inherit the parent env instead — pin "none" in
  # test configs where determinism matters.
  shell_env: login

  # Global env layer applied to EVERY app/process. Lower precedence than
  # env_file, app env, and request env.
  # env:
  #   - "NODE_ENV=development"

  # Log ring buffer size per stream (stdout/stderr). get_logs reads from here.
  log_buffer_lines: 10000

  # Grace period before SIGKILL after SIGTERM on stop_process (default 5s).
  stop_grace: 5s

  # How long to wait for graceful shutdown of everything on runtime exit.
  shutdown_timeout: 5s

  # Optional durable SQLite log archive (server-side persistence). The app
  # itself never writes log files.
  # log_store: sqlite

apps:
  # Each app is a name -> spec map. start_process(app="<name>") launches it.
  api:
    # Profile name — gives readiness detection + optional default command.
    type: node

    # Relative to the workspace root, then resolved through
    # filepath.EvalSymlinks (predictable uv/poetry walk-ups). Must exist — a
    # missing workdir fails the start BEFORE anything is exec'd.
    workdir: ./services/api

    # Explicit argv. Omit to use the profile's default command
    # (nextjs/node: detected package manager run dev; django:
    # python manage.py runserver; spring-boot: ./mvnw spring-boot:run /
    # ./gradlew bootRun; go: go run .; python: none).
    command: ["node", "server.js"]

    # Dotenv file, relative to the config file. Never logged. Higher
    # precedence than runtime.env, lower than this app's env block.
    # env_file: .env

    # Per-app env overrides, KEY=VALUE. Higher precedence than env_file.
    # env:
    #   - "LOG_LEVEL=info"
    #   - "PYTHONUNBUFFERED=1"

    # Ordered env layers, later wins (see "Env sources and precedence").
    # env_sources:
    #   - file: .env
    #   - file: .env.local
    #     optional: true
    #   - vars: {LOG_LEVEL: info}

    # Named ways to run this app (see "Modes").
    # default_mode: dev
    # modes:
    #   dev:
    #     command: [npm, run, dev]
    #   prod:
    #     command: [npm, start]
    #     env_sources:
    #       - vars: {NODE_ENV: production}
```

## Modes: one app, many ways to run it

An app's shared settings (workdir, type, env, checks) live on the app; each way of running it is a mode that overrides only what differs. `start_process(app="api", mode="prod")` picks a mode; omitting `mode` uses `default_mode` (or the only mode, or the app's own command).

```yaml
apps:
  api:
    workdir: ./services/api
    type: node
    default_mode: dev
    env_sources:
      - file: .env
    checks:
      - name: health
        url: /health
    modes:
      dev:
        description: local dev server with reload
        command: [npm, run, dev]
      headless:
        command: [npm, run, dev, --, --headless]
      prod:
        command: [npm, start]
        env_sources:
          - file: .env.production
          - vars: {NODE_ENV: production}
        checks:
          - name: health
            url: /healthz
            expect_status: 204
      test:
        command: [npm, test]
```

Mode fields: `description`, `command`, `workdir`, `type`, `env_sources`, `env_file`, `env`, `readiness`, `health_check`, `restart`, `checks`. A mode's env sources are layered after the app's; its checks replace app checks of the same name. Mode names use letters, digits, `.`, `_`, `-`.

Do not hand-write modes for a run that already worked: `save_run_config(process_id, app, mode)` records its exact command, workdir and env (after the user agrees to the `save_suggestion`). A workspace overlay (`scope="workspace"`) can add modes or change `default_mode` for one checkout only.

## Env sources and precedence

`env_sources` is an ordered list; later entries win, so the order is the priority. Each entry is either a dotenv file or inline vars:

```yaml
env_sources:
  - file: .env                 # lowest of the app's sources
  - file: .env.local
    optional: true             # skipped when missing (a missing non-optional file fails the start)
  - vars:                      # KEY: value mapping, or a list of KEY=VALUE
      PORT: 3000
      DATABASE_URL: postgres://localhost:5432/app
  - file: .env.override        # beats the vars above
    optional: true
```

Every process runs with a **complete merged environment**. Layers, lowest to highest precedence:

1. **Base env** — captured login-shell environment (`shell_env: login`, the default) or the parent process env (`shell_env: none`).
2. **`runtime.env`** — a runtime-wide layer applied to every app/process.
3. **App sources** — legacy `env_file`, then legacy `env`, then the app's `env_sources` in order.
4. **Mode sources** — the selected mode's `env_file`, `env`, then `env_sources` in order.
5. **Request `env_file`**, then **request `env`** passed to `start_process`.

Paths in `file:` resolve relative to the workspace root. `${port:name}` templates work in any vars value. `get_process_env` shows the merged result with per-key `source` provenance (`file:.env`, `vars`, `mode:prod/file:.env.production`, `request.env`).

Example — `PORT` set in every layer:

```yaml
runtime:
  env: ["PORT=3000"]          # layer 2
apps:
  web:
    type: node
    env_sources:
      - file: .env            # .env contains PORT=3001
      - vars: {PORT: 3002}    # later source wins over .env
```

`start_process(app="web", env=["PORT=3003"])` → the request layer wins → the process sees `PORT=3003`.

## Checks

`checks` are saved runtime probes that `run_checks(app, mode)` executes against the running process, returning pass/fail plus the logs each request produced.

```yaml
checks:
  - name: create-order
    method: POST                      # default GET
    url: /api/orders                  # path → the process's listening port (or PORT); or a full URL
    headers: {Authorization: "Bearer ${API_TOKEN}"}   # ${VAR} expands from the process env
    body: '{"sku": "A1", "qty": 2}'
    expect_status: 201                # default: any 2xx/3xx
    expect_contains: '"id"'
    timeout: 5s
  - name: db-port
    tcp: localhost:5432
```

## Monorepo

One config file, several independent services, each with its own workdir and
its own `.env` — same key, different values, no leakage:

```yaml
runtime:
  env: ["NODE_ENV=development"]   # shared default for every service
  log_buffer_lines: 10000

apps:
  api:
    type: node
    workdir: ./services/api
    env_sources:
      - file: ./services/api/.env     # PORT=3000, DATABASE_URL=...
    command: ["node", "server.js"]
  worker:
    type: python
    workdir: ./services/worker
    env_sources:
      - file: ./services/worker/.env  # PORT=4000, DATABASE_URL=... (same key, different value)
    command: ["python3", "-u", "worker.py"]
```

- Paths (`workdir`, `env_file`, `env_sources[].file`) resolve **relative to the workspace root**,
  then through `filepath.EvalSymlinks`.
- Each app's merged env is independent — `get_process_env(process_id)` per
  process shows exactly what that process got.
- A missing `workdir` fails the start during resolution, **before anything is
  exec'd or registered** — a typo fails loudly and fast, not silently.

## Profiles

| Profile | Detected by | Readiness patterns | Default command |
|---|---|---|---|
| `nextjs` | `package.json` contains `"next"` | `(?i)ready in`, `(?i)started server`, `(?i)local:\s*https?://`, `(?i)compiled successfully`, `(?i)ready` | detected package manager `run dev` (npm/pnpm/yarn/bun by lockfile) |
| `spring-boot` | `pom.xml` / `build.gradle` / `build.gradle.kts` | `Started \S+ in \d+(\.\d+)?s?`, `Tomcat started on port`, `Netty started on port`, `Jetty started on port`, `(UnderTow\|Undertow) started on port` | `./mvnw spring-boot:run` or `./gradlew bootRun` (by wrapper lockfile) |
| `django` | `manage.py` | `Starting development server at`, `Quit the server with`, `Uvicorn running on http` | `python manage.py runserver` |
| `node` | `package.json` | `(?i)listening on`, `(?i)server started`, `(?i)started server`, `(?i)ready` | detected package manager `run dev` |
| `python` | `requirements.txt` / `pyproject.toml` | `(?i)running on http`, `(?i)listening on`, `(?i)server started` | none |
| `go` | `go.mod` | `(?i)listening on`, `(?i)server started` | `go run .` |
| `generic` | fallback | none | none |

`wait_for_log(process_id, ready=true)` matches the app's profile readiness
patterns against captured log lines, one line at a time.

## Supervision (v2)

Supervision is **declarative and per-app**: jack-ketch notices crashes and
health failures on its own, without the agent polling. All fields are optional
(v1 files load unchanged). An app with none of these behaves exactly as in v1.

```yaml
apps:
  api:
    command: ["go", "run", "./cmd/api"]

    # Override the profile-detected readiness regexes for this app. A process
    # is "ready" once a captured log line matches. Omit to use the profile's.
    readiness:
      - 'listening on :8080'

    # Continuous health probe, run once the process is ready. After
    # failure_threshold consecutive failed probes the process is reported
    # unhealthy and, if the restart policy permits, restarted.
    health_check:
      http: "http://localhost:8080/healthz"   # or tcp: "localhost:8080" (set one)
      interval: 10s      # probe interval (default 10s)
      timeout: 3s        # per-probe timeout (default 3s, must be < interval)
      failure_threshold: 3  # consecutive failures that mark unhealthy (default 3)

    # Declarative auto-restart with exponential backoff.
    restart:
      policy: on-failure      # never (default) | on-failure | always
      backoff: [1s, 2s, 5s, 15s, 60s]   # capped-exponential steps (default)
      max_restarts: 10        # per rolling 10-minute window (default 10; 0 = no budget)
```

Rules:
- `on-failure` restarts only on non-zero exit; `always` restarts regardless of
  exit code.
- **A manual `stop_process` or shutdown never triggers a restart**, regardless
  of policy.
- Exhausting the restart budget marks the process `crashed` (process_status
  shows `status="crashed"`). Manual `restart_process` resets the budget.
- A process stable for a full 10-minute window has its budget reset.
- `set_restart_policy(process_id, policy)` opts an ad-hoc process (started
  without an app entry) into auto-restart.
- `process_status` reports `health` (unknown/healthy/unhealthy),
  `consecutive_failures`, `backoff_state`, and `restart_policy`.

## Security (v2)

Default `runtime.security.mode: trusted` — arbitrary exec, as today. Set
`restricted` for CI/shared machines:

```yaml
runtime:
  security:
    mode: restricted
    allowed_commands: ["go", "npm", "pnpm", "./mvnw", "python"]  # basename match
    allowed_workdirs: ["${project}", "${project}/../shared"]     # confinement
    allow_pid_attach: false   # disables open_shell(pid=)
    allow_stdin: true
    reveal_secrets: false     # hard-disables get_process_env(reveal=true)
```

In restricted mode, `start_process` with a non-allowlisted command or an
out-of-tree workdir fails with a structured `policy_denied` error and an audit
entry. Mutating tool calls and policy denials are recorded in the daemon's
audit history (kept 90 days, secrets redacted); `get_audit_log` reads it. In
restricted mode `reveal=true` is a policy error, not a silent ignore.

## Observability (v2)

- `runtime.metrics: ":9341"` — optional Prometheus scrape endpoint.
- `runtime.log_forward: { stdout: true, otlp: "http://localhost:4318/v1/logs" }`
  — best-effort, drop-on-backpressure log forwarding.
- `get_logs(..., level="error")` — filter structured logs by level
  (`level=...` / `"level":...` prefixes).
- `runtime_stats` — process counts by status and log-pipeline drop counters.

## Daemon

One per-user `jackketchd` hosts every project and is started on demand by the
first `jack-ketch serve`. `jack-ketch daemon start|status|stop|restart`
manage it. Managed processes outlive agent sessions but stop when the daemon
stops. There is no `runtime.daemon` key.

## HTTP transport

`jack-ketch serve --http 127.0.0.1:7341` serves the same MCP server over
streamable HTTP (a bare `:7341` binds loopback; other interfaces require
`--allow-remote`). The bearer token comes from `--token`, `--token-file`,
`JACK_KETCH_HTTP_TOKEN`, or the web UI token file. Select a project per
request with the `X-Jack-Ketch-Workspace` header or `?workspace=`. Tool
calls are rate-limited.

## Resource limits (v2)

Per-app `limits` bound machine impact (Linux-first via cgroup v2; degrades to
unlimited when unsupported):

```yaml
apps:
  api:
    command: ["go", "run", "./cmd/api"]
    limits:
      cpu: "1.0"       # cores, or "500m" (milli-cores)
      memory: "512M"   # 512M, 1G, 256MiB, ...
```

Each process gets its own cgroup v2 slice; `process_status` reports live
`memory_bytes`/`cpu_usage_nanos`; an OOM-kill surfaces to subscribers as
`process.crashed` with `reason: "oom"`.

## CLI equivalence

`jack-ketch run` mirrors the request-level env layer from the shell:

```bash
jack-ketch run --app api --env PORT=3003 --env-file ./services/api/.env
```

maps to `start_process(app="api", env=["PORT=3003"], env_file="./services/api/.env")`:
the `--env-file` becomes layer 3, `--env` becomes layer 5 (above the app's
`env:` block, above `runtime.env`). In `run` mode the process lifetime is tied
to the invocation; in MCP `serve` mode it outlives the call.

## Auto-generated apps

On project load and every start the daemon scans run-defining files (package.json scripts, go.mod, manage.py, pyproject, pom.xml, Procfile, Makefile, docker-compose) plus depth-1 subdirs and generates missing apps automatically. Generated apps are marked auto in the Apps page and revisions have source auto.

Raw starts are never saved silently. When one succeeds, tool results carry `save_suggestion`; the agent asks the user and only then calls `save_run_config` (revisions have source `saved`). The daemon setting `suggest_saving_runs` (web Settings page) turns the suggestions off.

## Keep jack-ketch files out of git

Config lives in the daemon database, not in the repository. The files jack-ketch may leave in a repo are its `.jack-ketch/` state dir, env files that saved apps load, and project-scope integration files. `get_project_info` lists the ones git would track under `gitignore.missing`; ask the user, then `update_gitignore` (or `action="dismiss"`). The web Config page shows the same with a one-click button.

## Ownership and proposals

Ownership is per app without a YAML flag: an app is auto-owned while its block hash matches the last generated hash. Any edit converts it to yours. When detection differs for your apps the daemon leaves them untouched and records a pending proposal with the full proposed YAML and a per-app summary. Approve via the Config page banner, `resolve_config_proposal` or CLI; dismiss stores the current signature so it is not re-proposed until files change. Never approve on the user behalf, always surface proposals first.
