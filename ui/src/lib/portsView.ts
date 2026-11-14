import type { PortEntry } from './api/services';

export type PortKind = 'managed' | 'conflict' | 'declared' | 'other' | 'daemon';

export function portKind(p: PortEntry): PortKind {
  if (p.self) return 'daemon';
  if (p.conflict) return 'conflict';
  if (p.managed) return 'managed';
  if (!p.listening && (p.declared?.length ?? 0) > 0) return 'declared';
  return 'other';
}

export function portMatches(p: PortEntry, needle: string): boolean {
  const q = needle.trim().toLowerCase();
  if (!q) return true;
  const hay = [
    String(p.port),
    p.command ?? '',
    p.address ?? '',
    String(p.pid ?? ''),
    p.managed?.app ?? '',
    p.managed?.processId ?? '',
    p.managed?.workspacePath ?? '',
    ...(p.declared ?? []).flatMap((d) => [d.app, d.name, d.workspacePath])
  ]
    .join(' ')
    .toLowerCase();
  return hay.includes(q);
}

export function shortCommand(cmd: string, max = 80): string {
  const first = cmd.split(' ')[0] ?? '';
  const base = first.includes('/') ? first.slice(first.lastIndexOf('/') + 1) : first;
  const rest = cmd.slice(first.length);
  const out = base + rest;
  return out.length > max ? out.slice(0, max - 1) + '…' : out;
}

export function browserURL(p: PortEntry): string {
  const host = p.address && p.address !== '*' && p.address !== 'ipv6' && p.address !== '::1' ? p.address : 'localhost';
  return `http://${host}:${p.port}/`;
}

export function countKinds(ports: PortEntry[]): Record<PortKind | 'all', number> {
  const out: Record<PortKind | 'all', number> = { all: ports.length, managed: 0, conflict: 0, declared: 0, other: 0, daemon: 0 };
  for (const p of ports) out[portKind(p)]++;
  return out;
}
