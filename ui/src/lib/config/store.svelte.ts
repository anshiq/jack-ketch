import { ConfigSession } from './session.svelte';

const MAX = 4;
const cache = new Map<string, ConfigSession>();
let blank: ConfigSession | null = null;

export function configSession(workspaceId: string): ConfigSession {
  if (!workspaceId) {
    if (!blank) blank = new ConfigSession();
    return blank;
  }
  const hit = cache.get(workspaceId);
  if (hit) {
    cache.delete(workspaceId);
    cache.set(workspaceId, hit);
    return hit;
  }
  const session = new ConfigSession();
  cache.set(workspaceId, session);
  while (cache.size > MAX) {
    const oldest = cache.entries().next().value;
    if (!oldest) break;
    const [id, old] = oldest;
    cache.delete(id);
    old.destroy();
  }
  void session.load(workspaceId);
  return session;
}

export function forgetConfigSession(workspaceId: string): void {
  const session = cache.get(workspaceId);
  if (!session) return;
  cache.delete(workspaceId);
  session.destroy();
}

export function cachedConfigSessionsDirty(): boolean {
  for (const session of cache.values()) if (session.anyDirty) return true;
  return blank?.anyDirty ?? false;
}
