import { SystemService, setNetworkObserver } from '../api';
import { cache } from '../data/query.svelte';

export type ConnectionState = 'online' | 'degraded' | 'offline';

class Connection {
  state = $state<ConnectionState>('online');
  version = $state('');
  apiVersion = $state('');
  uptimeSeconds = $state(0);
  socketPath = $state('');
  dataDir = $state('');
  tcpAddr = $state('');
  streams = $state<Record<string, string>>({});
  incompatible = $state(false);
  lastCheck = $state(0);
  lastUpdateAt = $state(Date.now());
  private failures = 0;
  private timer: ReturnType<typeof setInterval> | null = null;
  private probing = false;

  get reachable(): boolean {
    return this.state !== 'offline';
  }

  get degraded(): boolean {
    return this.state === 'degraded';
  }

  reportNetwork(ok: boolean) {
    if (ok) {
      this.failures = 0;
      if (this.state === 'offline') this.setState('online');
      return;
    }
    this.failures++;
    if (this.failures >= 2) this.setState('offline');
    else if (this.state === 'online') this.setState('degraded');
  }

  private setState(next: ConnectionState) {
    if (this.state === next) return;
    if (next === 'offline' && typeof document !== 'undefined') {
      document.documentElement.dataset.offline = 'true';
    } else if (typeof document !== 'undefined') {
      delete document.documentElement.dataset.offline;
    }
    this.state = next;
  }

  async check(): Promise<boolean> {
    if (this.probing) return this.reachable;
    this.probing = true;
    try {
      const v = await SystemService.version();
      this.version = v.daemonVersion;
      this.apiVersion = v.apiVersion;
      this.lastCheck = Date.now();
      this.failures = 0;
      this.setState('online');
      const h = await SystemService.health();
      this.uptimeSeconds = h.uptimeSeconds ?? Math.round((h.uptimeMs ?? 0) / 1000);
      this.socketPath = h.socketPath ?? '';
      this.dataDir = h.dataDir ?? '';
      this.tcpAddr = h.tcpAddr ?? '';
      this.lastUpdateAt = Date.now();
      return true;
    } catch {
      this.reportNetwork(false);
      return false;
    } finally {
      this.probing = false;
    }
  }

  setStream(name: string, state: string) {
    this.streams[name] = state;
    this.lastUpdateAt = Date.now();
  }

  setLive(state: string) {
    if (state === 'open') {
      const wasOffline = this.state !== 'online';
      this.lastUpdateAt = Date.now();
      if (this.failures === 0) this.setState('online');
      if (wasOffline) cache.revalidate('reconnect');
      return;
    }
    if ((state === 'connecting' || state === 'reconnecting') && this.state === 'online') this.setState('degraded');
  }

  start() {
    if (this.timer) return () => {};
    setNetworkObserver((ok) => this.reportNetwork(ok));
    const initial = setTimeout(() => void this.check(), 22);
    this.timer = setInterval(() => {
      if (this.state !== 'online') void this.check();
    }, 5000);
    if (typeof window !== 'undefined') {
      window.addEventListener('online', () => void this.check());
    }
    return () => {
      clearTimeout(initial);
      setNetworkObserver(null);
      if (this.timer) clearInterval(this.timer);
      this.timer = null;
    };
  }
}

export const connection = new Connection();
