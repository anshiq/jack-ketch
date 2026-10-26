import { getAuthToken } from '../api/auth';
import { notifyUnauthorized } from '../api/transport';
import { HubCore, type Frame, type HubPort, type StreamState } from './hub';

type FrameHandler = (frame: Frame) => void;

export interface RealtimeSubscription {
  close: () => void;
  update: (params: Record<string, unknown>) => void;
}

interface LocalSub {
  topic: string;
  params: Record<string, unknown>;
  onFrame: FrameHandler;
}

const FALLBACK_MS = 2000;
const PING_MS = 5000;

export class RealtimeClient {
  state = $state<StreamState>('closed');
  leader = $state(true);
  unauthorized = $state(false);
  available = $state(false);

  private subs = new Map<string, LocalSub>();
  private seq = 0;
  private port: MessagePort | null = null;
  private worker: SharedWorker | null = null;
  private hub: HubCore | null = null;
  private localPort: HubPort | null = null;
  private token = '';
  private started = false;
  private ready = false;
  private readyTimer: ReturnType<typeof setTimeout> | null = null;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private bound = false;

  start(token = getAuthToken() ?? ''): void {
    this.token = token;
    this.unauthorized = false;
    if (this.started) {
      this.send({ type: 'token', token });
      return;
    }
    this.started = true;
    this.bindLifecycle();
    if (typeof SharedWorker !== 'undefined') {
      try {
        this.worker = new SharedWorker(new URL('./shared.worker.ts', import.meta.url), {
          type: 'module',
          name: 'jack-ketch-realtime'
        });
        this.port = this.worker.port;
        this.port.onmessage = (ev: MessageEvent) => this.receive(ev.data as Record<string, unknown>);
        this.port.start();
        this.send({ type: 'init', id: crypto.randomUUID(), token });
        this.readyTimer = setTimeout(() => {
          if (!this.ready) this.fallback();
        }, FALLBACK_MS);
        return;
      } catch {
        this.worker = null;
        this.port = null;
      }
    }
    this.fallback();
  }

  stop(): void {
    this.started = false;
    if (this.readyTimer) clearTimeout(this.readyTimer);
    this.readyTimer = null;
    if (this.pingTimer) clearInterval(this.pingTimer);
    this.pingTimer = null;
    this.send({ type: 'bye' });
    this.worker?.port.close();
    this.worker = null;
    this.port = null;
    this.hub?.stop();
    this.hub = null;
    this.localPort = null;
    this.ready = false;
    this.state = 'closed';
    this.leader = true;
  }

  subscribe(topic: string, params: Record<string, unknown>, onFrame: FrameHandler): RealtimeSubscription {
    const id = `s${++this.seq}`;
    this.subs.set(id, { topic, params, onFrame });
    this.send({ type: 'sub', id, topic, params });
    return {
      close: () => {
        this.subs.delete(id);
        this.send({ type: 'unsub', id });
      },
      update: (next) => {
        const sub = this.subs.get(id);
        if (!sub) return;
        sub.params = next;
        this.send({ type: 'unsub', id });
        this.send({ type: 'sub', id, topic, params: next });
      }
    };
  }

  private fallback(): void {
    if (this.hub) return;
    if (this.readyTimer) clearTimeout(this.readyTimer);
    this.readyTimer = null;
    this.worker?.port.close();
    this.worker = null;
    this.port = null;
    this.available = false;
    const hub = new HubCore();
    this.hub = hub;
    this.localPort = {
      id: 'local',
      visible: true,
      lastSeen: Date.now(),
      send: (msg) => this.receive(msg)
    };
    hub.addPort(this.localPort);
    hub.setToken(this.token);
    hub.start();
    for (const [id, sub] of this.subs) {
      hub.subscribe('local', id, sub.topic, sub.params);
    }
    this.ready = true;
    this.startPing();
  }

  private startPing(): void {
    if (!this.pingTimer) {
      this.pingTimer = setInterval(() => {
        this.send({ type: 'ping' });
        if (this.localPort && this.hub) this.hub.ping('local');
      }, PING_MS);
    }
  }

  private receive(msg: Record<string, unknown>): void {
    const type = String(msg['type'] ?? '');
    if (type === 'ready') {
      this.ready = true;
      this.available = true;
      if (this.readyTimer) clearTimeout(this.readyTimer);
      this.readyTimer = null;
      this.startPing();
      return;
    }
    if (type === 'state') {
      this.state = String(msg['state'] ?? 'closed') as StreamState;
      return;
    }
    if (type === 'leader') {
      this.leader = !!msg['leader'];
      return;
    }
    if (type === 'unauthorized') {
      this.unauthorized = true;
      notifyUnauthorized();
      return;
    }
    if (type === 'frame') {
      const id = String(msg['id'] ?? '');
      const sub = this.subs.get(id);
      if (sub) sub.onFrame((msg['frame'] as Frame) ?? {});
    }
  }

  private send(msg: Record<string, unknown>): void {
    if (this.port) {
      try {
        this.port.postMessage(msg);
        return;
      } catch {
      }
    }
    if (!this.hub || !this.localPort) return;
    const type = String(msg['type'] ?? '');
    if (type === 'sub') {
      this.hub.subscribe('local', String(msg['id']), String(msg['topic']), (msg['params'] as Record<string, unknown>) ?? {});
    } else if (type === 'unsub') {
      this.hub.unsubscribe('local', String(msg['id']));
    } else if (type === 'token') {
      this.hub.setToken(String(msg['token'] ?? ''));
    } else if (type === 'visible') {
      this.hub.setVisible('local', !!msg['visible']);
    } else if (type === 'ping') {
      this.hub.ping('local');
    }
  }

  private bindLifecycle(): void {
    if (this.bound || typeof window === 'undefined') return;
    this.bound = true;
    const visibility = () => this.send({ type: 'visible', visible: document.visibilityState === 'visible' });
    const online = () => {
      if (this.token) this.send({ type: 'visible', visible: document.visibilityState === 'visible' });
    };
    document.addEventListener('visibilitychange', visibility);
    window.addEventListener('online', online);
    window.addEventListener('pagehide', () => this.send({ type: 'bye' }));
  }
}

export const realtime = new RealtimeClient();
export type { Frame, StreamState };
