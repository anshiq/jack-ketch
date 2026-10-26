import { describe, expect, it } from 'vitest';
import type { HarnessInfo, ScopeName, ScopeReport, ScopeStatus } from '../../lib/api/types';
import { actionLabel, filterHarnesses, hue, initials, needsAttention, scopeOf, scopeTitle, sortHarnesses, statusLabel, statusTone } from './harness';

function report(scope: ScopeName, status: ScopeStatus): ScopeReport {
  return { scope, status, portable: false, note: '', entries: [], error: '' };
}

function harness(id: string, over: Partial<HarnessInfo> = {}): HarnessInfo {
  return {
    id,
    displayName: id,
    tier: 1,
    docsUrl: '',
    detected: true,
    version: '',
    evidence: [],
    scopes: [],
    skillsSupported: false,
    manualSteps: '',
    restartHint: '',
    maxWaitMs: 0,
    connected: 0,
    ...over
  };
}

describe('statusTone', () => {
  it('maps every status', () => {
    expect(statusTone('installed')).toBe('ok');
    expect(statusTone('outdated')).toBe('warn');
    expect(statusTone('broken')).toBe('err');
    expect(statusTone('absent')).toBe('neutral');
    expect(statusTone('unavailable')).toBe('neutral');
  });
});

describe('statusLabel', () => {
  it('labels every status', () => {
    expect(statusLabel('installed')).toBe('Connected');
    expect(statusLabel('outdated')).toBe('Update available');
    expect(statusLabel('broken')).toBe('Needs repair');
    expect(statusLabel('unavailable')).toBe('Select a project');
    expect(statusLabel('absent')).toBe('Not connected');
  });
});

describe('actionLabel', () => {
  it('picks the verb', () => {
    expect(actionLabel('outdated')).toBe('Update');
    expect(actionLabel('broken')).toBe('Repair');
    expect(actionLabel('absent')).toBe('Connect');
    expect(actionLabel('installed')).toBe('Connect');
  });
});

describe('scopeTitle', () => {
  it('describes scopes', () => {
    expect(scopeTitle('user')).toBe('All projects');
    expect(scopeTitle('project')).toBe('This project · shared');
    expect(scopeTitle('local')).toBe('This project · only me');
  });
});

describe('needsAttention', () => {
  it('flags outdated and broken only', () => {
    expect(needsAttention(harness('a', { scopes: [report('user', 'outdated')] }), 'user')).toBe(true);
    expect(needsAttention(harness('a', { scopes: [report('user', 'broken')] }), 'user')).toBe(true);
    expect(needsAttention(harness('a', { scopes: [report('user', 'installed')] }), 'user')).toBe(false);
    expect(needsAttention(harness('a', { scopes: [report('user', 'absent')] }), 'user')).toBe(false);
  });
});

describe('filterHarnesses', () => {
  const manual = harness('cline', { displayName: 'Cline', tier: 2, detected: false });
  const detected = harness('cursor', { displayName: 'Cursor', scopes: [report('user', 'installed')] });
  const broken = harness('codex', { displayName: 'Codex', scopes: [report('user', 'broken')] });

  it('hides undetected tier-2 agents unless showManual', () => {
    expect(filterHarnesses([manual, detected], 'user', '', 'all', false)).toEqual([detected]);
    expect(filterHarnesses([manual, detected], 'user', '', 'all', true)).toHaveLength(2);
  });

  it('matches the query against id and display name', () => {
    expect(filterHarnesses([manual, detected, broken], 'user', 'curs', 'all', true)).toEqual([detected]);
    expect(filterHarnesses([manual, detected, broken], 'user', 'cline', 'all', true)).toEqual([manual]);
  });

  it('applies the detected, connected and attention filters', () => {
    expect(filterHarnesses([manual, detected, broken], 'user', '', 'detected', true)).toEqual([detected, broken]);
    expect(filterHarnesses([manual, detected, broken], 'user', '', 'connected', true)).toEqual([detected]);
    expect(filterHarnesses([manual, detected, broken], 'user', '', 'attention', true)).toEqual([broken]);
  });
});

describe('sortHarnesses', () => {
  it('puts broken and outdated first and sorts by name inside a rank', () => {
    const absent = harness('zeta', { displayName: 'Zeta', tier: 2, detected: false });
    const connected = harness('beta', { displayName: 'Beta', scopes: [report('user', 'installed')] });
    const broken = harness('omega', { displayName: 'Omega', scopes: [report('user', 'broken')] });
    const outdated = harness('alpha', { displayName: 'Alpha', scopes: [report('user', 'outdated')] });
    const seen = harness('mid', { displayName: 'Mid', scopes: [report('user', 'absent')] });
    expect(sortHarnesses([absent, connected, broken, outdated, seen], 'user').map((h) => h.id)).toEqual(['alpha', 'omega', 'beta', 'mid', 'zeta']);
  });
});

describe('scopeOf', () => {
  it('finds the selected scope report', () => {
    const h = harness('a', { scopes: [report('user', 'installed'), report('project', 'absent')] });
    expect(scopeOf(h, 'project')?.status).toBe('absent');
    expect(scopeOf(h, 'local')).toBeNull();
  });
});

describe('initials', () => {
  it('strips parentheticals and takes the first letters', () => {
    expect(initials('VS Code (GitHub Copilot)')).toBe('VC');
    expect(initials('Claude Code')).toBe('CC');
    expect(initials('  ')).toBe('?');
  });

  it('uses both halves of a hyphenated name', () => {
    expect(initials('claude-code')).toBe('CC');
  });
});

describe('hue', () => {
  it('is stable and within range', () => {
    const a = hue('claude-code');
    expect(a).toBe(hue('claude-code'));
    expect(a).toBeGreaterThanOrEqual(0);
    expect(a).toBeLessThan(360);
  });
});
