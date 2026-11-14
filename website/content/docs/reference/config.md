---
title: "Config reference"
description: "Fields of the v3 project config: runtime, apps, modes, env sources, checks, logging and alerts."
weight: 30
---

The machine-readable [JSON Schema](/schema.json) is served with this site. Use it from editors that support `yaml-language-server`.

```yaml
# yaml-language-server: $schema=https://jack-ketch.anshik.in/schema.json
version: 3
```

## `runtime`

| Field | Default | Meaning |
|---|---|---|
| `shell_env` | `login` | `login` captures your login-shell env once so nvm, pyenv and uv shims reach processes. `none` inherits the parent env |
| `env` | none | List of `KEY=VALUE` applied to every app, at lower precedence than app env |
| `stop_grace` | `5s` | Grace period between SIGTERM and SIGKILL |
| `log_buffer_lines` | `10000` | Ring-buffer lines kept per process |
| `max_exited_processes` | `50` | How many exited processes stay listed |
| `exited_ttl` | `5m` | How long exited processes stay listed |
| `security.mode` | `trusted` | `restricted` confines commands and working directories |

## `apps.<name>`

| Field | Meaning |
|---|---|
| `type` | Profile: `node`, `nextjs`, `python`, `django`, `spring-boot`, `go`, `generic`, or another label such as `shell` |
| `workdir` | Relative to the workspace root, resolved through symlinks. Must exist |
| `command` | Command and arguments as a list |
| `env_file`, `env` | A dotenv file and `KEY=VALUE` entries for this app |
| `env_sources` | Ordered env layers, later wins. See below |
| `readiness` | Regex patterns that mark the app ready when a log line matches |
| `health_check` | `http` or `tcp`, `interval`, `timeout`, `failure_threshold` |
| `restart` | `policy` (`never`, `on-failure`, `always`), `max_restarts`, `backoff` |
| `checks` | Saved runtime checks. See below |
| `default_mode`, `modes` | Named ways to run the app. A mode overrides only what differs |
| `depends_on` | Apps that must be running first. `start_stack` starts them in this order |
| `autostart` | Start the app when the daemon loads the project |
| `reload` | What happens to running processes when this app's config changes: `manual` (default) marks them stale until you restart; `restart` restarts them automatically |
| `watch` | Restart the app's processes when files change. See below |

## `watch`

```yaml
apps:
  api:
    command: [go, run, ./cmd/api]
    watch: [cmd, internal, go.mod]
  web:
    command: [node, server.js]
    watch:
      paths: [src]
      ignore: ["*.test.js", "src/generated/**"]
      debounce: 500ms
```

`watch` takes a path, a list of paths, or a mapping. Paths are relative to the app's `workdir`; directories are watched recursively, including directories created later. When a watched file changes, the app's running processes and its crashed or exited ones are restarted once after `debounce` (default `300ms`, between `50ms` and `1m`) of quiet; processes you stopped stay stopped. `.git`, `node_modules`, `vendor`, `dist`, `build`, `target`, `bin`, `obj`, `__pycache__`, `.venv`, framework caches and editor temp files (`*.swp`, `*~`, `*.log`, ...) are always ignored. `ignore` patterns match the file name, or the path relative to the workdir when they contain a `/`. Each automatic restart is recorded as a `process.auto_restarted` event and in the audit log. Leave `watch` off for dev servers that already hot-reload.

## `env_sources`

```yaml
env_sources:
  - file: .env
  - file: .env.local
    optional: true
  - vars: {PORT: 3000}
```

| Entry key | Meaning |
|---|---|
| `file` | Dotenv file, relative to the workspace root |
| `optional` | Skip silently when the file is missing |
| `vars` | Inline variables as a map or a list of `KEY=VALUE`. Supports `${port:name}` |

## `checks`

| Field | Meaning |
|---|---|
| `name` | Required. The check's name |
| `method` | HTTP method, default `GET` |
| `url` | A path resolved against the process port, or a full URL. `${VAR}` expands from the process env |
| `tcp` | `host:port` to connect to instead |
| `headers`, `body` | Request headers and body |
| `credential` | A credential profile name. `none` sends anonymously |
| `expect_status`, `expect_contains` | Assertions. The default status expectation is any 2xx or 3xx |
| `timeout` | For example `5s` |

## `logging` and `alerts`

```yaml
logging:
  redact:
    - 'Bearer [A-Za-z0-9._-]+'
alerts:
  - pattern: "OutOfMemory"
    message: "api ran out of memory"
  - pattern: 'ERROR .* db'
    regex: true
```

`logging.redact` patterns are replaced with `***` in `get_logs`, search and tail output. An alert fires an event and a toast when its pattern appears in any process's logs. `message` defaults to `pattern matched: <pattern>`.
