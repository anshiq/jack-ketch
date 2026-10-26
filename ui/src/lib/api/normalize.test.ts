import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { toSession, toAuditEntry, toEvent, toHarnessInfo, toProcess, toResource, toSkillState } from './normalize';

const harnesses = JSON.parse(
  readFileSync(new URL('./__fixtures__/harnesses.json', import.meta.url), 'utf8')
) as { harnesses: Record<string, unknown>[] };

describe('toProcess', () => {
  it('normalises snake_case list rows', () => {
    const p = toProcess({ process_id: 'proc_1', status: 'running', command: 'npm run', pid: 12, ports: null } as unknown as Record<string, unknown>);
    expect(p.id).toBe('proc_1');
    expect(p.ports).toEqual([]);
    expect(p.status).toBe('running');
  });
  it('normalises camelCase watch rows', () => {
    const p = toProcess({ id: 'proc_2', processId: 'proc_2', status: 'failed', command: 'api', ports: [3000] } as unknown as Record<string, unknown>);
    expect(p.id).toBe('proc_2');
    expect(p.ports).toEqual([3000]);
  });
});

describe('toEvent', () => {
  it('parses payload JSON', () => {
    const e = toEvent({ id: 3, ts: 1000, type: 'process.exited', payload: '{"exitCode":1}' } as unknown as Record<string, unknown>);
    expect(e.payload).toEqual({ exitCode: 1 });
    expect(e.cursor).toBe('3');
  });
});

describe('toAuditEntry', () => {
  it('maps duration', () => {
    const a = toAuditEntry({ id: 1, ts: 100, action: 'ProcessService.Start', result: 'ok', durationMs: 5 } as unknown as Record<string, unknown>);
    expect(a.duration).toBe(5);
  });
});

describe('epoch handling', () => {
  it('converts audit ts seconds to milliseconds', () => {
    const a = toAuditEntry({ id: 1, ts: 1790678310, action: 'x', result: 'ok' } as unknown as Record<string, unknown>);
    expect(a.time).toBe(1790678310000);
  });

  it('maps session seconds, lastSeenAt and closedAt', () => {
    const s = toSession({ id: 'sess_1', kind: 'mcp', harness: 'claude', clientPid: 42, workspaceId: 'ws', startedAt: 1790678000, lastSeenAt: 1790678300, closedAt: 1790678310 } as unknown as Record<string, unknown>);
    expect(s.started).toBe(1790678000000);
    expect(s.lastSeen).toBe(1790678300000);
    expect(s.closed).toBe(true);
    expect(s.clientPid).toBe(42);
  });

  it('treats a session without closedAt as open', () => {
    const s = toSession({ id: 'sess_2', startedAt: 1790678000, lastSeenAt: 1790678000 } as unknown as Record<string, unknown>);
    expect(s.closed).toBe(false);
  });
});

describe('toResource', () => {
  it('keeps cpuNanos and percent', () => {
    const r = toResource({ processId: 'p', cpuNanos: 10, cpuPercent: 2.5 } as unknown as Record<string, unknown>);
    expect(r.cpuNanos).toBe(10);
    expect(r.cpuPercent).toBe(2.5);
  });
});

describe('toHarnessInfo', () => {
  it('normalises the flattened daemon report', () => {
    const raw = harnesses.harnesses[0] as Record<string, unknown>;
    const h = toHarnessInfo(raw);
    expect(h.id).toBe('claude-code');
    expect(h.displayName).toBe('Claude Code');
    expect(h.tier).toBe(1);
    expect(h.detected).toBe(true);
    expect(h.version).toBe('1.2.3');
    expect(h.evidence).toEqual(['/home/dev/.claude', 'claude on PATH']);
    expect(h.connected).toBe(2);
    expect(h.skillsSupported).toBe(true);
    expect(h.restartHint).toBe('restart Claude Code');
    expect(h.maxWaitMs).toBe(120000);
    expect(h.scopes.map((s) => s.scope)).toEqual(['user', 'project']);
    expect(h.scopes[0]?.status).toBe('installed');
    expect(h.scopes[0]?.portable).toBe(false);
    expect(h.scopes[0]?.entries[0]?.installed).toBe(true);
    expect(h.scopes[0]?.entries[0]?.command).toBe('jack-ketch');
    expect(h.scopes[0]?.entries[0]?.args).toEqual(['serve']);
    expect(h.scopes[1]?.status).toBe('outdated');
    expect(h.scopes[1]?.note).toBe('shared with the team');
    expect(h.scopes[1]?.entries[0]?.upToDate).toBe(false);
    expect(h.scopes[1]?.entries[0]?.problems).toEqual(['command outdated']);
  });

  it('falls back to nested detection and applies defaults', () => {
    const raw = harnesses.harnesses[1] as Record<string, unknown>;
    const h = toHarnessInfo(raw);
    expect(h.tier).toBe(2);
    expect(h.detected).toBe(false);
    expect(h.version).toBe('');
    expect(h.evidence).toEqual([]);
    expect(h.skillsSupported).toBe(false);
    expect(h.connected).toBe(0);
    expect(h.maxWaitMs).toBe(0);
    expect(h.scopes[0]?.status).toBe('absent');
    expect(h.scopes[0]?.entries).toEqual([]);
  });

  it('maps unknown scope and status values to defaults', () => {
    const h = toHarnessInfo({ detection: { detected: true, version: '9.9.9' }, scopes: [{ scope: 'workspace', status: 'weird' }] });
    expect(h.detected).toBe(true);
    expect(h.version).toBe('9.9.9');
    expect(h.scopes[0]?.scope).toBe('user');
    expect(h.scopes[0]?.status).toBe('absent');
    expect(h.scopes[0]?.portable).toBe(false);
    expect(h.scopes[0]?.error).toBe('');
  });
});

describe('toSkillState', () => {
  it('maps the skill install state and location metadata', () => {
    const st = toSkillState({
      name: 'jack-ketch-ready',
      description: 'how to use jack-ketch',
      want: '8',
      have: '7',
      dir: '/home/dev/.claude/skills/jack-ketch-ready',
      target: '/home/dev/.claude/skills/jack-ketch-ready',
      visible: true,
      own: false,
      shared: true,
      outdated: true
    });
    expect(st).toEqual({
      name: 'jack-ketch-ready',
      description: 'how to use jack-ketch',
      want: '8',
      have: '7',
      dir: '/home/dev/.claude/skills/jack-ketch-ready',
      target: '/home/dev/.claude/skills/jack-ketch-ready',
      visible: true,
      own: false,
      shared: true,
      outdated: true
    });
  });
});
