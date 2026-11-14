import type { AuditEntry, DaemonEvent } from './api/types';
import type { ErrorSeries } from './api/services';

export interface Mark {
  ts: number;
  lane: string;
  tone: 'ok' | 'err' | 'warn' | 'info' | 'neutral';
  label: string;
  detail: string;
  href?: string;
  kind: 'event' | 'audit';
}

export interface Insight {
  processId: string;
  spikeAt: number;
  errors: number;
  cause?: Mark;
  gapMs?: number;
}

export const WINDOWS: { value: string; label: string; ms: number }[] = [
  { value: '15m', label: '15m', ms: 15 * 60_000 },
  { value: '1h', label: '1h', ms: 60 * 60_000 },
  { value: '6h', label: '6h', ms: 6 * 60 * 60_000 },
  { value: '24h', label: '24h', ms: 24 * 60 * 60_000 }
];

export function auditTone(a: AuditEntry): Mark['tone'] {
  if (a.result.startsWith('error') || a.result.includes('denied')) return 'err';
  return 'info';
}

export function auditLabel(a: AuditEntry): string {
  const who = a.harness || (a.session ? a.session.slice(0, 10) : 'api');
  return `${who} · ${a.action}`;
}

export function firstSpike(series: ErrorSeries, from: number, bucketMs: number): { at: number; errors: number } | null {
  let quiet = 0;
  for (let i = 0; i < series.counts.length; i++) {
    const c = series.counts[i] ?? 0;
    if (c === 0) {
      quiet++;
      continue;
    }
    if (quiet >= 2 || i === 0) {
      let errors = 0;
      for (let j = i; j < Math.min(series.counts.length, i + 3); j++) errors += series.counts[j] ?? 0;
      return { at: from + i * bucketMs, errors };
    }
    quiet = 0;
  }
  return null;
}

export function insights(series: ErrorSeries[], marks: Mark[], from: number, bucketMs: number, lookbackMs = 10 * 60_000): Insight[] {
  const out: Insight[] = [];
  const sorted = [...marks].sort((a, b) => a.ts - b.ts);
  for (const s of series) {
    const spike = firstSpike(s, from, bucketMs);
    if (!spike) continue;
    const windowEnd = spike.at + bucketMs;
    let cause: Mark | undefined;
    for (const m of sorted) {
      if (m.ts > windowEnd) break;
      if (m.ts < spike.at - lookbackMs) continue;
      if (m.tone === 'err' && m.lane === s.processId) continue;
      if (m.lane === s.processId || m.lane === 'agent' || m.lane === 'config') cause = m;
    }
    out.push({ processId: s.processId, spikeAt: spike.at, errors: spike.errors, cause, gapMs: cause ? Math.max(0, spike.at - cause.ts) : undefined });
  }
  return out.sort((a, b) => a.spikeAt - b.spikeAt);
}

export function ticks(from: number, to: number, target = 6): number[] {
  const span = to - from;
  const steps = [60_000, 5 * 60_000, 10 * 60_000, 15 * 60_000, 30 * 60_000, 3_600_000, 2 * 3_600_000, 3 * 3_600_000, 6 * 3_600_000];
  const step = steps.find((s) => span / s <= target) ?? 12 * 3_600_000;
  const out: number[] = [];
  const offset = new Date(from).getTimezoneOffset() * 60_000;
  for (let t = Math.ceil((from - offset) / step) * step + offset; t <= to; t += step) out.push(t);
  return out;
}

export function eventLane(e: DaemonEvent): string {
  if (e.type.startsWith('config.')) return 'config';
  return e.processId || 'other';
}
