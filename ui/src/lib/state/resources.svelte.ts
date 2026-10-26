import { ProcessService } from '../api';
import { toResource } from '../api/normalize';
import type { ResourceSample } from '../api/types';
import { realtime } from '../realtime/index.svelte';

class Resources {
  samples = $state(new Map<string, ResourceSample[]>());
  private consumers = new Map<string, { ids: string[]; token: symbol }>();
  private closer: (() => void) | null = null;
  private ids = '';

  watch(ids: string[], consumer = 'default'): () => void {
    const token = Symbol(consumer);
    if (ids.length === 0) this.consumers.delete(consumer);
    else this.consumers.set(consumer, { ids: ids.slice(0, 20), token });
    this.sync();
    return () => {
      if (this.consumers.get(consumer)?.token === token) {
        this.consumers.delete(consumer);
        this.sync();
      }
    };
  }

  private sync() {
    const wanted = [...new Set([...this.consumers.values()].flatMap((c) => c.ids))].slice(0, 20);
    const key = [...wanted].sort().join(',');
    if (key === this.ids) return;
    this.ids = key;
    this.closer?.();
    this.closer = null;
    if (wanted.length === 0) return;
    this.closer = realtime.subscribe('resources', { processIds: wanted, intervalMs: 5000 }, (msg) => {
      if (msg['kind'] !== 'sample') return;
      const id = String(msg['processId'] ?? '');
      if (!id) return;
      const s = toResource(msg);
      const arr = this.samples.get(id) ?? [];
      const m = new Map(this.samples);
      m.set(id, [...arr.slice(-149), s]);
      this.samples = m;
    }).close;
  }

  live(id: string): ResourceSample | null {
    const arr = this.samples.get(id);
    return arr && arr.length > 0 ? (arr[arr.length - 1] as ResourceSample) : null;
  }

  history(id: string): ResourceSample[] {
    return this.samples.get(id) ?? [];
  }
}

export const resources = new Resources();

export async function fetchResource(id: string): Promise<ResourceSample | null> {
  try {
    const r = await ProcessService.getResourceUsage(id);
    return toResource(r as unknown as Record<string, unknown>);
  } catch {
    return null;
  }
}
