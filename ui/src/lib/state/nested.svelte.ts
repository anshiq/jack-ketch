import { ProjectService } from '../api';
import type { NestedProject } from '../api';

const KEY = 'jk.nested.dismissed';

function readDismissed(): string[] {
  try {
    const raw = localStorage.getItem(KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === 'string') : [];
  } catch {
    return [];
  }
}

export function nestedKey(n: NestedProject): string {
  return `${n.child.projectId}>${n.parent.projectId}`;
}

export function overlapCount(n: NestedProject): number {
  return n.apps.filter((a) => a.status === 'duplicate' || a.status === 'conflict').length;
}

class NestedStore {
  list = $state<NestedProject[]>([]);
  loaded = $state(false);
  dismissed = $state<string[]>(readDismissed());
  private inflight: Promise<void> | null = null;

  async load(force = false): Promise<void> {
    if (this.loaded && !force) return;
    if (this.inflight) return this.inflight;
    this.inflight = (async () => {
      try {
        this.list = await ProjectService.nested();
        this.loaded = true;
      } catch {
        this.list = [];
      } finally {
        this.inflight = null;
      }
    })();
    return this.inflight;
  }

  visible(projectId = ''): NestedProject[] {
    return this.list.filter((n) => !this.dismissed.includes(nestedKey(n)) && (!projectId || n.child.projectId === projectId || n.parent.projectId === projectId));
  }

  dismiss(n: NestedProject) {
    this.dismissed = [...this.dismissed, nestedKey(n)];
    try {
      localStorage.setItem(KEY, JSON.stringify(this.dismissed));
    } catch {
      return;
    }
  }
}

export const nestedStore = new NestedStore();
