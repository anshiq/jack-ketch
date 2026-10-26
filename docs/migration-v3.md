# Migrating to jack-ketch v3

v3 moves state out of your repos into `~/.local/share/jack-ketch`
(`state.db`, project configs, per-workspace log indexes). Migration is
automatic on first contact, explicit with `migrate`, and never deletes
anything without asking.

## Automatic (first contact)

1. The daemon resolves your directory to a project + workspace (stable
   across moves, re-clones and worktrees).
2. `<root>/.jack-ketch/` is absorbed: a running v2 daemon hands over
   (adopted processes show "legacy" until restarted for full log
   capture), `logs.db` entries land in the workspace FTS index,
   `audit.log` lands in the audit table. The directory is left in place.

## Explicit

```sh
jack-ketch migrate [--dry-run] [--all-known] [--cleanup] [<dir>...]
```

- `--all-known` scans harness configs (`~/.claude.json`,
  `~/.codex/config.toml`, opencode) for projects to import.
- `--cleanup` offers to remove legacy `.jack-ketch/` dirs afterwards.
- Existing MCP entries (`jack-ketch serve`) keep working unchanged —
  the same command now runs as a bridge. `integrate` can collapse
  project-scope entries into one global entry.

## Rollback / compatibility

- v3.0 keeps `serve --embedded` (old in-process mode); v3.2 removes it.
- `runtime.daemon` is ignored with a deprecation notice (the daemon is
  always used).
- `project forget` soft-deletes (30-day trash); `project gc`
  lists vanished workspaces.
