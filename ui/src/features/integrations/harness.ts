import type { HarnessInfo, ScopeName, ScopeReport, ScopeStatus } from '../../lib/api/types';

export type HarnessFilter = 'all' | 'detected' | 'connected' | 'attention';

export function scopeOf(h: HarnessInfo, scope: ScopeName): ScopeReport | null {
  return h.scopes.find((s) => s.scope === scope) ?? null;
}

export function statusTone(s: ScopeStatus): 'ok' | 'warn' | 'err' | 'neutral' {
  if (s === 'installed') return 'ok';
  if (s === 'outdated') return 'warn';
  if (s === 'broken') return 'err';
  return 'neutral';
}

export function statusLabel(s: ScopeStatus): string {
  switch (s) {
    case 'installed':
      return 'Connected';
    case 'outdated':
      return 'Update available';
    case 'broken':
      return 'Needs repair';
    case 'unavailable':
      return 'Select a project';
    default:
      return 'Not connected';
  }
}

export function actionLabel(s: ScopeStatus): string {
  if (s === 'outdated') return 'Update';
  if (s === 'broken') return 'Repair';
  return 'Connect';
}

export function scopeTitle(scope: ScopeName): string {
  if (scope === 'project') return 'This project · shared';
  if (scope === 'local') return 'This project · only me';
  return 'All projects';
}

export function needsAttention(h: HarnessInfo, scope: ScopeName): boolean {
  const s = scopeOf(h, scope)?.status;
  return s === 'outdated' || s === 'broken';
}

export function filterHarnesses(list: HarnessInfo[], scope: ScopeName, query: string, filter: HarnessFilter, showManual: boolean): HarnessInfo[] {
  const q = query.trim().toLowerCase();
  return list.filter((h) => {
    if (!showManual && h.tier === 2 && !h.detected) return false;
    if (q && !h.displayName.toLowerCase().includes(q) && !h.id.includes(q)) return false;
    if (filter === 'detected') return h.detected;
    if (filter === 'connected') return scopeOf(h, scope)?.status === 'installed';
    if (filter === 'attention') return needsAttention(h, scope);
    return true;
  });
}

export function sortHarnesses(list: HarnessInfo[], scope: ScopeName): HarnessInfo[] {
  const rank = (h: HarnessInfo) => {
    const s = scopeOf(h, scope)?.status;
    if (s === 'broken' || s === 'outdated') return 0;
    if (s === 'installed') return 1;
    if (h.detected) return 2;
    return h.tier === 1 ? 3 : 4;
  };
  return [...list].sort((a, b) => rank(a) - rank(b) || a.displayName.localeCompare(b.displayName));
}

export function initials(name: string): string {
  const parts = name.replace(/\(.*?\)/g, '').trim().split(/[\s-]+/).filter(Boolean);
  return ((parts[0]?.[0] ?? '') + (parts[1]?.[0] ?? '')).toUpperCase() || '?';
}

export function hue(id: string): number {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) % 360;
  return h;
}
