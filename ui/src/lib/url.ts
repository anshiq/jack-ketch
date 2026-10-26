export function paramSet(path: string, key: string): Set<string> {
  const value = new URLSearchParams(path.split('?')[1] ?? '').get(key) ?? '';
  return new Set(value.split(',').map((x) => x.trim()).filter(Boolean));
}

export function listParam(path: string, key: string): string[] {
  return [...paramSet(path, key)];
}
