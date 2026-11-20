import { describe, expect, it } from 'vitest';
import { nestedKey, overlapCount } from './nested.svelte';
import type { NestedProject } from '../api';

const side = (id: string) => ({ projectId: id, projectName: id, workspaceId: `ws_${id}`, path: `/p/${id}` });

const sample: NestedProject = {
  child: side('c'),
  parent: side('p'),
  running: 0,
  apps: [
    { name: 'a', workdir: '/p/c', status: 'duplicate', parentApp: 'a', detail: '' },
    { name: 'b', workdir: '/p/c/b', status: 'conflict', parentApp: 'b', detail: '' },
    { name: 'c', workdir: '/p/c/c', status: 'unique', parentApp: '', detail: '' }
  ]
};

describe('nested helpers', () => {
  it('keys a pair by child and parent project', () => {
    expect(nestedKey(sample)).toBe('c>p');
  });

  it('counts only apps defined in both projects', () => {
    expect(overlapCount(sample)).toBe(2);
    expect(overlapCount({ ...sample, apps: [] })).toBe(0);
  });
});
