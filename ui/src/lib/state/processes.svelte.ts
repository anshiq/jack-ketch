import { ProcessService } from '../api';
import { realtime } from '../realtime/index.svelte';
import { toProcess } from '../api/normalize';
import type { Process } from '../api/types';
import { scopeState } from './scope.svelte';

const SNAPSHOT_KEY = 'ar.processes.snapshot';

function readSnapshot(): Map<string, Process> {
  try {
    const raw = sessionStorage.getItem(SNAPSHOT_KEY);
    if (!raw) return new Map();
    const rows = JSON.parse(raw) as Record<string, unknown>[];
    if (!Array.isArray(rows)) return new Map();
    const map = new Map<string, Process>();
    for (const row of rows) {
      const p = toProcess(row);
      map.set(p.id, p);
    }
    return map;
  } catch {
    return new Map();
  }
}

function writeSnapshot(map: Map<string, Process>) {
  try {
    const rows = [...map.values()];
    if (rows.length === 0) sessionStorage.removeItem(SNAPSHOT_KEY);
    else sessionStorage.setItem(SNAPSHOT_KEY, JSON.stringify(rows));
  } catch {
  }
}

function tally(list: Process[]): { running: number; failed: number; exited: number; starting: number } {
  let running = 0;
  let failed = 0;
  let exited = 0;
  let starting = 0;
  for (const p of list) {
    if (p.status === 'running' || p.status === 'ready') running++;
    else if (p.status === 'failed' || p.status === 'crashed') failed++;
    else if (p.status === 'starting') starting++;
    else exited++;
  }
  return { running, failed, exited, starting };
}

function rank(p: Process): number {
  if (p.status === 'running' || p.status === 'ready' || p.status === 'starting') return 0;
  if (p.status === 'failed' || p.status === 'crashed') return 1;
  return 2;
}

class Processes {
  synced = $state(false);
  map = $state(readSnapshot());
  private closer: (() => void) | null = null;
  private fallback: ReturnType<typeof setTimeout> | null = null;

  get streamState() {
    return realtime.state;
  }

  get list(): Process[] {
    return [...this.map.values()].sort((a, b) => {
      const ar = rank(a);
      const br = rank(b);
      if (ar !== br) return ar - br;
      if (ar > 0) {
        const at = a.exitedAt ?? a.startedAt ?? 0;
        const bt = b.exitedAt ?? b.startedAt ?? 0;
        if (at !== bt) return bt - at;
      }
      const an = (a.app || a.command).toLowerCase();
      const bn = (b.app || b.command).toLowerCase();
      if (an !== bn) return an.localeCompare(bn);
      return (a.startedAt ?? 0) - (b.startedAt ?? 0);
    });
  }

  get(id: string): Process | undefined {
    return this.map.get(id);
  }

  patch(id: string, patch: Partial<Process>) {
    const prev = this.map.get(id);
    if (!prev) return;
    const next = new Map(this.map);
    next.set(id, { ...prev, ...patch });
    this.map = next;
    writeSnapshot(next);
  }

  insert(p: Process) {
    const next = new Map(this.map);
    next.set(p.id, p);
    this.map = next;
    writeSnapshot(next);
  }

  removeLocal(id: string) {
    const prev = this.map.get(id);
    if (!prev) return;
    const next = new Map(this.map);
    next.delete(id);
    this.map = next;
    writeSnapshot(next);
  }

  get scoped(): Process[] {
    if (scopeState.all) return this.list;
    const ws = scopeState.workspaceId;
    return this.list.filter((p) => p.workspaceId === ws);
  }

  get counts(): { running: number; failed: number; exited: number; starting: number } {
    return tally(this.scoped);
  }

  get totals(): { running: number; failed: number; exited: number; starting: number } {
    return tally(this.list);
  }

  byWorkspace(ws: string): Process[] {
    return this.list.filter((p) => p.workspaceId === ws);
  }

  filtered(filter: string, status: string): Process[] {
    const q = filter.trim().toLowerCase();
    return this.list.filter((p) => {
      if (status !== 'all' && status !== '' && p.status !== status) return false;
      if (!q) return true;
      return p.command.toLowerCase().includes(q) || p.id.toLowerCase().includes(q) || String(p.pid).includes(q) || p.ports.some((x) => String(x).includes(q));
    });
  }

  connect(workspaceId = '', all = true) {
    this.disconnect();
    let snapshotSeen = false;
    this.fallback = setTimeout(() => {
      this.fallback = null;
      if (!snapshotSeen) void this.handleSnapshot(workspaceId, all);
    }, 3000);
    this.closer = realtime.subscribe('processes', { workspaceId, allWorkspaces: all }, (msg) => {
      if (msg['kind'] === 'snapshot' && Array.isArray(msg['snapshot'])) {
        snapshotSeen = true;
        const next = new Map<string, Process>();
        for (const p of msg['snapshot'] as Record<string, unknown>[]) {
          const n = toProcess(p);
          next.set(n.id, n);
        }
        this.map = next;
        this.synced = true;
        writeSnapshot(next);
        if (this.fallback) clearTimeout(this.fallback);
        this.fallback = null;
      } else if (msg['kind'] === 'upsert' && msg['process']) {
        const n = toProcess(msg['process'] as Record<string, unknown>);
        const next = new Map(this.map);
        next.set(n.id, n);
        this.map = next;
        writeSnapshot(next);
      } else if (msg['kind'] === 'removed' && msg['processId']) {
        const next = new Map(this.map);
        next.delete(String(msg['processId']));
        this.map = next;
        writeSnapshot(next);
      } else if (msg['kind'] === 'gap') {
        void this.handleSnapshot(workspaceId, all);
      }
    }).close;
  }

  async handleSnapshot(workspaceId: string, all: boolean) {
    try {
      const arr = await ProcessService.list(workspaceId, all);
      const next = new Map<string, Process>();
      for (const p of arr) {
        const prev = this.map.get(p.id);
        const sparse = p.args.length === 0 && p.startedAt === null;
        next.set(p.id, prev && sparse ? { ...prev, status: p.status, pid: p.pid || prev.pid, exitCode: p.exitCode ?? prev.exitCode, workspaceId: p.workspaceId || prev.workspaceId, projectId: p.projectId || prev.projectId } : p);
      }
      this.map = next;
      this.synced = true;
      writeSnapshot(next);
    } catch {
    }
  }

  disconnect() {
    this.closer?.();
    this.closer = null;
    if (this.fallback) clearTimeout(this.fallback);
    this.fallback = null;
  }
}

export const processes = new Processes();
