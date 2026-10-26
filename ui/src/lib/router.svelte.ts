import { hasAuthToken } from './api/auth';

export interface RouteMatch {
  path: string;
  params: Record<string, string>;
}

const titles: [RegExp, string][] = [
  [/^\/$/, 'Overview · jack-ketch'],
  [/^\/processes\/[^/]+/, 'Process detail · jack-ketch'],
  [/^\/processes/, 'Processes · jack-ketch'],
  [/^\/logs/, 'Logs · jack-ketch'],
  [/^\/apps/, 'Apps · jack-ketch'],
  [/^\/config/, 'Config · jack-ketch'],
  [/^\/events/, 'Events · jack-ketch'],
  [/^\/projects/, 'Projects · jack-ketch'],
  [/^\/sessions/, 'Sessions · jack-ketch'],
  [/^\/integrations/, 'Integrations · jack-ketch'],
  [/^\/audit/, 'Audit · jack-ketch'],
  [/^\/settings/, 'Settings · jack-ketch'],
  [/^\/login/, 'Login · jack-ketch']
];

function applyTitle(path: string) {
  const clean = path.split('?')[0] ?? '/';
  for (const [re, t] of titles) {
    if (re.test(clean)) {
      document.title = t;
      return;
    }
  }
  document.title = 'jack-ketch';
}

function compile(pattern: string): { re: RegExp; keys: string[] } {
  const keys: string[] = [];
  const body = pattern
    .split('/')
    .map((seg) => {
      if (seg.startsWith(':')) {
        const optional = seg.endsWith('?');
        keys.push(seg.slice(1).replace(/\?$/, ''));
        return optional ? '(?:/([^/]+))?' : '/([^/]+)';
      }
      return seg ? `/${seg}` : '';
    })
    .join('');
  return { re: new RegExp(`^${body || '/'}$`), keys };
}

export function matchPath(pattern: string, path: string): RouteMatch | null {
  const { re, keys } = compile(pattern);
  const [rawPath, query] = path.split('?');
  const m = re.exec(rawPath ?? '/');
  if (!m) return null;
  const params: Record<string, string> = {};
  keys.forEach((k, i) => {
    if (m[i + 1] !== undefined) params[k] = decodeURIComponent(m[i + 1] as string);
  });
  if (query) {
    for (const [k, v] of new URLSearchParams(query)) params[k] = v;
  }
  return { path: rawPath ?? '/', params };
}

function parseLocation(): string {
  const path = window.location.pathname || '/';
  return path + window.location.search;
}

function normalize(path: string): string {
  if (!path) return '/';
  if (path.startsWith('#/')) return path.slice(1);
  if (path.startsWith('/')) return path;
  return `/${path}`;
}

function withScope(path: string): string {
  if (path.startsWith('/login')) return path;
  let url: URL;
  try {
    url = new URL(path, window.location.origin);
  } catch {
    return path;
  }
  if (url.origin !== window.location.origin) return path;
  if (!url.searchParams.has('ws')) {
    const ws = new URLSearchParams(window.location.search).get('ws');
    if (ws !== null) url.searchParams.set('ws', ws);
  }
  return url.pathname + url.search + url.hash;
}

type LeaveGuard = () => boolean | Promise<boolean>;

class Router {
  path = $state(typeof window !== 'undefined' ? parseLocation() : '/');
  private guards: LeaveGuard[] = [];
  private lastScroll = new Map<string, number>();

  constructor() {
    if (typeof window === 'undefined') return;
    const legacy = window.location.hash.match(/^#\/(.*)$/);
    if (legacy) window.history.replaceState(null, '', `/${legacy[1]}${window.location.search}`);
    this.path = parseLocation();
    window.addEventListener('popstate', () => {
      this.saveScroll();
      this.path = parseLocation();
      this.apply();
      this.restoreScroll();
    });
    document.addEventListener('click', (e) => this.onClick(e));
    this.apply();
  }

  private apply() {
    applyTitle(this.path);
    if (!hasAuthToken() && !this.path.startsWith('/login')) {
      void this.navigate('/login?next=' + encodeURIComponent(this.path), { replace: true });
    }
  }

  private scroller(): HTMLElement | null {
    const host = document.querySelector('[data-route-active="true"]');
    return host?.querySelector<HTMLElement>('.page') ?? document.querySelector<HTMLElement>('.content');
  }

  private saveScroll() {
    const el = this.scroller();
    if (el) this.lastScroll.set(this.path, el.scrollTop);
  }

  private announce() {
    const host = document.querySelector('[data-route-active="true"]');
    const h = host?.querySelector('h1');
    if (!(h instanceof HTMLElement)) return;
    h.setAttribute('tabindex', '-1');
    h.focus({ preventScroll: true });
    const live = document.getElementById('route-announcer');
    if (live) live.textContent = h.textContent?.trim() ?? '';
  }

  private restoreScroll() {
    requestAnimationFrame(() => {
      const el = this.scroller();
      if (el) el.scrollTop = this.lastScroll.get(this.path) ?? 0;
      this.announce();
    });
  }

  private onClick(e: MouseEvent) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const target = e.target as Element | null;
    const a = target?.closest('a[href]');
    if (!(a instanceof HTMLAnchorElement)) return;
    if (a.target || a.hasAttribute('download') || a.dataset.native !== undefined) return;
    const href = a.getAttribute('href') ?? '';
    if (!href || href.startsWith('#') && !href.startsWith('#/')) return;
    if (href.startsWith('//')) return;
    let url: URL;
    try {
      url = new URL(href, window.location.href);
    } catch {
      return;
    }
    if (url.origin !== window.location.origin) return;
    const path = href.startsWith('#/') ? href.slice(1) : url.pathname + url.search;
    e.preventDefault();
    void this.navigate(path);
  }

  async navigate(path: string, opts: { replace?: boolean; preserveScroll?: boolean } = {}) {
    const next = withScope(normalize(path));
    for (const g of this.guards) {
      const ok = await g();
      if (!ok) return;
    }
    if (!opts.preserveScroll) this.saveScroll();
    if (opts.replace) window.history.replaceState(null, '', next);
    else window.history.pushState(null, '', next);
    this.path = parseLocation();
    this.apply();
    if (opts.preserveScroll) this.announce();
    else this.restoreScroll();
  }

  updateParams(patch: Record<string, string | null>, replace = true) {
    if (typeof window === 'undefined') return;
    const url = new URL(window.location.href);
    let changed = false;
    for (const [key, value] of Object.entries(patch)) {
      if (value === null || value === '') {
        if (url.searchParams.has(key)) {
          url.searchParams.delete(key);
          changed = true;
        }
      } else if (url.searchParams.get(key) !== value) {
        url.searchParams.set(key, value);
        changed = true;
      }
    }
    if (!changed) return;
    const next = url.pathname + url.search + url.hash;
    if (replace) window.history.replaceState(window.history.state, '', next);
    else window.history.pushState(window.history.state, '', next);
    this.path = parseLocation();
    this.apply();
  }

  match(pattern: string): RouteMatch | null {
    return matchPath(pattern, this.path);
  }

  onBeforeLeave(g: LeaveGuard): () => void {
    this.guards.push(g);
    return () => {
      this.guards = this.guards.filter((x) => x !== g);
    };
  }

  link(node: HTMLAnchorElement, path: string) {
    const click = (e: MouseEvent) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey) return;
      e.preventDefault();
      void this.navigate(path);
    };
    node.addEventListener('click', click);
    return {
      destroy() {
        node.removeEventListener('click', click);
      }
    };
  }
}

export const router = new Router();

export function navigate(path: string, opts?: { replace?: boolean }) {
  return router.navigate(path, opts);
}
