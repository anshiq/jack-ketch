---
title: "Runtime verification"
description: "Use probe and saved checks to prove a change works against the running app, with the logs each request produced."
weight: 50
---

Passing tests is not the same as the running app working. `probe` exercises a live process and returns the response together with the exact log lines the process printed while handling it.

## Probe

```text
probe(process_id, url="/api/orders", method="POST",
      body='{"sku":"A1","qty":2}', expect_status=201, expect_contains='"id"')
```

| Parameter | Meaning |
|---|---|
| `url` | A path like `/api/users`, resolved against the port the process listens on, or `PORT` from its env, or a full http(s) URL. `${VAR}` expands from the process env |
| `method`, `headers`, `body` | The request. JSON bodies get `Content-Type: application/json` automatically |
| `tcp` | `host:port` to connect to instead of an HTTP request |
| `expect_status`, `expect_contains` | Assertions. By default 4xx and 5xx count as a failure |
| `timeout_ms`, `settle_ms` | Request timeout (default 10000) and how long to keep collecting logs after the response (default 300) |
| `credential` | A [credential profile](../credentials/). Omit for the project default, `none` for anonymous |

`GET`, `HEAD` and `OPTIONS` to localhost run immediately. Anything that changes data or targets a non-local host needs your approval first (`confirmed=true`).

## Saved checks

A probe worth repeating becomes a check on the app or mode. `run_checks` re-runs them in one call and reports pass or fail with the logs each request produced.

```yaml
checks:
  - name: create-order
    method: POST
    url: /api/orders
    headers: {Authorization: "Bearer ${API_TOKEN}"}
    body: '{"sku": "A1", "qty": 2}'
    expect_status: 201
    expect_contains: '"id"'
    timeout: 5s
  - name: db-port
    tcp: localhost:5432
```

Ask your agent to offer this after a fix: it can call `save_run_config(process_id, app, mode, checks=[...])`, and from then on `run_checks(app="api", mode="dev")` is the regression test for the running behavior.

## A good verification flow

1. Reuse the running process or `restart_process`, then `wait_for_log(ready=true)`.
2. Exercise two to five concrete interactions, including an edge case.
3. Compare expected and actual, with the key log lines from each probe.
4. On a failure, call `diagnose_process`.

## Health checks and restarts

Separate from probes, an app can declare a continuous health check and a restart policy so jack-ketch notices trouble without polling.

```yaml
apps:
  api:
    readiness:
      - 'listening on :8080'
    health_check:
      http: "http://localhost:8080/healthz"
      interval: 10s
      timeout: 3s
      failure_threshold: 3
    restart:
      policy: on-failure
      max_restarts: 5
```

Health probing starts once the process is ready. After `failure_threshold` consecutive failures it is reported `unhealthy` and, if the policy permits, restarted. A manual `stop_process` never triggers a restart.
