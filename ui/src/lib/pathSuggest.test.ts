import { describe, expect, it } from 'vitest';
import { joinPathEntry, pathSuggestionBase, splitPathInput } from './pathSuggest';

describe('splitPathInput', () => {
  it('handles empty values', () => {
    expect(splitPathInput('')).toEqual({ dir: '', prefix: '' });
  });
  it('splits absolute paths', () => {
    expect(splitPathInput('/home/ni')).toEqual({ dir: '/home/', prefix: 'ni' });
    expect(splitPathInput('/home/')).toEqual({ dir: '/home/', prefix: '' });
    expect(splitPathInput('/')).toEqual({ dir: '/', prefix: '' });
  });
  it('splits relative paths', () => {
    expect(splitPathInput('src')).toEqual({ dir: '', prefix: 'src' });
    expect(splitPathInput('src/co')).toEqual({ dir: 'src/', prefix: 'co' });
    expect(splitPathInput('./co')).toEqual({ dir: './', prefix: 'co' });
  });
  it('keeps home and dot navigation as directories', () => {
    expect(splitPathInput('~')).toEqual({ dir: '~', prefix: '' });
    expect(splitPathInput('~/')).toEqual({ dir: '~/', prefix: '' });
    expect(splitPathInput('~/co')).toEqual({ dir: '~/', prefix: 'co' });
    expect(splitPathInput('.')).toEqual({ dir: '.', prefix: '' });
    expect(splitPathInput('..')).toEqual({ dir: '..', prefix: '' });
  });
  it('handles windows drives and separators', () => {
    expect(splitPathInput('C:')).toEqual({ dir: 'C:\\', prefix: '' });
    expect(splitPathInput('C:\\')).toEqual({ dir: 'C:\\', prefix: '' });
    expect(splitPathInput('C:\\Users\\ni')).toEqual({ dir: 'C:\\Users\\', prefix: 'ni' });
  });
});

describe('joinPathEntry', () => {
  it('joins into the typed directory', () => {
    expect(joinPathEntry('/home/', 'nixos', true)).toBe('/home/nixos/');
    expect(joinPathEntry('/home', 'nixos', false)).toBe('/home/nixos');
    expect(joinPathEntry('src/', 'main.go', false)).toBe('src/main.go');
    expect(joinPathEntry('', '.env', false)).toBe('.env');
    expect(joinPathEntry('', 'src', true)).toBe('src/');
  });
  it('handles home and windows separators', () => {
    expect(joinPathEntry('~', 'projects', true)).toBe('~/projects/');
    expect(joinPathEntry('C:\\Users\\', 'nick', true)).toBe('C:\\Users\\nick\\');
  });
});

describe('pathSuggestionBase', () => {
  it('keeps relative values in their typed directory', () => {
    expect(pathSuggestionBase('src/co', '/home/me/app', false)).toBe('src/');
  });
  it('uses the resolved directory for absolute fields', () => {
    expect(pathSuggestionBase('', '/home/me', true)).toBe('/home/me');
    expect(pathSuggestionBase('proj', '/home/me', true)).toBe('/home/me');
  });
  it('uses the resolved directory for home-relative values', () => {
    expect(pathSuggestionBase('~/proj', '/home/me', false)).toBe('/home/me');
  });
});
