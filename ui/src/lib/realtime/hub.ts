export type StreamState = 'connecting' | 'open' | 'reconnecting' | 'closed';

export type Frame = Record<string, unknown>;

export interface HubPort {
  id: string;
  send: (msg: Record<string, unknown>) => void;
  visible: boolean;
  lastSeen: number;
}

interface WireSub {
  id: string;
  key: string;
  topic: string;
  params: Record<string, unknown>;
  cursor: Record<string, unknown>;
  resume: Record<string, number>;
  portId: string;
  tabSub: string;
}

function wireKey(portId: string, tabSub: string): string {
  return `${portId}\u0000${tabSub}`;
}

export class HubCore {
  private token = '';
  private ports = new Map<string, HubPort>();
  private wires = new Map<string, WireSub>();
  private byID = new Map<string, WireSub>();
  private order: string[] = [];
  private seq = 0;
  private state: StreamState = 'closed';
  private controller: AbortController | null = null;
  private reader: ReadableStreamDefaultReader<Uint8Array> | null = null;
  private attempt = 0;
  private live = false;
  private retryMs = 500;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private watchdog: ReturnType<typeof setInterval> | null = null;
  private lastFrame = 0;
  private streamId = '';
  private unauthorized = false;
  private leaderId = '';
  private closed = false;

  start(): void {
    this.closed = false;
    this.connect();
    if (!this.watchdog) {
      this.watchdog = setInterval(() => {
        this.prunePorts();
        if (this.live && this.controller && Date.now() - this.lastFrame > 40000) {
          this.controller.abort();
          void this.reader?.cancel().catch(() => {});
        }
        if (!this.live && !this.retryTimer && !this.unauthorized) this.connect();
      }, 5000);
    }
  }

  stop(): void {
    this.closed = true;
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    if (this.watchdog) clearInterval(this.watchdog);
    this.watchdog = null;
    this.attempt++;
    this.live = false;
    void this.reader?.cancel().catch(() => {});
    this.reader = null;
    this.controller?.abort();
    this.controller = null;
    this.setStreamState('closed');
  }

  setToken(token: string): void {
    if (!token || token === this.token) return;
    const changed = !!this.token;
    this.token = token;
    this.unauthorized = false;
    if (changed) {
      this.attempt++;
      this.live = false;
      void this.reader?.cancel().catch(() => {});
      this.reader = null;
      this.controller?.abort();
      this.controller = null;
    }
    this.retryMs = 500;
    if (!this.live) this.connect();
  }

  addPort(port: HubPort): void {
    this.ports.set(port.id, port);
    port.send({ type: 'state', state: this.state });
    if (port.visible || !this.leaderId) this.setLeader(port.id);
    else port.send({ type: 'leader', leader: false });
  }

  dropPort(id: string): void {
    if (!this.ports.has(id)) return;
    for (const wire of [...this.wires.values()]) {
      if (wire.portId === id) this.removeWire(wire);
    }
    this.ports.delete(id);
    if (this.leaderId === id) {
      this.leaderId = '';
      const next = [...this.ports.values()].filter((p) => p.visible).pop() ?? [...this.ports.values()].pop();
      if (next) this.setLeader(next.id);
      else this.broadcast({ type: 'leader', leader: false });
    }
  }

  subscribe(portId: string, tabSub: string, topic: string, params: Record<string, unknown> = {}): void {
    if (!this.ports.has(portId)) return;
    const key = wireKey(portId, tabSub);
    const prev = this.wires.get(key);
    if (prev) this.removeWire(prev);
    const wire: WireSub = {
      id: `w${++this.seq}`,
      key,
      topic,
      params,
      cursor: {},
      resume: {},
      portId,
      tabSub
    };
    this.wires.set(key, wire);
    this.byID.set(wire.id, wire);
    this.order.push(wire.id);
    if (this.streamId) this.sendSubscribe(wire);
  }

  unsubscribe(portId: string, tabSub: string): void {
    const wire = this.wires.get(wireKey(portId, tabSub));
    if (wire) this.removeWire(wire);
  }

  setVisible(portId: string, visible: boolean): void {
    const port = this.ports.get(portId);
    if (!port) return;
    port.visible = visible;
    port.lastSeen = Date.now();
    if (visible) this.setLeader(portId);
    else if (this.leaderId === portId) {
      const next = [...this.ports.values()].filter((p) => p.visible).pop();
      if (next) this.setLeader(next.id);
    }
  }

  ping(portId: string): void {
    const port = this.ports.get(portId);
    if (!port) return;
    port.lastSeen = Date.now();
    port.send({ type: 'pong' });
  }

  leader(): string {
    return this.leaderId;
  }

  private removeWire(wire: WireSub): void {
    this.wires.delete(wire.key);
    this.byID.delete(wire.id);
    this.order = this.order.filter((id) => id !== wire.id);
    if (this.streamId) this.sendUnary('Unsubscribe', { streamId: this.streamId, sub: wire.id });
  }

  private setLeader(id: string): void {
    if (this.leaderId === id) {
      this.ports.get(id)?.send({ type: 'leader', leader: true });
      return;
    }
    this.leaderId = id;
    for (const p of this.ports.values()) p.send({ type: 'leader', leader: p.id === id });
  }

  private prunePorts(): void {
    const now = Date.now();
    for (const p of [...this.ports.values()]) {
      if (now - p.lastSeen > 10000) this.dropPort(p.id);
    }
  }

  private setStreamState(next: StreamState): void {
    if (this.state === next) return;
    this.state = next;
    this.broadcast({ type: 'state', state: next });
  }

  private broadcast(msg: Record<string, unknown>): void {
    for (const p of this.ports.values()) p.send(msg);
  }

  private headers(): Record<string, string> {
    const h: Record<string, string> = { 'Content-Type': 'application/json' };
    if (this.token) h['Authorization'] = `Bearer ${this.token}`;
    return h;
  }

  private cursorFor(wire: WireSub): Record<string, unknown> {
    if (wire.topic === 'logs' || wire.topic === 'attach') {
      return Object.keys(wire.resume).length > 0 ? { resume: wire.resume } : {};
    }
    return wire.cursor;
  }

  private sendUnary(method: string, body: Record<string, unknown>): void {
    if (!this.token) return;
    void fetch(`/api/jackketch.v1.RealtimeService/${method}`, {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify(body)
    })
      .then((res) => {
        if (res.status === 401) this.unauthorized = true;
      })
      .catch(() => {});
  }

  private sendSubscribe(wire: WireSub): void {
    if (!this.streamId) return;
    this.sendUnary('Subscribe', {
      streamId: this.streamId,
      sub: wire.id,
      topic: wire.topic,
      params: wire.params,
      cursor: this.cursorFor(wire)
    });
  }

  private connect(): void {
    if (this.closed || this.live || !this.token || this.unauthorized) return;
    if (this.retryTimer) {
      clearTimeout(this.retryTimer);
      this.retryTimer = null;
    }
    const id = ++this.attempt;
    const ac = new AbortController();
    this.controller = ac;
    this.streamId = '';
    this.live = true;
    this.setStreamState(this.state === 'closed' ? 'connecting' : 'reconnecting');
    const subs = this.order
      .map((wid) => this.byID.get(wid))
      .filter((w): w is WireSub => !!w)
      .map((w) => ({ sub: w.id, topic: w.topic, params: w.params, cursor: this.cursorFor(w) }));
    void this.run(id, ac, subs, new Set(subs.map((s) => s.sub)));
  }

  private async run(
    id: number,
    ac: AbortController,
    subs: { sub: string; topic: string; params: Record<string, unknown>; cursor: Record<string, unknown> }[],
    included: Set<string>
  ): Promise<void> {
    let retryAfter = 0;
    try {
      const res = await fetch('/api/jackketch.v1.RealtimeService/Connect', {
        method: 'POST',
        headers: this.headers(),
        body: JSON.stringify({ subscriptions: subs }),
        signal: ac.signal
      });
      if (id !== this.attempt) return;
      if (res.status === 401) {
        this.unauthorized = true;
        this.setStreamState('closed');
        this.broadcast({ type: 'unauthorized' });
        return;
      }
      if (!res.ok || !res.body) {
        const ra = res.headers.get('Retry-After');
        if (ra) retryAfter = Math.min(15000, Number(ra) * 1000 || 0);
        throw new Error(`stream ${res.status}`);
      }
      this.setStreamState('open');
      this.lastFrame = Date.now();
      const reader = res.body.getReader();
      this.reader = reader;
      const decoder = new TextDecoder();
      let buf = '';
      for (;;) {
        const { done, value } = await reader.read();
        if (done || id !== this.attempt) break;
        buf += decoder.decode(value, { stream: true });
        const parts = buf.split('\n');
        buf = parts.pop() ?? '';
        for (const line of parts) {
          if (!line.trim()) continue;
          this.lastFrame = Date.now();
          let msg: Record<string, unknown>;
          try {
            msg = JSON.parse(line) as Record<string, unknown>;
          } catch {
            continue;
          }
          if (msg['kind'] === 'hello') {
            this.streamId = String(msg['streamId'] ?? '');
            if (this.streamId) {
              for (const wire of this.wires.values()) {
                if (!included.has(wire.id)) this.sendSubscribe(wire);
              }
              for (const sub of included) {
                if (!this.byID.has(sub)) this.sendUnary('Unsubscribe', { streamId: this.streamId, sub });
              }
            }
            included.clear();
            continue;
          }
          if (msg['kind'] === 'heartbeat') continue;
          this.onFrame(msg);
        }
      }
    } catch {
      if (id !== this.attempt) return;
    } finally {
      if (id === this.attempt) {
        this.live = false;
        this.reader = null;
        this.controller = null;
        this.streamId = '';
      }
    }
    if (this.closed || id !== this.attempt) return;
    this.setStreamState('reconnecting');
    const jitter = Math.random() * this.retryMs;
    const delay = retryAfter || Math.min(this.retryMs / 2 + jitter, 15000);
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      this.connect();
    }, Math.max(500, delay));
    this.retryMs = Math.min(this.retryMs * 2, 15000);
  }

  private onFrame(msg: Record<string, unknown>): void {
    const subId = String(msg['sub'] ?? '');
    const wire = this.byID.get(subId);
    if (!wire) return;
    const kind = String(msg['kind'] ?? '');
    if (kind === 'error') {
      this.ports.get(wire.portId)?.send({ type: 'frame', id: wire.tabSub, frame: msg });
      return;
    }
    if (kind === 'gap') {
      if (wire.topic === 'processes') {
        wire.cursor = {};
        this.sendSubscribe(wire);
        return;
      }
      this.retryMs = 500;
    }
    const cursor = msg['cursor'];
    if (typeof cursor === 'string' || typeof cursor === 'number') {
      if (wire.topic === 'events' || wire.topic === 'audit' || wire.topic === 'config') {
        wire.cursor = { since: cursor };
      } else if (wire.topic === 'logs' || wire.topic === 'attach') {
        this.trackResume(wire, cursor);
      }
    } else if (typeof msg['id'] === 'number' && (wire.topic === 'events' || wire.topic === 'audit')) {
      wire.cursor = { since: msg['id'] };
    }
    const frame: Frame = { ...msg };
    delete frame['sub'];
    this.ports.get(wire.portId)?.send({ type: 'frame', id: wire.tabSub, frame });
  }

  private trackResume(wire: WireSub, cursor: unknown): void {
    if (typeof cursor !== 'string') return;
    const at = cursor.lastIndexOf(':');
    if (at <= 0) return;
    const proc = cursor.slice(0, at);
    const n = Number(cursor.slice(at + 1));
    if (!proc || !Number.isFinite(n)) return;
    wire.resume[proc] = n + 1;
  }
}
