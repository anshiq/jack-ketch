export interface PathSplit {
  dir: string;
  prefix: string;
}

export type { DirectoryEntry, DirectoryListing } from './api/types';

const WIN_DRIVE_ROOT = /^[A-Za-z]:[\\/]?$/;

export function splitPathInput(value: string): PathSplit {
  if (value === '') return { dir: '', prefix: '' };
  if (value === '~') return { dir: '~', prefix: '' };
  if (value === '.' || value === '..') return { dir: value, prefix: '' };
  if (WIN_DRIVE_ROOT.test(value)) {
    const dir = value.endsWith('\\') || value.endsWith('/') ? value : `${value}\\`;
    return { dir, prefix: '' };
  }
  const slash = Math.max(value.lastIndexOf('/'), value.lastIndexOf('\\'));
  if (slash < 0) return { dir: '', prefix: value };
  return { dir: value.slice(0, slash + 1), prefix: value.slice(slash + 1) };
}

export function joinPathEntry(dir: string, name: string, isDir: boolean): string {
  const sep = separatorFor(dir);
  let joined: string;
  if (dir === '') joined = name;
  else if (dir.endsWith('/') || dir.endsWith('\\')) joined = dir + name;
  else joined = dir + sep + name;
  if (isDir && !joined.endsWith('/') && !joined.endsWith('\\')) joined += sep;
  return joined;
}

export function pathSuggestionBase(value: string, resolvedDir: string, absolute: boolean): string {
  if (absolute) return resolvedDir;
  const { dir } = splitPathInput(value);
  if (dir === '~' || dir.startsWith('~/') || dir.startsWith('~\\')) return resolvedDir;
  return dir;
}

function separatorFor(dir: string): '/' | '\\' {
  if (dir.includes('\\') && !dir.includes('/')) return '\\';
  if (dir.includes('\\') && dir.lastIndexOf('\\') > dir.lastIndexOf('/')) return '\\';
  return '/';
}
