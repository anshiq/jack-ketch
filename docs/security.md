# jack-ketch v3 security model

Local, single-user threat model. The daemon enforces; agents are untrusted
callers of the API.

| Threat | Mitigation |
|---|---|
| Another local user talks to the daemon | Socket dir `0700`, socket `0600`, **peer-credential uid check enforced on Linux and macOS** (fail closed; other platforms rely on the socket modes) |
| Malicious web page → TCP listener (CSRF, DNS rebinding) | TCP off by default; bearer token compared in constant time; strict `Host` (`127.0.0.1`/`localhost`) + `Origin` checks; no permissive CORS; token never in cookies |
| Secrets in logs/env in the web UI | Redaction on by default; reveal is policy-gated and audited |
| Agent escapes the project | `restricted` mode per project; workdir confinement against the workspace path |
| Client/harness environment leaking into supervised processes | The daemon sanitises the spawn environment, dropping `CLAUDECODE`, `CLAUDE_CODE_*`, `CODEX_*`, `GEMINI_CLI`, `CURSOR_*`, `VSCODE_*`, `MCP_*` and similar client variables |
| Tampered shim bundle | Bundle dir `0700`; shim validates spec owner uid |
| DB/log files readable by others | Data dir `0700`, files `0600`; `doctor` checks and fixes |
| Agent reads test credentials | Secrets live in the daemon, never in config, env files or MCP results; see Credential profiles |

## Audit

Every mutating RPC plus every policy denial lands in the `audit` table with
session + harness attribution, including the redacted `args` (90-day
retention). `AuditService.ListAudit` and MCP `get_audit_log` read it.

## File modes

`doctor --fix` repairs data-dir/socket modes. Never run the daemon as
root: cgroup delegation comes from the user systemd instance
(`Delegate=yes`), not privilege.

## Credential profiles

Profiles (JWTs, cookies, API keys, tenant ids, login recipes) are entered in
the web UI and kept per project in `state.db`. The agent refers to a profile
by name; the daemon attaches it to `probe`, saved checks and credential
proxies and scrubs the values from everything it returns.

What the design guarantees:

- **Encrypted at rest.** Secret values and captured cookies are sealed with
  AES-256-GCM (per-field additional data, so values cannot be swapped between
  fields). Non-secret fields such as a tenant id are stored in clear and are
  visible to the agent. Bearer, basic and cookie values, and any field whose name
  or target looks like a key, token, secret, password, auth or session, are
  always treated as secret.
- **Key storage.** The 32-byte key lives in `credentials.key` (mode `0600`) or
  in the OS keychain (`secret-tool` on Linux, `security` on macOS). `auto`
  prefers the keychain and falls back to the file; switching moves the key
  without re-encrypting. A missing or unreachable key is an error, never a
  silent new key; `ResetKey` creates a new one and clears the secrets.
  `JACK_KETCH_NO_KEYCHAIN=1` forces the file backend. On macOS the `security`
  tool receives the key as an argument for a moment while it is stored.
- **Web-only changes.** Saving, deleting, revealing, clearing cookies and
  changing key storage are refused on the agent socket and accepted only on the
  authenticated web listener. Reveal is audited without the value and denied
  when restricted mode sets `reveal_secrets: false`.
- **Scrubbing.** Secret values are replaced with `[credential:<profile>.<field>]`
  (raw, URL-encoded, base64 and JSON-escaped forms) in probe bodies, headers,
  errors, URLs, saved-check fields and captured log lines. `get_logs`,
  `search_logs`, tails, exports, `wait_for_log` and diagnosis output mask the
  same values with `***`. Values shorter than 4 characters are not masked.
- **Approval.** Non-local targets, data-changing methods and profiles tagged
  `production` need the user's approval for each request. A saved check never
  sends credentials to a non-local host.
- **Proxies.** A credential proxy binds `127.0.0.1` on a random port, checks the
  `Host` header, refuses `Sec-Fetch-Site: cross-site`, expires (default 30
  minutes, at most 8 hours), disappears when the daemon stops and needs the
  user's approval to start. `Set-Cookie` stays inside jack-ketch, redirects are
  rewritten to the proxy, text responses up to 16 MiB are scrubbed, WebSocket
  upgrades are refused.

What it does not guarantee: this is not a boundary against code running as
your user. A process with shell access can read the data directory, the key
file and the web token. Use test or staging accounts, and give profiles that
point at real data a `production` environment tag.
