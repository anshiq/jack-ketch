export interface YamlPathTarget {
  value: string;
  startColumn: number;
  dirsOnly: boolean;
}

const PATH_KEY = /^(\s*)(?:-\s+)?(workdir|env_file|file)\s*:/;

export function yamlPathTarget(line: string, column: number): YamlPathTarget | null {
  const match = PATH_KEY.exec(line);
  if (!match) return null;
  const key = match[2];
  const colon = match[0].length - 1;
  if (colon < 0) return null;
  let i = colon + 1;
  while (i < line.length && (line[i] === ' ' || line[i] === '\t')) i++;
  const quote = line[i] === '"' || line[i] === "'" ? line[i] : '';
  if (quote) i++;
  const startColumn = i + 1;
  if (column < startColumn) return null;
  const cursor = column - 1;
  let valueEnd = line.length;
  if (quote) {
    const close = line.indexOf(quote, i);
    if (close >= 0) valueEnd = close;
  }
  if (cursor > valueEnd) return null;
  const value = line.slice(i, Math.min(cursor, valueEnd));
  if (!quote) {
    const comment = value.indexOf(' #');
    if (comment >= 0) return null;
  }
  return { value, startColumn, dirsOnly: key === 'workdir' };
}
