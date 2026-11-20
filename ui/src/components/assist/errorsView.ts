import type { ErrorGroup, ErrorLocation } from '../../lib/api';

export interface KindInfo {
  label: string;
  color: string;
}

const KINDS: Record<string, KindInfo> = {
  compile_error: { label: 'Build', color: '--err' },
  panic: { label: 'Panic', color: '--err' },
  exception: { label: 'Exception', color: '--err' },
  test_failure: { label: 'Test', color: '--warn' },
  browser_error: { label: 'Browser', color: '--info' },
  lint_error: { label: 'Lint', color: '--warn' },
  log_error: { label: 'Log', color: '--text-2' }
};

export function kindInfo(kind: string): KindInfo {
  return KINDS[kind] ?? { label: kind.replace(/_/g, ' '), color: '--text-2' };
}

export function locationLabel(l: ErrorLocation): string {
  let s = l.file;
  if (l.line > 0) s += `:${l.line}`;
  if (l.column > 0) s += `:${l.column}`;
  return s;
}

export function errorsDigest(groups: ErrorGroup[]): string {
  return groups
    .map((g, i) => {
      const where = g.locations.find((l) => l.inProject) ?? g.locations[0];
      const head = `${i + 1}. [${kindInfo(g.kind).label.toLowerCase()}] ${g.message}${g.count > 1 ? ` (x${g.count})` : ''}`;
      return where ? `${head}\n   at ${locationLabel(where)}` : head;
    })
    .join('\n');
}
