import { describe, expect, it } from 'vitest';
import type { SkillState } from '../../lib/api/types';
import { countLabel, primaryActionLabel, removeActionLabel, skillBadge, skillCounts, versionLabel } from './skills';

function skill(over: Partial<SkillState> = {}): SkillState {
  return {
    name: 'jack-ketch-ready',
    description: '',
    want: '8',
    have: '8',
    dir: '/home/dev/.claude/skills/jack-ketch-ready',
    target: '/home/dev/.claude/skills/jack-ketch-ready',
    visible: true,
    own: true,
    shared: false,
    outdated: false,
    ...over
  };
}

describe('skillBadge', () => {
  it('maps every state', () => {
    expect(skillBadge(skill({ visible: false }))).toEqual({ tone: 'neutral', label: 'Not installed' });
    expect(skillBadge(skill({ outdated: true }))).toEqual({ tone: 'warn', label: 'Update available' });
    expect(skillBadge(skill())).toEqual({ tone: 'ok', label: 'Installed' });
  });
});

describe('versionLabel', () => {
  it('shows installed, missing and update versions', () => {
    expect(versionLabel(skill())).toBe('v8');
    expect(versionLabel(skill({ visible: false }))).toBe('v8');
    expect(versionLabel(skill({ have: '7', outdated: true }))).toBe('v7 → v8');
  });
});

describe('skillCounts', () => {
  it('counts installed, outdated, missing, shared and owned', () => {
    const counts = skillCounts([
      skill(),
      skill({ name: 'a', outdated: true }),
      skill({ name: 'b', visible: false, own: false }),
      skill({ name: 'c', shared: true, own: false })
    ]);
    expect(counts).toEqual({ installed: 3, outdated: 1, missing: 1, shared: 1, owned: 2 });
  });
});

describe('primaryActionLabel', () => {
  it('describes the next action', () => {
    expect(primaryActionLabel([])).toBe('Install');
    expect(primaryActionLabel([skill(), skill({ name: 'a' })])).toBe('All installed');
    expect(primaryActionLabel([skill({ visible: false })])).toBe('Install 1');
    expect(primaryActionLabel([skill({ outdated: true, have: '7' })])).toBe('Update 1');
    expect(primaryActionLabel([skill({ outdated: true, have: '7' }), skill({ name: 'a', visible: false })])).toBe('Install 1, update 1');
  });
});

describe('removeActionLabel', () => {
  it('includes the owned count', () => {
    expect(removeActionLabel(2)).toBe('Remove 2');
    expect(removeActionLabel(0)).toBe('Remove');
  });
});

describe('countLabel', () => {
  it('pluralises', () => {
    expect(countLabel(1, 'skill')).toBe('1 skill');
    expect(countLabel(2, 'skill')).toBe('2 skills');
  });
});
