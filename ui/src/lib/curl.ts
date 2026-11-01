import type { CredentialField, CredentialKind } from './api/types';

export interface ParsedCurl {
  url: string;
  method: string;
  headers: { name: string; value: string }[];
  cookies: { name: string; value: string }[];
  basic: string;
  body: string;
}

export function tokenize(input: string): string[] {
  const text = input.replace(/\\\r?\n/g, ' ').replace(/\^\r?\n/g, ' ');
  const out: string[] = [];
  let cur = '';
  let has = false;
  let quote: '"' | "'" | null = null;
  let ansi = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i] as string;
    if (quote) {
      if (ch === quote) {
        quote = null;
        ansi = false;
      } else if (ch === '\\' && (quote === '"' || ansi) && i + 1 < text.length) {
        const next = text[++i] as string;
        if (ansi) cur += next === 'n' ? '\n' : next === 't' ? '\t' : next === 'r' ? '\r' : next;
        else if (next === '"' || next === '\\' || next === '$' || next === '`') cur += next;
        else cur += '\\' + next;
      } else {
        cur += ch;
      }
      continue;
    }
    if (ch === '$' && text[i + 1] === "'") {
      quote = "'";
      ansi = true;
      has = true;
      i++;
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      has = true;
      continue;
    }
    if (/\s/.test(ch)) {
      if (has) {
        out.push(cur);
        cur = '';
        has = false;
      }
      continue;
    }
    cur += ch;
    has = true;
  }
  if (has) out.push(cur);
  return out;
}

const VALUE_FLAGS = new Set(['-H', '--header', '-b', '--cookie', '-u', '--user', '-X', '--request', '-d', '--data', '--data-raw', '--data-binary', '--data-ascii', '--data-urlencode', '--url', '--json', '-e', '--referer', '-A', '--user-agent', '-o', '--output', '-m', '--max-time', '--connect-timeout', '-F', '--form', '-x', '--proxy', '--cacert', '--cert', '--key', '-w', '--write-out', '--resolve', '--retry']);

export function parseCurl(input: string): ParsedCurl | null {
  const tokens = tokenize(input.trim());
  if (tokens.length === 0) return null;
  const start = tokens[0] === 'curl' ? 1 : 0;
  const out: ParsedCurl = { url: '', method: '', headers: [], cookies: [], basic: '', body: '' };
  let sawBody = false;
  for (let i = start; i < tokens.length; i++) {
    const t = tokens[i] as string;
    let flag = t;
    let inline: string | null = null;
    if (t.startsWith('--') && t.includes('=')) {
      flag = t.slice(0, t.indexOf('='));
      inline = t.slice(t.indexOf('=') + 1);
    } else if (/^-[HbuXdeAmoFx]./.test(t) && !t.startsWith('--')) {
      flag = t.slice(0, 2);
      inline = t.slice(2);
    }
    const value = (): string => (inline !== null ? inline : (tokens[++i] ?? ''));
    if (VALUE_FLAGS.has(flag)) {
      const v = value();
      switch (flag) {
        case '-H':
        case '--header': {
          const idx = v.indexOf(':');
          if (idx > 0) out.headers.push({ name: v.slice(0, idx).trim(), value: v.slice(idx + 1).trim() });
          break;
        }
        case '-b':
        case '--cookie':
          for (const part of v.split(';')) {
            const eq = part.indexOf('=');
            if (eq > 0) out.cookies.push({ name: part.slice(0, eq).trim(), value: part.slice(eq + 1).trim() });
          }
          break;
        case '-u':
        case '--user':
          out.basic = v;
          break;
        case '-X':
        case '--request':
          out.method = v.toUpperCase();
          break;
        case '--url':
          out.url = v;
          break;
        case '-d':
        case '--data':
        case '--data-raw':
        case '--data-binary':
        case '--data-ascii':
        case '--json':
          out.body = out.body ? `${out.body}&${v}` : v;
          sawBody = true;
          break;
        default:
          break;
      }
      continue;
    }
    if (t.startsWith('-')) continue;
    if (!out.url) out.url = t;
  }
  if (!out.method) out.method = sawBody ? 'POST' : 'GET';
  const cookieHeader = out.headers.findIndex((h) => h.name.toLowerCase() === 'cookie');
  if (cookieHeader >= 0) {
    for (const part of (out.headers[cookieHeader] as { value: string }).value.split(';')) {
      const eq = part.indexOf('=');
      if (eq > 0) out.cookies.push({ name: part.slice(0, eq).trim(), value: part.slice(eq + 1).trim() });
    }
    out.headers.splice(cookieHeader, 1);
  }
  return out.url || out.headers.length > 0 || out.cookies.length > 0 || out.basic ? out : null;
}

const NOISE = /^(accept|accept-.*|cache-control|connection|content-length|content-type|dnt|host|origin|pragma|priority|referer|sec-.*|te|upgrade-insecure-requests|user-agent|x-requested-with|if-none-match|if-modified-since)$/i;
const SECRETISH = /(key|token|secret|auth|session|password|passwd|credential|signature)/i;

function slug(s: string): string {
  const v = s.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '');
  return v || 'value';
}

function unique(name: string, taken: Set<string>): string {
  let out = name;
  let i = 2;
  while (taken.has(out.toLowerCase())) out = `${name}_${i++}`;
  taken.add(out.toLowerCase());
  return out;
}

function field(name: string, kind: CredentialKind, target: string, value: string, secret: boolean): CredentialField {
  return { name, kind, target, secret, value, hasValue: value !== '', keep: false, expiresAt: 0, updatedAt: 0 };
}

export function curlToFields(p: ParsedCurl): CredentialField[] {
  const taken = new Set<string>();
  const fields: CredentialField[] = [];
  for (const h of p.headers) {
    const lower = h.name.toLowerCase();
    if (lower === 'authorization') {
      const m = h.value.match(/^(\w+)\s+(.+)$/);
      if (m && (m[1] as string).toLowerCase() === 'bearer') {
        fields.push(field(unique('token', taken), 'bearer', '', (m[2] as string).trim(), true));
        continue;
      }
      if (m && (m[1] as string).toLowerCase() === 'basic') {
        let decoded = '';
        try {
          decoded = atob((m[2] as string).trim());
        } catch {
          decoded = '';
        }
        if (decoded.includes(':')) {
          fields.push(field(unique('login', taken), 'basic', '', decoded, true));
          continue;
        }
      }
      fields.push(field(unique('authorization', taken), 'header', 'Authorization', h.value, true));
      continue;
    }
    if (NOISE.test(h.name)) continue;
    fields.push(field(unique(slug(h.name), taken), 'header', h.name, h.value, SECRETISH.test(h.name)));
  }
  if (p.basic) fields.push(field(unique('login', taken), 'basic', '', p.basic, true));
  const seen = new Set<string>();
  for (const c of p.cookies) {
    if (seen.has(c.name)) continue;
    seen.add(c.name);
    fields.push(field(unique(slug(c.name), taken), 'cookie', c.name, c.value, true));
  }
  if (p.url) {
    try {
      const u = new URL(p.url);
      for (const [k, v] of u.searchParams) {
        if (SECRETISH.test(k)) fields.push(field(unique(slug(k), taken), 'query', k, v, true));
      }
    } catch {
      return fields;
    }
  }
  return fields;
}
