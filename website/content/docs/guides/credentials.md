---
title: "Credentials for agents"
description: "Let an agent call your authenticated API as a real test user without ever seeing a secret."
weight: 60
---

Authenticated endpoints are where agents usually get stuck: they need a token, and you do not want to paste one into a conversation. Credential profiles solve this. You store test credentials in the web UI. The agent refers to a profile by name. The daemon attaches it to the request and scrubs the values from everything it returns.

## Set up a profile

Open `jack-ketch web`, go to **Credentials** and add a profile. A profile holds:

- **Fields**: bearer tokens, basic auth, headers, cookies, query parameters or plain variables such as a tenant id.
- **A login recipe** (optional): a request that fetches and refreshes a token, so an expired JWT renews itself.
- **An environment tag** such as `staging` or `production`, and whether it is the project default.

Bearer, basic and cookie values, and any field whose name looks like a key, token, secret, password, auth or session, are always treated as secret. Non-secret fields such as a tenant id are stored in clear and visible to the agent.

## How the agent uses it

```text
list_credentials()
probe(process_id, url="/api/me")
probe(process_id, url="/api/admin/users", credential="admin")
```

`list_credentials` shows each profile's name, environment, default flag, fields (and values only when not secret) and token status. `probe` attaches the default profile automatically. Results include `credential_status` (`ok`, `expiring`, `expired`, `empty` or `rejected`). If a credential is rejected or expired, the result carries a `credential_hint` telling the agent to ask you to update the profile.

## Credential proxy

For curl, Playwright or e2e suites, `credential_proxy` opens a short-lived local proxy that forwards to your app and attaches the profile to every request.

```text
credential_proxy(action="start", profile="admin", process_id=...)
```

It binds `127.0.0.1` on a random port, expires after 30 minutes by default (at most 8 hours), disappears when the daemon stops and needs your approval to start. Redirects are rewritten to the proxy, `Set-Cookie` stays inside jack-ketch and WebSocket upgrades are refused.

## What protects the values

| Property | How |
|---|---|
| Encrypted at rest | AES-256-GCM, with per-field additional data so values cannot be swapped between fields |
| Key storage | A `0600` key file, or the OS keychain (`secret-tool` on Linux, `security` on macOS). A missing key is an error, never a silent new key |
| Web-only changes | Saving, deleting, revealing and key changes are refused on the agent socket |
| Scrubbing | Values become `[credential:<profile>.<field>]` in probe results, and `***` in logs, in raw, URL-encoded, base64 and JSON-escaped forms |
| Approval | Non-local targets, data-changing methods and `production` profiles need your approval for each request. A saved check never sends credentials to a non-local host |

> [!WARNING]
> This is not a boundary against code running as your user. A process with shell access can read the data directory, the key file and the web token. Use test or staging accounts, and tag any profile that points at real data as `production`.

The full threat model is in the [security reference](../../reference/security/).
