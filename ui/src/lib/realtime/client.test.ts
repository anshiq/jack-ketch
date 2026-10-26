import { afterEach, describe, expect, it, vi } from 'vitest';
import { RealtimeClient } from './index.svelte';

function streaming(frames: Record<string, unknown>[], keepOpen = false): Response {
  const enc = new TextEncoder();
  return new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        for (const f of frames) controller.enqueue(enc.encode(`${JSON.stringify(f)}\n`));
        if (!keepOpen) controller.close();
      }
    }),
    { status: 200, headers: { 'Content-Type': 'application/x-ndjson' } }
  );
}

describe('RealtimeClient fallback', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('uses an in-tab hub when SharedWorker is unavailable', async () => {
    vi.stubGlobal('SharedWorker', undefined);
    vi.stubGlobal('fetch', () =>
      Promise.resolve(
        streaming(
          [
            { kind: 'hello', streamId: 'stream-1', heartbeatMs: 15000 },
            { sub: 'w1', kind: 'snapshot', sessions: [] }
          ],
          true
        )
      )
    );
    const client = new RealtimeClient();
    const frames: Record<string, unknown>[] = [];
    client.subscribe('sessions', {}, (f) => frames.push(f));
    client.start('tok');
    await new Promise((r) => setTimeout(r, 30));
    expect(client.available).toBe(false);
    expect(client.state).toBe('open');
    expect(frames.some((f) => f['kind'] === 'snapshot')).toBe(true);
    client.stop();
  });
});
