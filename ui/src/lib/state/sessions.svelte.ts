import { SessionService } from '../api';
import { toSession } from '../api/normalize';
import type { Session } from '../api/types';
import { realtime } from '../realtime/index.svelte';

class Sessions {
  map = $state(new Map<string, Session>());
  loaded = $state(false);
  private closer: (() => void) | null = null;

  get list(): Session[] {
    return [...this.map.values()].sort((a, b) => b.lastSeen - a.lastSeen);
  }

  get active(): Session[] {
    return this.list.filter((s) => !s.closed);
  }

  connect() {
    this.disconnect();
    this.closer = realtime.subscribe('sessions', {}, (msg) => {
      if (msg['kind'] === 'snapshot' && Array.isArray(msg['sessions'])) {
        const next = new Map<string, Session>();
        for (const raw of msg['sessions'] as Record<string, unknown>[]) {
          const s = toSession(raw);
          next.set(s.id, s);
        }
        this.map = next;
        this.loaded = true;
      } else if (msg['kind'] === 'upsert' && msg['session']) {
        const s = toSession(msg['session'] as Record<string, unknown>);
        const next = new Map(this.map);
        next.set(s.id, s);
        this.map = next;
      } else if (msg['kind'] === 'closed' && msg['sessionId']) {
        const id = String(msg['sessionId']);
        const prev = this.map.get(id);
        if (!prev) return;
        const next = new Map(this.map);
        next.set(id, { ...prev, closed: true });
        this.map = next;
      }
    }).close;
  }

  markClosed(id: string) {
    const prev = this.map.get(id);
    if (!prev) return;
    const next = new Map(this.map);
    next.set(id, { ...prev, closed: true });
    this.map = next;
  }

  restore(session: Session) {
    const next = new Map(this.map);
    next.set(session.id, session);
    this.map = next;
  }

  async refresh(): Promise<void> {
    const list = await SessionService.list();
    const next = new Map<string, Session>();
    for (const s of list) next.set(s.id, s);
    this.map = next;
    this.loaded = true;
  }

  disconnect() {
    this.closer?.();
    this.closer = null;
  }
}

export const sessions = new Sessions();
