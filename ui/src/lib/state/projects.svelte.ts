import { ProjectService } from '../api';

export interface ProjectRow {
  id: string;
  name: string;
  configMode: string;
  lastUsedAt: number;
  workspaceCount: number;
}

export interface WorkspaceRow {
  id: string;
  projectId: string;
  path: string;
  confirmed: boolean;
  lastSeenAt: number;
  missing: boolean;
}

const SNAPSHOT_KEY = 'ar.projects.snapshot';

function readSnapshot(): { projects: ProjectRow[]; workspaces: Record<string, WorkspaceRow[]> } | null {
  try {
    const raw = sessionStorage.getItem(SNAPSHOT_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { projects?: ProjectRow[]; workspaces?: Record<string, WorkspaceRow[]> };
    if (!Array.isArray(parsed.projects) || !parsed.workspaces) return null;
    return { projects: parsed.projects, workspaces: parsed.workspaces };
  } catch {
    return null;
  }
}

function ms(n: unknown): number {
  const v = typeof n === 'number' ? n : Number(n ?? 0);
  if (!v) return 0;
  return v < 1e12 ? v * 1000 : v;
}

class ProjectsStore {
  projects = $state<ProjectRow[]>(readSnapshot()?.projects ?? []);
  workspaces = $state<Record<string, WorkspaceRow[]>>(readSnapshot()?.workspaces ?? {});
  loading = $state(false);
  loaded = $state(false);
  error = $state('');
  private inflight: Promise<void> | null = null;

  private save() {
    try {
      if (this.projects.length === 0) sessionStorage.removeItem(SNAPSHOT_KEY);
      else sessionStorage.setItem(SNAPSHOT_KEY, JSON.stringify({ projects: this.projects, workspaces: this.workspaces }));
    } catch {
    }
  }

  get allWorkspaces(): WorkspaceRow[] {
    return Object.values(this.workspaces).flat();
  }

  async load(force = false): Promise<void> {
    if (this.loaded && !force) return;
    if (this.inflight) return this.inflight;
    this.loading = true;
    this.error = '';
    this.inflight = (async () => {
      try {
        const list = (await ProjectService.list(true)) as Record<string, unknown>[];
        const rows: ProjectRow[] = list.map((p) => ({
          id: String(p['id'] ?? p['projectId'] ?? ''),
          name: String(p['name'] ?? p['id'] ?? ''),
          configMode: String(p['configMode'] ?? ''),
          lastUsedAt: ms(p['lastUsedAt']),
          workspaceCount: Number(p['workspaceCount'] ?? 0)
        })).filter((p) => p.id !== '');
        rows.sort((a, b) => b.lastUsedAt - a.lastUsedAt);
        const entries = await Promise.all(
          rows.map(async (p) => {
            const embedded = list.find((x) => String(x['id'] ?? x['projectId'] ?? '') === p.id)?.['workspaces'];
            const raw = Array.isArray(embedded)
              ? (embedded as Record<string, unknown>[])
              : ((await ProjectService.workspaces(p.id).catch(() => [])) as Record<string, unknown>[]);
            const ws = raw.map<WorkspaceRow>((w) => ({
              id: String(w['id'] ?? w['workspaceId'] ?? ''),
              projectId: p.id,
              path: String(w['path'] ?? ''),
              confirmed: w['confirmed'] !== false,
              lastSeenAt: ms(w['lastSeenAt']),
              missing: w['missing'] === true
            })).filter((w) => w.id !== '');
            return [p.id, ws] as const;
          })
        );
        this.projects = rows;
        this.workspaces = Object.fromEntries(entries);
        this.loaded = true;
        this.save();
      } catch (err) {
        this.error = err instanceof Error ? err.message : String(err);
      } finally {
        this.loading = false;
        this.inflight = null;
      }
    })();
    return this.inflight;
  }

  project(id: string): ProjectRow | undefined {
    return this.projects.find((p) => p.id === id);
  }

  workspace(id: string): WorkspaceRow | undefined {
    return this.allWorkspaces.find((w) => w.id === id);
  }

  snapshot(): { projects: ProjectRow[]; workspaces: Record<string, WorkspaceRow[]> } {
    return { projects: this.projects.map((p) => ({ ...p })), workspaces: Object.fromEntries(Object.entries(this.workspaces).map(([k, v]) => [k, v.map((w) => ({ ...w }))])) };
  }

  restore(snap: { projects: ProjectRow[]; workspaces: Record<string, WorkspaceRow[]> }) {
    this.projects = snap.projects;
    this.workspaces = snap.workspaces;
    this.save();
  }

  rename(id: string, name: string) {
    this.projects = this.projects.map((p) => (p.id === id ? { ...p, name } : p));
    this.save();
  }

  forget(id: string) {
    this.projects = this.projects.filter((p) => p.id !== id);
    const next = { ...this.workspaces };
    delete next[id];
    this.workspaces = next;
    this.save();
  }

  touched(projectId: string, workspaceId: string) {
    this.projects = this.projects.map((p) => (p.id === projectId ? { ...p, lastUsedAt: Date.now() } : p));
    const list = this.workspaces[projectId];
    if (!list) return;
    this.workspaces = {
      ...this.workspaces,
      [projectId]: list.map((w) => (w.id === workspaceId ? { ...w, lastSeenAt: Date.now(), missing: false } : w))
    };
    this.save();
  }

  markMissing(ids: string[]) {
    const set = new Set(ids);
    const next: Record<string, WorkspaceRow[]> = {};
    for (const [pid, list] of Object.entries(this.workspaces)) {
      next[pid] = list.map((w) => (set.has(w.id) ? { ...w, missing: true } : w));
    }
    this.workspaces = next;
    this.save();
  }
}

export const projectsStore = new ProjectsStore();
