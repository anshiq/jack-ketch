import type { SkillState } from '../../lib/api/types';

export interface SkillCounts {
  installed: number;
  outdated: number;
  missing: number;
  shared: number;
  owned: number;
}

export function skillCounts(states: SkillState[]): SkillCounts {
  const counts: SkillCounts = { installed: 0, outdated: 0, missing: 0, shared: 0, owned: 0 };
  for (const st of states) {
    if (!st.visible) {
      counts.missing++;
      continue;
    }
    counts.installed++;
    if (st.outdated) counts.outdated++;
    if (st.shared) counts.shared++;
    else counts.owned++;
  }
  return counts;
}

export function skillBadge(st: SkillState): { tone: 'ok' | 'warn' | 'neutral'; label: string } {
  if (!st.visible) return { tone: 'neutral', label: 'Not installed' };
  if (st.outdated) return { tone: 'warn', label: 'Update available' };
  return { tone: 'ok', label: 'Installed' };
}

export function versionLabel(st: SkillState): string {
  if (!st.visible) return `v${st.want}`;
  if (st.outdated) return `v${st.have} → v${st.want}`;
  return `v${st.have}`;
}

export function primaryActionLabel(states: SkillState[]): string {
  const c = skillCounts(states);
  if (states.length === 0) return 'Install';
  if (c.missing === 0 && c.outdated === 0) return 'All installed';
  if (c.missing === 0) return `Update ${c.outdated}`;
  if (c.outdated === 0) return `Install ${c.missing}`;
  return `Install ${c.missing}, update ${c.outdated}`;
}

export function removeActionLabel(owned: number): string {
  return owned > 0 ? `Remove ${owned}` : 'Remove';
}

export function countLabel(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}
