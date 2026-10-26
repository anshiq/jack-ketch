import { ProjectService } from '../api';
import { projectsStore, type ProjectRow, type WorkspaceRow } from './projects.svelte';
import { router } from '../router.svelte';

export type ScopeProject = Pick<ProjectRow, 'id' | 'name'>;
export type ScopeWorkspace = Pick<WorkspaceRow, 'id' | 'projectId' | 'path' | 'missing'>;

const LEGACY_KEY = 'ar.scope.v2';
const LAST_PATH_KEY = 'ar.lastWorkspacePath';

function baseName(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

class ScopeState {
  workspaceId = $state('');
  resolving = $state(false);
  error = $state('');

  get projects(): ScopeProject[] {
    return projectsStore.projects;
  }

  get workspaces(): ScopeWorkspace[] {
    return projectsStore.allWorkspaces;
  }

  get loaded(): boolean {
    return projectsStore.loaded;
  }

  get all(): boolean {
    return this.workspaceId === '';
  }

  get workspace(): ScopeWorkspace | null {
    return this.workspaces.find((w) => w.id === this.workspaceId) ?? null;
  }

  get projectId(): string {
    return this.workspace?.projectId ?? '';
  }

  get projectName(): string {
    const p = this.projects.find((x) => x.id === this.projectId);
    return p?.name ?? '';
  }

  get path(): string {
    return this.workspace?.path ?? '';
  }

  get label(): string {
    if (this.all) return 'All workspaces';
    const ws = this.workspace;
    if (!ws) return 'All workspaces';
    return this.projectName || baseName(ws.path);
  }

  workspaceLabel(id: string): string {
    const ws = this.workspaces.find((w) => w.id === id);
    if (!ws) return id.length > 14 ? `${id.slice(0, 7)}…${id.slice(-4)}` : id;
    const p = this.projects.find((x) => x.id === ws.projectId);
    return p?.name || baseName(ws.path);
  }

  workspacePath(id: string): string {
    return this.workspaces.find((w) => w.id === id)?.path ?? '';
  }

  private valid(id: string): boolean {
    return id === '' || this.workspaces.some((w) => w.id === id);
  }

  private fromUrl(): string | null {
    if (typeof window === 'undefined') return null;
    return new URLSearchParams(window.location.search).get('ws');
  }

  private syncUrl(replace: boolean) {
    if (typeof window === 'undefined') return;
    const url = new URL(window.location.href);
    if (url.searchParams.get('ws') === this.workspaceId) return;
    url.searchParams.set('ws', this.workspaceId);
    void router.navigate(url.pathname + url.search + url.hash, { replace, preserveScroll: true });
  }

  syncFromUrl() {
    if (!this.loaded) return;
    const raw = this.fromUrl();
    if (raw === null) {
      this.syncUrl(true);
      return;
    }
    const next = this.valid(raw) ? raw : '';
    if (next === this.workspaceId) return;
    this.workspaceId = next;
  }

  private applyUrl() {
    const raw = this.fromUrl();
    if (raw !== null) this.workspaceId = this.valid(raw) ? raw : '';
    else this.workspaceId = this.workspaces.length === 1 ? (this.workspaces[0]?.id ?? '') : '';
    try {
      localStorage.removeItem(LEGACY_KEY);
    } catch {
    }
    this.syncUrl(true);
  }

  async refresh(force = false) {
    await projectsStore.load(force);
    if (!this.loaded) return;
    const first = !this.urlApplied;
    if (this.workspaceId && !this.workspaces.some((w) => w.id === this.workspaceId)) this.workspaceId = '';
    if (first) {
      this.urlApplied = true;
      this.applyUrl();
    }
  }

  select(workspaceId: string, opts: { replace?: boolean } = {}) {
    this.workspaceId = workspaceId;
    this.syncUrl(opts.replace === true);
  }

  private urlApplied = false;

  async open(path: string): Promise<boolean> {
    const trimmed = path.trim();
    if (!trimmed) return false;
    this.resolving = true;
    this.error = '';
    try {
      const res = await ProjectService.resolve(trimmed);
      await this.refresh(true);
      projectsStore.touched(res.projectId, res.workspaceId);
      this.select(res.workspaceId);
      try {
        localStorage.setItem(LAST_PATH_KEY, trimmed);
      } catch {
        return true;
      }
      return true;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      return false;
    } finally {
      this.resolving = false;
    }
  }
}

export const scopeState = new ScopeState();
