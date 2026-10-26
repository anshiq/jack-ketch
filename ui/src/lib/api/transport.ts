import { clearAuthToken, getAuthToken } from './auth';

export class ApiError extends Error {
  status: number;
  code: string;
  details: unknown;
  service: string;
  method: string;
  constructor(message: string, status: number, code: string, service = '', method = '', details: unknown = undefined) {
    super(message);
    this.status = status;
    this.code = code;
    this.service = service;
    this.method = method;
    this.details = details;
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError;
}

export function codeOf(e: unknown): string {
  return e instanceof ApiError ? e.code : 'internal';
}

let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn;
}

export function notifyUnauthorized(): void {
  onUnauthorized?.();
}

let onNetwork: ((ok: boolean) => void) | null = null;
export function setNetworkObserver(fn: ((ok: boolean) => void) | null): void {
  onNetwork = fn;
}

function headers(): HeadersInit {
  const h: Record<string, string> = {
    'Content-Type': 'application/json',
    'X-Jack-Ketch-Client': `web/${__APP_VERSION__}`
  };
  const token = getAuthToken();
  if (token) h['Authorization'] = `Bearer ${token}`;
  return h;
}

export async function call<Req, Res>(
  service: string,
  method: string,
  body: Req,
  opts: { signal?: AbortSignal; timeoutMs?: number } = {}
): Promise<Res> {
  const controller = new AbortController();
  const timeout = opts.timeoutMs ?? 15000;
  const timer = setTimeout(() => controller.abort(), timeout);
  const forwardAbort = () => controller.abort();
  if (opts.signal) {
    if (opts.signal.aborted) controller.abort();
    else opts.signal.addEventListener('abort', forwardAbort);
  }
  try {
    const res = await fetch(`/api/jackketch.v1.${service}/${method}`, {
      method: 'POST',
      headers: headers(),
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: controller.signal
    });
    if (res.status === 401) {
      clearAuthToken();
      onUnauthorized?.();
      throw new ApiError('unauthorized', 401, 'unauthorized', service, method);
    }
    onNetwork?.(true);
    const text = await res.text();
    let parsed: unknown = undefined;
    if (text) {
      try {
        parsed = JSON.parse(text);
      } catch {
        parsed = undefined;
      }
    }
    if (!res.ok) {
      const p = (parsed ?? {}) as { error?: string; code?: string; details?: unknown };
      throw new ApiError(p.error ?? text ?? `${res.status}`, res.status, p.code ?? 'internal', service, method, p.details);
    }
    return (parsed ?? {}) as Res;
  } catch (err) {
    if (err instanceof ApiError) throw err;
    onNetwork?.(false);
    if (controller.signal.aborted) throw new ApiError('request timed out', 0, 'timeout', service, method);
    if (err instanceof TypeError) throw new ApiError(err.message, 0, 'unavailable', service, method);
    throw new ApiError(err instanceof Error ? err.message : String(err), 0, 'unavailable', service, method);
  } finally {
    clearTimeout(timer);
    opts.signal?.removeEventListener('abort', forwardAbort);
  }
}

export interface StreamMessage {
  kind?: string;
  cursor?: string | number;
  [key: string]: unknown;
}

export type StreamState = 'connecting' | 'open' | 'reconnecting' | 'closed';
export type StreamStateHandler = (s: StreamState) => void;
export interface StreamHandle {
  close: () => void;
}
export type StreamHandler = (msg: StreamMessage) => void;

export function openStream(
  service: string,
  method: string,
  body: Record<string, unknown>,
  handlers: {
    onMessage: (msg: StreamMessage) => void;
    onState?: (s: StreamState) => void;
    onGap?: (dropped: number) => void;
    onSnapshot?: (msg: StreamMessage) => void;
    resume?: () => Record<string, unknown>;
  },
  opts: { heartbeatTimeoutMs?: number } = {}
): { close: () => void } {
  const hbTimeout = opts.heartbeatTimeoutMs ?? 40000;
  let closed = false;
  let retryMs = 500;
  let attemptId = 0;
  let controller: AbortController | null = null;
  let reader: ReadableStreamDefaultReader<Uint8Array> | null = null;
  let attemptLive = false;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let watchdog: ReturnType<typeof setInterval> | null = null;
  let healthySince = 0;
  let lastFrame = 0;
  let unauthorized = false;

  const clearRetry = () => {
    if (retryTimer) clearTimeout(retryTimer);
    retryTimer = null;
  };

  const schedule = (delay: number) => {
    clearRetry();
    retryTimer = setTimeout(() => {
      retryTimer = null;
      void connect();
    }, Math.max(500, delay));
  };

  const kick = () => {
    if (closed || attemptLive || unauthorized) return;
    if (typeof navigator !== 'undefined' && navigator.onLine === false) return;
    clearRetry();
    retryMs = 500;
    void connect();
  };

  const onlineHandler = () => kick();
  const visHandler = () => {
    if (document.visibilityState === 'visible') kick();
  };
  if (typeof window !== 'undefined') {
    window.addEventListener('online', onlineHandler);
    document.addEventListener('visibilitychange', visHandler);
    watchdog = setInterval(() => {
      if (closed || !attemptLive || !controller) return;
      if (Date.now() - lastFrame > hbTimeout) {
        controller.abort();
        void reader?.cancel().catch(() => {});
      }
    }, 5000);
  }

  const cleanup = () => {
    if (typeof window !== 'undefined') {
      window.removeEventListener('online', onlineHandler);
      document.removeEventListener('visibilitychange', visHandler);
      if (watchdog) clearInterval(watchdog);
      watchdog = null;
    }
    clearRetry();
  };

  const release = (id: number) => {
    if (id !== attemptId) return;
    attemptLive = false;
    void reader?.cancel().catch(() => {});
    reader = null;
    controller?.abort();
    controller = null;
  };

  const connect = async (): Promise<void> => {
    if (closed || attemptLive || unauthorized) return;
    if (typeof navigator !== 'undefined' && navigator.onLine === false) {
      handlers.onState?.('reconnecting');
      schedule(2000);
      return;
    }
    const id = ++attemptId;
    const ac = new AbortController();
    controller = ac;
    attemptLive = true;
    handlers.onState?.('connecting');
    let retryAfter = 0;
    try {
      const patch = handlers.resume?.() ?? {};
      const res = await fetch(`/api/jackketch.v1.${service}/${method}`, {
        method: 'POST',
        headers: headers(),
        body: JSON.stringify({ ...body, ...patch }),
        signal: ac.signal
      });
      if (id !== attemptId) return;
      if (res.status === 401) {
        unauthorized = true;
        clearAuthToken();
        onUnauthorized?.();
        handlers.onState?.('closed');
        return;
      }
      if (!res.ok || !res.body) {
        const ra = res.headers.get('Retry-After');
        if (ra) retryAfter = Math.min(15000, Number(ra) * 1000 || 0);
        throw new Error(`stream ${res.status}`);
      }
      handlers.onState?.('open');
      healthySince = Date.now();
      lastFrame = Date.now();
      const r = res.body.getReader();
      reader = r;
      const decoder = new TextDecoder();
      let buf = '';
      for (;;) {
        const { done, value } = await r.read();
        if (done || closed || id !== attemptId) break;
        buf += decoder.decode(value, { stream: true });
        const parts = buf.split('\n');
        buf = parts.pop() ?? '';
        for (const line of parts) {
          if (!line.trim()) continue;
          lastFrame = Date.now();
          let msg: StreamMessage;
          try {
            msg = JSON.parse(line) as StreamMessage;
          } catch {
            continue;
          }
          if (msg.kind === 'heartbeat' || msg.kind === 'hello') continue;
          if (msg.kind === 'gap') {
            handlers.onGap?.(typeof msg['dropped'] === 'number' ? (msg['dropped'] as number) : 0);
            continue;
          }
          if (msg.kind === 'snapshot') {
            handlers.onSnapshot?.(msg);
            continue;
          }
          handlers.onMessage(msg);
        }
        if (Date.now() - healthySince > 10000) {
          retryMs = 500;
          healthySince = Date.now();
        }
      }
    } catch {
      if (closed || id !== attemptId) return;
    } finally {
      release(id);
    }
    if (closed || id !== attemptId || unauthorized) return;
    handlers.onState?.('reconnecting');
    const jitter = Math.random() * retryMs;
    const delay = retryAfter || Math.min(retryMs / 2 + jitter, 15000);
    schedule(delay);
    retryMs = Math.min(retryMs * 2, 15000);
  };

  void connect();
  return {
    close() {
      if (closed) return;
      closed = true;
      attemptId++;
      handlers.onState?.('closed');
      void reader?.cancel().catch(() => {});
      reader = null;
      controller?.abort();
      controller = null;
      attemptLive = false;
      cleanup();
    }
  };
}
