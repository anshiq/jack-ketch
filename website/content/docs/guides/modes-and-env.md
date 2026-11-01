---
title: "Modes and environment"
description: "Run one app several ways with modes, and understand exactly which env value wins."
weight: 30
---

## Modes

An app's shared settings (workdir, type, env, checks) live on the app. Each way of running it is a mode that overrides only what differs.

```yaml
version: 3
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

`start_process(app="api", mode="prod")` picks a mode. Without `mode`, jack-ketch uses `default_mode`, or the only mode, or the app's own command. Mode names use letters, digits, `.`, `_` and `-`.

A mode can set `description`, `command`, `workdir`, `type`, `env_sources`, `env_file`, `env`, `readiness`, `health_check`, `restart` and `checks`. Its env sources are layered after the app's. Its checks replace app checks of the same name.

> [!TIP]
> Don't hand-write a mode for a run that already worked. Let the agent call `save_run_config` after you agree to the `save_suggestion`: it records the exact command, workdir and env.

## Ordered environment

`env_sources` is an ordered list. **Later entries win.** Each entry is a dotenv file or inline variables.

```yaml
env_sources:
  - file: .env
  - file: .env.local
    optional: true
  - vars:
      PORT: 3000
      DATABASE_URL: postgres://localhost:5432/app
  - file: .env.override
    optional: true
```

A missing file that is not `optional` fails the start. Paths resolve relative to the workspace root. `${port:name}` templates work in any value.

## Precedence, lowest to highest

1. **Base env**: the captured login-shell environment (`runtime.shell_env: login`, the default), or the parent environment (`none`). The login shell is what lets nvm, pyenv and uv shims reach managed processes.
2. **`runtime.env`**: a layer applied to every app.
3. **App sources**: `env_file`, then `env`, then the app's `env_sources` in order.
4. **Mode sources**: the same three, for the selected mode.
5. **Request `env_file`**, then request `env`, passed to `start_process`.

If `PORT` is set in every layer, `start_process(app="web", env=["PORT=3003"])` gives the process `PORT=3003`.

`get_process_env(process_id)` shows the merged result with the source of each key, such as `file:.env`, `vars`, `mode:prod/file:.env.production` or `request.env`. Secret-like values are masked unless explicitly revealed, and revealing is policy-gated and audited.

## A monorepo

One config, several independent services, each with its own workdir and `.env`: the same key with different values, with no leakage.

```yaml
runtime:
  env: ["NODE_ENV=development"]
apps:
  api:
    type: node
    workdir: ./services/api
    env_sources:
      - file: ./services/api/.env
    command: ["node", "server.js"]
  worker:
    type: python
    workdir: ./services/worker
    env_sources:
      - file: ./services/worker/.env
    command: ["python3", "-u", "worker.py"]
```

A missing `workdir` fails the start before anything is executed or registered, so a typo fails loudly and fast.
