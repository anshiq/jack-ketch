import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { HubCore, type HubPort } from './hub';

interface Call {
  url: string;
  body: Record<string, unknown>;
}

function port(id: string, visible = true): HubPort & { messages: Record<string, unknown>[] } {
  const messages: Record<string, unknown>[] = [];
  return { id, visible, lastSeen: Date.now(), messages, send: (m) => messages.push(m) };
}

function pending(): Promise<Response> {
  return new Promise(() => {});
}

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

async function settle(): Promise<void> {
  await new Promise((r) => setTimeout(r, 10));
}

function bodyOf(input: RequestInfo | URL, init?: RequestInit): Record<string, unknown> {
  void input;
  try {
    return JSON.parse(String(init?.body ?? '{}')) as Record<string, unknown>;
  } catch {
    return {};
  }
}

describe('HubCore', () => {
  let calls: Call[];

  beforeEach(() => {
    calls = [];
    vi.stubGlobal('fetch', (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), body: bodyOf(input, init) });
      return String(input).endsWith('/Connect') ? Promise.resolve(streaming([{ kind: 'hello', streamId: 'stream-1', heartbeatMs: 15000 }], true)) : pending();
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('gives every tab subscription its own server subscription and frames', async () => {
    const hub = new HubCore();
    const a = port('a');
    const b = port('b');
    hub.addPort(a);
    hub.addPort(b);
    hub.subscribe('a', 's1', 'processes', { allWorkspaces: true });
    hub.subscribe('b', 's2', 'processes', { allWorkspaces: true });
    hub.subscribe('b', 's3', 'processes', { allWorkspaces: true });
    hub.setToken('tok');
    hub.start();
    await settle();
    const connect = calls.find((c) => c.url.endsWith('/Connect'));
    expect(connect).toBeTruthy();
    const subs = connect!.body['subscriptions'] as { sub: string }[];
    expect(subs.length).toBe(3);
    const internal = hub as unknown as { onFrame: (m: Record<string, unknown>) => void };
    for (const s of subs) internal.onFrame({ sub: s.sub, kind: 'snapshot', snapshot: [] });
    const frameIds = (p: { messages: Record<string, unknown>[] }) => p.messages.filter((m) => m['type'] === 'frame').map((m) => m['id']);
    expect(frameIds(a)).toEqual(['s1']);
    expect(frameIds(b).sort()).toEqual(['s2', 's3']);
    hub.unsubscribe('a', 's1');
    expect(calls.filter((c) => c.url.endsWith('/Unsubscribe')).length).toBe(1);
    hub.dropPort('b');
    expect(calls.filter((c) => c.url.endsWith('/Unsubscribe')).length).toBe(3);
    hub.stop();
  });

  it('subscribes late joiners separately so they receive their own snapshot', async () => {
    const hub = new HubCore();
    const a = port('a');
    hub.addPort(a);
    hub.subscribe('a', 's1', 'processes', {});
    hub.setToken('tok');
    hub.start();
    await settle();
    const b = port('b');
    hub.addPort(b);
    hub.subscribe('b', 's1', 'processes', {});
    const late = calls.filter((c) => c.url.endsWith('/Subscribe'));
    expect(late.length).toBe(1);
    expect(late[0]!.body['topic']).toBe('processes');
    expect(late[0]!.body['streamId']).toBe('stream-1');
    hub.stop();
  });

  it('resubscribes with the last event cursor', () => {
    const hub = new HubCore();
    const a = port('a');
    hub.addPort(a);
    hub.subscribe('a', 's1', 'events', {});
    hub.setToken('tok');
    hub.start();
    const first = calls.find((c) => c.url.endsWith('/Connect'))!;
    const sub = (first.body['subscriptions'] as { sub: string }[])[0]!;
    const internal = hub as unknown as {
      onFrame: (m: Record<string, unknown>) => void;
      attempt: number;
      live: boolean;
      connect: () => void;
    };
    internal.onFrame({ sub: sub.sub, kind: 'event', id: 7, cursor: '7' });
    internal.attempt++;
    internal.live = false;
    internal.connect();
    const second = calls.filter((c) => c.url.endsWith('/Connect')).pop()!;
    expect((second.body['subscriptions'] as { cursor: Record<string, unknown> }[])[0]!.cursor).toEqual({ since: '7' });
    hub.stop();
  });

  it('makes the most recently visible port the leader', () => {
    const hub = new HubCore();
    const a = port('a');
    const b = port('b');
    hub.addPort(a);
    hub.addPort(b);
    expect(hub.leader()).toBe('b');
    hub.setVisible('a', true);
    expect(hub.leader()).toBe('a');
    hub.setVisible('a', false);
    hub.setVisible('b', false);
    expect(hub.leader()).toBe('b');
    hub.stop();
  });
});
