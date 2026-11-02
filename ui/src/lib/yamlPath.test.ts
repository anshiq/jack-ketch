import { describe, expect, it } from 'vitest';
import { yamlPathTarget } from './yamlPath';

describe('yamlPathTarget', () => {
  it('matches workdir and env_file values', () => {
    expect(yamlPathTarget('workdir: src/ma', 16)).toEqual({ value: 'src/ma', startColumn: 10, dirsOnly: true });
    expect(yamlPathTarget('  env_file: .env.local', 23)).toEqual({ value: '.env.local', startColumn: 13, dirsOnly: false });
  });
  it('matches env source list items', () => {
    expect(yamlPathTarget('      - file: .env.local', 25)).toEqual({ value: '.env.local', startColumn: 15, dirsOnly: false });
  });
  it('strips quotes from the replacement value', () => {
    expect(yamlPathTarget('  env_file: ".env', 18)).toEqual({ value: '.env', startColumn: 14, dirsOnly: false });
  });
  it('ignores other keys and positions before the value', () => {
    expect(yamlPathTarget('command: npm run dev', 19)).toBeNull();
    expect(yamlPathTarget('workdir: src', 5)).toBeNull();
  });
  it('ignores cursors in trailing comments', () => {
    expect(yamlPathTarget('env_file: .env # keep', 22)).toBeNull();
  });
});
