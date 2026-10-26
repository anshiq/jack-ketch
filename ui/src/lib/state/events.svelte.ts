import { EventService } from '../api';
import { realtime } from '../realtime/index.svelte';
import { toEvent } from '../api/normalize';
import type { DaemonEvent } from '../api/types';
import { eventSentence } from '../activity';

type Cb = (e: DaemonEvent) => void;

const CAP = 5000;
const PAGE = 400;

function merge(a: DaemonEvent[], b: DaemonEvent[]): DaemonEvent[] {
  const byId = new Map<number, DaemonEvent>();
  for (const e of a) byId.set(e.id, e);
  for (const e of b) byId.set(e.id, e);
  return [...byId.values()].sort((x, y) => x.id - y.id);
}

class Events {
  items = $state<DaemonEvent[]>([]);
  hasMore = $state(false);
  loadingOlder = $state(false);
  loaded = $state(false);
  private subs = new Map<string, Set<Cb>>();
  private closer: (() => void) | null = null;
  private floor = 0;
  private generation = 0;
  private search = new Map<number, string>();

  onEvent(type: string, cb: Cb): () => void {
    if (!this.subs.has(type)) this.subs.set(type, new Set());
    this.subs.get(type)?.add(cb);
    return () => this.subs.get(type)?.delete(cb);
  }

  emit(e: DaemonEvent) {
    this.subs.get(e.type)?.forEach((cb) => cb(e));
    this.subs.get('*')?.forEach((cb) => cb(e));
  }

  connect(workspaceId: string) {
    this.disconnect();
    const gen = ++this.generation;
    this.loaded = false;
    void this.open(workspaceId, gen);
  }

  private async open(workspaceId: string, gen: number) {
    try {
      const r = await EventService.list(workspaceId, PAGE, { order: 'desc' });
      if (gen !== this.generation) return;
      this.items = merge(this.items, r.events.slice().reverse()).slice(-CAP);
      this.floor = r.events.length > 0 ? Math.min(...r.events.map((e) => e.id)) : r.latestId + 1;
      this.hasMore = r.hasMore;
    } catch {
      this.hasMore = false;
    } finally {
      if (gen === this.generation) this.loaded = true;
    }
    if (gen !== this.generation) return;
    const cursor = this.items.length > 0 ? this.items[this.items.length - 1]!.id : 0;
    const bootCursor = cursor;
    const h = realtime.subscribe('events', { workspaceId, since: cursor }, (msg) => {
      if (msg['kind'] !== 'event') return;
      const e = toEvent(msg);
      if (e.id <= bootCursor) return;
      this.add(e);
      this.emit(e);
    });
    this.closer = () => h.close();
  }

  private add(e: DaemonEvent) {
    const last = this.items[this.items.length - 1];
    if (!last || e.id > last.id) {
      this.items = [...this.items, e].slice(-CAP);
      return;
    }
    this.items = merge(this.items, [e]).slice(-CAP);
  }

  searchText(e: DaemonEvent): string {
    let text = this.search.get(e.id);
    if (text === undefined) {
      text = `${eventSentence(e)} ${e.type} ${e.processId} ${e.instanceId} ${JSON.stringify(e.payload)}`.toLowerCase();
      this.search.set(e.id, text);
      if (this.search.size > CAP * 2) {
        const keep = new Set(this.items.map((x) => x.id));
        for (const id of this.search.keys()) if (!keep.has(id)) this.search.delete(id);
      }
    }
    return text;
  }

  async loadOlder(workspaceId = ''): Promise<number> {
    if (this.loadingOlder || !this.hasMore) return 0;
    this.loadingOlder = true;
    try {
      const r = await EventService.list(workspaceId, PAGE, { before: String(this.floor), order: 'desc' });
      if (r.events.length === 0) {
        this.hasMore = false;
        return 0;
      }
      this.items = merge(r.events.slice().reverse(), this.items);
      this.floor = Math.min(...r.events.map((e) => e.id));
      this.hasMore = r.hasMore;
      return r.events.length;
    } catch {
      return 0;
    } finally {
      this.loadingOlder = false;
    }
  }

  disconnect() {
    this.closer?.();
    this.closer = null;
  }
}

export const events = new Events();
