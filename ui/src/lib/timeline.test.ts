import { describe, expect, it } from 'vitest';
import { firstSpike, insights, ticks, type Mark } from './timeline';

const series = (counts: number[]) => ({ processId: 'p1', command: 'node', status: 'running', workspaceId: 'w', counts, total: counts.reduce((a, b) => a + b, 0) });

describe('timeline', () => {
  it('finds the first spike after a quiet period', () => {
    const s = firstSpike(series([0, 0, 0, 4, 2, 0]), 1000, 100);
    expect(s).toEqual({ at: 1300, errors: 6 });
    expect(firstSpike(series([0, 0, 0]), 0, 100)).toBeNull();
  });

  it('links a spike to the latest action shortly before it', () => {
    const marks: Mark[] = [
      { ts: 900, lane: 'agent', tone: 'info', label: 'claude-code · ConfigService/Apply', detail: 'ok', kind: 'audit' },
      { ts: 1250, lane: 'p1', tone: 'info', label: 'api restarted', detail: 'process.restarted', kind: 'event' },
      { ts: 5000, lane: 'agent', tone: 'info', label: 'late', detail: 'ok', kind: 'audit' }
    ];
    const [f] = insights([series([0, 0, 0, 4, 2, 0])], marks, 1000, 100);
    expect(f?.cause?.label).toBe('api restarted');
    expect(f?.gapMs).toBe(50);
  });

  it('produces evenly spaced ticks inside the window', () => {
    const from = Date.UTC(2026, 10, 13, 10, 0, 0);
    const t = ticks(from, from + 3_600_000, 6);
    expect(t.length).toBeGreaterThanOrEqual(4);
    expect(t.every((v) => v >= from && v <= from + 3_600_000)).toBe(true);
  });
});
