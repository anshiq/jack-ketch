---
title: "Project config"
description: "How apps are detected and stored, the config sections, profiles and how to edit config safely."
weight: 70
---

A project's config declares how its apps start, how they are supervised and which env they get. It lives in the daemon database, not in your repository.

## Where it lives and how to edit it

| Method | Use it to |
|---|---|
| Web UI **Config** page | Edit with line and column validation, see revisions and roll back |
| `jack-ketch config edit` | Edit in `$EDITOR` with validation |
| `jack-ketch config export` | Print the current YAML |
| `jack-ketch config show` | Show resolved apps with provenance |
| MCP `validate_config`, `plan_config`, `apply_config` | Let an agent propose a change: validate, diff, then apply |

An invalid save keeps the last good revision active. `apply_config` takes a `base_revision` for optimistic concurrency and answers `stale_revision` on a conflict.

A [JSON Schema](/schema.json) is available for editors with YAML language servers.

## Auto-detect and self-sync

On first sight of a project, jack-ketch scans run-defining files and generates apps for you. It keeps them in sync when those files change. If an app you own needs an update, you get a pending proposal that you approve or dismiss, and an agent never auto-approves one.

## Top-level sections

```yaml
version: 3
project:
  name: my-project
runtime:
  shell_env: login
  stop_grace: 5s
  log_buffer_lines: 10000
  max_exited_processes: 50
  exited_ttl: 5m
  security:
    mode: trusted
apps: {}
logging:
  redact:
    - 'sk-[A-Za-z0-9]{20,}'
alerts:
  - pattern: "OutOfMemory"
    message: "The api ran out of memory"
```

| Section | Purpose |
|---|---|
| `runtime` | Daemon defaults: login-shell env capture, shared `env`, stop grace, ring-buffer size, how many exited processes stay listed and for how long, and the security mode |
| `apps` | Named apps with `command`, `workdir`, `type`, env, `readiness`, `health_check`, `restart`, `checks` and `modes` |
| `logging.redact` | Regex patterns replaced with `***` in `get_logs`, search and tail output |
| `alerts` | Notify, with an event and a toast, when a pattern appears in any process's logs |

`security.mode: restricted` confines commands and working directories to the workspace and respects `allowed_commands`. See the [config reference](../../reference/config/) for every field.

## Built-in profiles

An app's `type` selects a profile that supplies readiness patterns and, for some, a default command.

| Profile | Detected by | Default command |
|---|---|---|
| `nextjs` | `package.json` with `next` | Package manager `run dev` (npm, pnpm, yarn or bun by lockfile) |
| `spring-boot` | `pom.xml` or Gradle files | `./mvnw spring-boot:run` or `./gradlew bootRun` |
| `django` | `manage.py` | `python manage.py runserver` |
| `node` | `package.json` | Package manager `run dev` |
| `python` | `requirements.txt` or `pyproject.toml` | none |
| `go` | `go.mod` | `go run .` |
| `generic` | fallback | none |

## Git hygiene

jack-ketch lists the files it uses that git would track: its `.jack-ketch/` directory, env files that saved apps load, and project-scope integration files. It offers to add them to `.gitignore` through `update_gitignore`, and remembers a no.
