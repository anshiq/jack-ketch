import { HubCore, type HubPort } from './hub';

const hub = new HubCore();
hub.start();

interface Connection {
  port: MessagePort;
  id: string;
}

const connections = new Map<MessagePort, Connection>();

function attach(port: MessagePort, id: string): HubPort {
  const state: HubPort = {
    id,
    visible: true,
    lastSeen: Date.now(),
    send: (msg) => {
      try {
        port.postMessage(msg);
      } catch {
      }
    }
  };
  hub.addPort(state);
  return state;
}

const worker = self as unknown as { onconnect: ((event: MessageEvent) => void) | null };

worker.onconnect = (event: MessageEvent) => {
  const port = event.ports[0];
  if (!port) return;
  let state: HubPort | null = null;
  port.onmessage = (ev: MessageEvent) => {
    const msg = ev.data as Record<string, unknown>;
    const type = String(msg['type'] ?? '');
    if (type === 'init') {
      if (state) {
        hub.dropPort(state.id);
        connections.delete(port);
      }
      const id = String(msg['id'] ?? '');
      if (!id) return;
      state = attach(port, id);
      state.lastSeen = Date.now();
      connections.set(port, { port, id });
      port.postMessage({ type: 'ready' });
      if (typeof msg['token'] === 'string') hub.setToken(msg['token'] as string);
      return;
    }
    if (!state) return;
    state.lastSeen = Date.now();
    if (type === 'token') {
      hub.setToken(String(msg['token'] ?? ''));
    } else if (type === 'sub') {
      hub.subscribe(state.id, String(msg['id'] ?? ''), String(msg['topic'] ?? ''), (msg['params'] as Record<string, unknown>) ?? {});
    } else if (type === 'unsub') {
      hub.unsubscribe(state.id, String(msg['id'] ?? ''));
    } else if (type === 'visible') {
      hub.setVisible(state.id, !!msg['visible']);
    } else if (type === 'ping') {
      hub.ping(state.id);
    } else if (type === 'bye') {
      hub.dropPort(state.id);
      connections.delete(port);
      state = null;
      port.postMessage({ type: 'pong' });
    }
  };
  port.start();
};
