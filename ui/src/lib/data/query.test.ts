import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cache, mutation, query } from './query.svelte';

function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void; reject: (e: unknown) => void } {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
}

describe('query cache', () => {
  beforeEach(() => {
    cache.reset();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('dedupes in-flight requests for the same key', async () => {
    const d = deferred<string>();
    let calls = 0;
    const fetcher = () => {
      calls++;
      return d.promise;
    };
    const a = query(() => 'same', fetcher);
    const b = query(() => 'same', fetcher);
    const pa = a.refetch();
    const pb = b.refetch();
    d.resolve('v');
    await Promise.all([pa, pb]);
    await flush();
    expect(calls).toBe(1);
    expect(a.data).toBe('v');
    expect(b.data).toBe('v');
  });

  it('refetches stale data', async () => {
    let calls = 0;
    const q = query(
      () => 'stale',
      async () => {
        calls++;
        return calls;
      },
      { staleMs: 0 }
    );
    await q.refetch();
    await q.refetch();
    expect(calls).toBe(2);
    expect(q.data).toBe(2);
  });

  it('aborts the previous request when the key changes', async () => {
    const signals: AbortSignal[] = [];
    let key = 'one';
    const q = query(
      () => key,
      (signal) => {
        signals.push(signal);
        return new Promise<string>(() => {});
      }
    );
    void q.refetch();
    await flush();
    key = 'two';
    void q.refetch();
    await flush();
    expect(signals.length).toBeGreaterThanOrEqual(2);
    expect(signals[0]!.aborted).toBe(true);
  });

  it('rolls back optimistic mutations on failure', async () => {
    cache.set('items', ['a']);
    const m = mutation(
      async (_args: { add: string }) => {
        throw new Error('nope');
      },
      {
        errorToast: false,
        optimistic: (args) => {
          const prev = cache.get<string[]>('items') ?? [];
          cache.set('items', [...prev, args.add]);
          return () => cache.set('items', prev);
        }
      }
    );
    const res = await m.run({ add: 'b' });
    expect(res).toBeUndefined();
    expect(cache.get<string[]>('items')).toEqual(['a']);
    expect(m.error).toBe('nope');
  });

  it('persists entries in session storage and hydrates them stale', async () => {
    const store = new Map<string, string>();
    vi.stubGlobal('sessionStorage', {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => store.set(k, v),
      removeItem: (k: string) => store.delete(k),
      clear: () => store.clear(),
      key: () => null,
      length: 0
    });
    cache.reset();
    const q = query(() => 'persisted', async () => ({ n: 1 }), { persist: true });
    await q.refetch();
    cache.reset();
    const again = query(() => 'persisted', async () => ({ n: 2 }), { persist: true, staleMs: 60000 });
    expect(again.data).toEqual({ n: 1 });
  });

  it('garbage collects idle entries', () => {
    cache.set('gc', 1);
    const entry = cache as unknown as { gc(): void };
    const map = cache as unknown as { get<T>(k: string): T | undefined };
    expect(map.get('gc')).toBe(1);
    vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 10 * 60 * 1000);
    entry.gc();
    expect(map.get('gc')).toBeUndefined();
    vi.restoreAllMocks();
  });
});
