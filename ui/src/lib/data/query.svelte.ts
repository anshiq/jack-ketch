import { toastError } from '../toasts.svelte';

export type QueryStatus = 'idle' | 'loading' | 'ok' | 'error';

export interface Query<T> {
  readonly data: T | undefined;
  readonly error: string;
  readonly status: QueryStatus;
  readonly fetching: boolean;
  readonly updatedAt: number;
  refetch(): Promise<void>;
}

export interface QueryOptions<T> {
  staleMs?: number;
  gcMs?: number;
  refetchOnFocus?: boolean;
  refetchOnReconnect?: boolean;
  persist?: boolean;
  enabled?: () => boolean;
  initial?: () => T | undefined;
}

interface Entry<T = unknown> {
  data?: T;
  updatedAt: number;
  persist: boolean;
  gcMs: number;
  inflight?: Promise<T>;
  controller?: AbortController;
  touchedAt: number;
  version: number;
  focus?: boolean;
  reconnect?: boolean;
}

const MAX_PERSIST = 256 * 1024;
const DEFAULT_GC = 5 * 60 * 1000;
const entries = new Map<string, Entry>();
const watchers = new Map<string, Set<() => void>>();
let foregroundCount = $state(0);
let gcTimer: ReturnType<typeof setInterval> | null = null;
let storage: Storage | null = null;

function storageOf(): Storage | null {
  if (storage) return storage;
  try {
    if (typeof sessionStorage !== 'undefined') storage = sessionStorage;
  } catch {
    storage = null;
  }
  return storage;
}

function readPersisted<T>(key: string): { data: T; updatedAt: number } | null {
  if (!storageOf()) return null;
  try {
    const raw = sessionStorage.getItem(`ar.q.${key}`);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { data: T; updatedAt: number };
    return parsed;
  } catch {
    return null;
  }
}

function writePersisted(key: string, data: unknown, updatedAt: number): void {
  if (!storageOf()) return;
  try {
    const raw = JSON.stringify({ data, updatedAt });
    if (raw.length > MAX_PERSIST) return;
    sessionStorage.setItem(`ar.q.${key}`, raw);
  } catch {
  }
}

function dropPersisted(key: string): void {
  if (!storageOf()) return;
  try {
    sessionStorage.removeItem(`ar.q.${key}`);
  } catch {
  }
}

function notify(key: string): void {
  const set = watchers.get(key);
  if (!set) return;
  for (const fn of set) fn();
}

function ensureGc(): void {
  if (gcTimer || typeof setInterval === 'undefined') return;
  gcTimer = setInterval(() => cache.gc(), 60000);
}

function request<T>(key: string, fetcher: (signal: AbortSignal) => Promise<T>, opts: QueryOptions<T> = {}): Promise<T> {
  let entry = entries.get(key) as Entry<T> | undefined;
  if (entry?.inflight) return entry.inflight;
  if (!entry) {
    entry = { updatedAt: 0, persist: opts.persist === true, gcMs: opts.gcMs ?? DEFAULT_GC, touchedAt: Date.now(), version: 0 };
    entries.set(key, entry);
  }
  entry.focus = entry.focus || opts.refetchOnFocus === true;
  entry.reconnect = entry.reconnect || opts.refetchOnReconnect === true;
  const ac = new AbortController();
  entry.controller = ac;
  foregroundCount++;
  const run = (async () => {
    try {
      const data = await fetcher(ac.signal);
      const current = entries.get(key);
      if (!current || current.controller !== ac) return data;
      current.data = data;
      current.updatedAt = Date.now();
      current.version++;
      current.controller = undefined;
      if (current.persist) writePersisted(key, data, current.updatedAt);
      notify(key);
      return data;
    } finally {
      foregroundCount = Math.max(0, foregroundCount - 1);
    }
  })();
  entry.inflight = run;
  void run.finally(() => {
    const current = entries.get(key) as Entry<T> | undefined;
    if (current === entry) current.inflight = undefined;
  }).catch(() => {});
  ensureGc();
  return run;
}

class QueryState<T> implements Query<T> {
  data = $state<T | undefined>(undefined);
  error = $state('');
  status = $state<QueryStatus>('idle');
  fetching = $state(false);
  updatedAt = $state(0);
  private keyFn: () => string;
  private fetcher: (signal: AbortSignal) => Promise<T>;
  private opts: QueryOptions<T>;
  private current = '';
  private watcher: (() => void) | null = null;
  private disposed = false;

  constructor(key: () => string, fetcher: (signal: AbortSignal) => Promise<T>, opts: QueryOptions<T> = {}) {
    this.keyFn = key;
    this.fetcher = fetcher;
    this.opts = opts;
    this.sync();
    queueMicrotask(() => {
      if (!this.disposed) this.ensure();
    });
  }

  refetch(): Promise<void> {
    return this.fetch(true);
  }

  dispose(): void {
    this.disposed = true;
    this.unwatch();
  }

  private readKey(): string {
    let k = '';
    try {
      k = this.keyFn();
    } catch {
      k = '';
    }
    if (k !== this.current) this.switchKey(k);
    return k;
  }

  private sync(): void {
    this.readKey();
    if (!this.current) {
      this.status = 'idle';
      return;
    }
    const entry = entries.get(this.current);
    if (entry?.data !== undefined) {
      this.data = entry.data as T;
      this.updatedAt = entry.updatedAt;
      this.status = 'ok';
    } else {
      const persisted = this.opts.persist ? readPersisted<T>(this.current) : null;
      if (persisted) {
        this.data = persisted.data;
        this.updatedAt = persisted.updatedAt;
        this.status = 'ok';
      } else if (this.opts.initial) {
        const v = this.opts.initial();
        if (v !== undefined) {
          this.data = v;
          this.status = 'ok';
        }
      }
    }
  }

  private switchKey(next: string): void {
    this.unwatch();
    const old = entries.get(this.current);
    if (old?.controller && (watchers.get(this.current)?.size ?? 0) === 0) old.controller.abort();
    this.current = next;
    this.error = '';
    this.fetching = false;
    this.data = undefined;
    this.status = 'idle';
    this.updatedAt = 0;
    this.sync();
    if (next) {
      const set = watchers.get(next) ?? new Set();
      this.watcher = () => {
        this.sync();
        this.ensure();
      };
      set.add(this.watcher);
      watchers.set(next, set);
    }
    queueMicrotask(() => {
      if (!this.disposed) this.ensure();
    });
  }

  private unwatch(): void {
    if (!this.watcher) return;
    watchers.get(this.current)?.delete(this.watcher);
    this.watcher = null;
  }

  private enabled(): boolean {
    return this.current !== '' && (this.opts.enabled?.() ?? true);
  }

  private ensure(): void {
    if (!this.enabled()) return;
    const entry = entries.get(this.current);
    const staleMs = this.opts.staleMs ?? 0;
    const age = entry ? Date.now() - entry.updatedAt : Infinity;
    if (!entry || age > staleMs) void this.fetch(false);
  }

  private async fetch(force: boolean): Promise<void> {
    if (!this.enabled()) return;
    const key = this.readKey();
    if (!key) return;
    if (!force) {
      const entry = entries.get(key);
      if (entry?.data !== undefined && Date.now() - entry.updatedAt <= (this.opts.staleMs ?? 0)) return;
      if (entry?.inflight) {
        this.fetching = true;
        this.status = this.data === undefined ? 'loading' : this.status;
        try {
          const data = (await entry.inflight) as T;
          this.apply(data, entry.updatedAt);
        } catch (err) {
          this.fail(err);
        } finally {
          this.fetching = false;
        }
        return;
      }
    }
    this.fetching = true;
    if (this.data === undefined) this.status = 'loading';
    try {
      const data = await request(key, this.fetcher, this.opts);
      const entry = entries.get(key);
      if (key !== this.current) return;
      this.apply(data, entry?.updatedAt ?? Date.now());
    } catch (err) {
      if (key !== this.current) return;
      this.fail(err);
    } finally {
      if (key === this.current) this.fetching = false;
    }
  }

  private apply(data: T, updatedAt: number): void {
    this.data = data;
    this.updatedAt = updatedAt;
    this.error = '';
    this.status = 'ok';
  }

  private fail(err: unknown): void {
    this.error = err instanceof Error ? err.message : String(err);
    this.status = 'error';
  }
}

export function query<T>(key: () => string, fetcher: (signal: AbortSignal) => Promise<T>, opts: QueryOptions<T> = {}): Query<T> {
  return new QueryState(key, fetcher, opts);
}

export const cache = {
  get<T>(key: string): T | undefined {
    const e = entries.get(key);
    if (!e) return undefined;
    e.touchedAt = Date.now();
    return e.data as T | undefined;
  },
  set<T>(key: string, value: T | ((prev: T | undefined) => T)): void {
    let entry = entries.get(key);
    if (!entry) {
      entry = { updatedAt: 0, persist: false, gcMs: DEFAULT_GC, touchedAt: Date.now(), version: 0 };
      entries.set(key, entry);
    }
    const next = typeof value === 'function' ? (value as (prev: T | undefined) => T)(entry.data as T | undefined) : value;
    entry.data = next;
    entry.updatedAt = Date.now();
    entry.version++;
    entry.touchedAt = Date.now();
    if (entry.persist) writePersisted(key, next, entry.updatedAt);
    notify(key);
  },
  invalidate(prefix: string): void {
    for (const key of [...entries.keys()]) {
      if (key === prefix || key.startsWith(prefix)) {
        const entry = entries.get(key)!;
        entry.inflight = undefined;
        entry.controller?.abort();
        entry.controller = undefined;
        if (entry.persist) dropPersisted(key);
        entry.updatedAt = 0;
        entry.data = undefined;
        notify(key);
      }
    }
  },
  prefetch<T>(key: string, fetcher: (signal: AbortSignal) => Promise<T>): Promise<void> {
    return request(key, fetcher).then(
      () => {},
      () => {}
    );
  },
  gc(): void {
    const now = Date.now();
    for (const [key, entry] of [...entries]) {
      if (entry.inflight || (watchers.get(key)?.size ?? 0) > 0) continue;
      const age = now - Math.max(entry.updatedAt, entry.touchedAt);
      if (age > entry.gcMs) {
        entry.controller?.abort();
        if (entry.persist) dropPersisted(key);
        entries.delete(key);
      }
    }
  },
  get foreground(): number {
    return foregroundCount;
  },
  revalidate(reason: 'focus' | 'reconnect'): void {
    for (const [key, entry] of entries) {
      if (reason === 'focus' ? !entry.focus : !entry.reconnect) continue;
      entry.updatedAt = 0;
      notify(key);
    }
  },
  reset(): void {
    for (const entry of entries.values()) entry.controller?.abort();
    entries.clear();
    watchers.clear();
    foregroundCount = 0;
  }
};

if (typeof window !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') cache.revalidate('focus');
  });
  window.addEventListener('online', () => cache.revalidate('reconnect'));
}

export function mutation<A, R>(
  fn: (args: A) => Promise<R>,
  opts: {
    optimistic?: (args: A) => () => void;
    onSuccess?: (res: R, args: A) => void;
    invalidate?: (args: A) => string[];
    errorToast?: boolean;
  } = {}
): { run(args: A): Promise<R | undefined>; readonly pending: boolean; readonly error: string } {
  let pending = $state(false);
  let error = $state('');
  return {
    get pending() {
      return pending;
    },
    get error() {
      return error;
    },
    async run(args: A): Promise<R | undefined> {
      pending = true;
      error = '';
      const rollback = opts.optimistic?.(args);
      try {
        const res = await fn(args);
        opts.onSuccess?.(res, args);
        for (const prefix of opts.invalidate?.(args) ?? []) cache.invalidate(prefix);
        return res;
      } catch (err) {
        rollback?.();
        error = err instanceof Error ? err.message : String(err);
        if (opts.errorToast !== false) toastError(err);
        return undefined;
      } finally {
        pending = false;
      }
    }
  };
}
