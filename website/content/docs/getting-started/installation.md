---
title: "Installation"
description: "Install the two binaries on Linux, macOS or Windows, or from Nix or source."
weight: 10
---

jack-ketch installs two binaries: `jack-ketch` (the CLI and MCP bridge) and `jackketchd` (the daemon). They must sit in the same directory.

{{< tabs >}}
{{< tab name="Linux and macOS" >}}
```sh
curl -fsSL https://raw.githubusercontent.com/anshiq/jack-ketch/master/scripts/install.sh | bash
```
Installs to `~/.local/bin`. Make sure that directory is on your `PATH`.
{{< /tab >}}
{{< tab name="Windows" >}}
```powershell
irm https://raw.githubusercontent.com/anshiq/jack-ketch/master/scripts/install.ps1 | iex
```
Installs to `%LOCALAPPDATA%\jack-ketch\bin`.
{{< /tab >}}
{{< tab name="Nix" >}}
A flake lives under `packaging/nix/` in the repository.
{{< /tab >}}
{{< tab name="From source" >}}
```sh
git clone https://github.com/anshiq/jack-ketch
cd jack-ketch
go build ./cmd/jack-ketch
go build ./cmd/jackketchd
```
Requires Go. See `go.mod` for the minimum version.
{{< /tab >}}
{{< /tabs >}}

The installers resolve the latest release, verify its SHA-256 checksum and place the binaries for you. Upgrading from a release that shipped `jack-ketch-shim` removes that binary.

## Updating

Re-run the installer. If the daemon is running, it keeps running the previous version: finishing the update means restarting it, and that stops every managed process. The installer warns you, the web UI shows a banner with the processes a restart would stop, and agents see `update_notice`. Run `jack-ketch daemon restart` when you are ready; it lists the processes and asks before stopping them.

## Pin a version or directory

| Option | Flag | Environment variable |
|---|---|---|
| Release version | `-v`, `--version` | `VERSION` |
| Install directory | `-d`, `--dir` | `INSTALL_DIR` |

```sh
curl -fsSL https://raw.githubusercontent.com/anshiq/jack-ketch/master/scripts/install.sh | bash -s -- --version <version> --dir /usr/local/bin
```

## Verify

```sh
jack-ketch version
jack-ketch doctor
```

`doctor` checks the daemon, socket and file permissions, and every agent integration. It never fails because an agent is simply not connected. See [Troubleshooting](../../reference/troubleshooting/) for each check.

## Start the daemon automatically

The daemon starts on demand the first time an agent calls a tool or you run `jack-ketch web`. To start it at login instead:

```sh
jack-ketch daemon install
```

This installs a systemd user socket on Linux, a LaunchAgent on macOS or a logon task on Windows. `jack-ketch daemon uninstall` removes it.

> [!NOTE]
> Never run the daemon as root. cgroup delegation comes from the user systemd instance, not from privilege.
