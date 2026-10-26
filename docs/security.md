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

## Audit

Every mutating RPC plus every policy denial lands in the `audit` table with
session + harness attribution, including the redacted `args` (90-day
retention). `AuditService.ListAudit` and MCP `get_audit_log` read it.

## File modes

`doctor --fix` repairs data-dir/socket modes. Never run the daemon as
root: cgroup delegation comes from the user systemd instance
(`Delegate=yes`), not privilege.
