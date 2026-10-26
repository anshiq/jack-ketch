export interface Toast {
  id: number;
  message: string;
  tone: 'info' | 'ok' | 'err';
  action?: { label: string; onClick: () => void };
  count: number;
  createdAt: number;
  timer: ReturnType<typeof setTimeout> | null;
  remaining: number;
  startedAt: number;
}

let nextId = 1;

class Toasts {
  items = $state<Toast[]>([]);
  private readonly max = 3;

  get visible(): Toast[] {
    return this.items.slice(-this.max);
  }

  get overflow(): number {
    return Math.max(0, this.items.length - this.max);
  }

  private schedule(t: Toast) {
    if (t.timer) clearTimeout(t.timer);
    if (t.action) return;
    t.startedAt = Date.now();
    t.timer = setTimeout(() => this.dismiss(t.id), t.remaining);
  }

  push(message: string, tone: Toast['tone'] = 'info', action?: Toast['action']): number {
    const now = Date.now();
    const existing = this.items.find((t) => t.message === message && t.tone === tone && now - t.createdAt < 4000);
    if (existing) {
      existing.count++;
      existing.createdAt = now;
      existing.remaining = tone === 'err' ? 10000 : 5000;
      this.schedule(existing);
      this.items = [...this.items.filter((t) => t.id !== existing.id), existing];
      return existing.id;
    }
    const toast: Toast = {
      id: nextId++,
      message,
      tone,
      action,
      count: 1,
      createdAt: now,
      timer: null,
      remaining: tone === 'err' ? 10000 : 5000,
      startedAt: now
    };
    this.items.push(toast);
    this.schedule(toast);
    return toast.id;
  }

  ok(message: string, action?: Toast['action']) {
    return this.push(message, 'ok', action);
  }

  err(message: string, action?: Toast['action']) {
    return this.push(message, 'err', action);
  }

  info(message: string, action?: Toast['action']) {
    return this.push(message, 'info', action);
  }

  pause(id: number) {
    const t = this.items.find((x) => x.id === id);
    if (!t || !t.timer) return;
    clearTimeout(t.timer);
    t.timer = null;
    t.remaining = Math.max(1000, t.remaining - (Date.now() - t.startedAt));
  }

  resume(id: number) {
    const t = this.items.find((x) => x.id === id);
    if (!t || t.timer || t.action) return;
    this.schedule(t);
  }

  dismiss(id: number) {
    const t = this.items.find((x) => x.id === id);
    if (t?.timer) clearTimeout(t.timer);
    this.items = this.items.filter((x) => x.id !== id);
  }
}

export const toasts = new Toasts();

export function toastError(err: unknown, fallback = 'Something went wrong'): void {
  const message = err instanceof Error ? err.message : String(err ?? fallback);
  toasts.err(message);
}
