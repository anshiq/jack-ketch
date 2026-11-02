import type { CredentialDraft, CredentialField, CredentialKind, CredentialProfile, CredentialRecipe } from './api/types';

export const NAME_RE = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/;
export const HEADER_RE = /^[A-Za-z0-9!#$%&'*+.^_`|~-]+$/;

export const KIND_OPTIONS: { value: CredentialKind; label: string; hint: string }[] = [
  { value: 'bearer', label: 'Bearer token', hint: 'Sent as Authorization: Bearer <value>' },
  { value: 'basic', label: 'Basic auth', hint: 'user:password, sent as Authorization: Basic' },
  { value: 'header', label: 'Header', hint: 'Any request header, for example X-Tenant-Id' },
  { value: 'cookie', label: 'Cookie', hint: 'Added to the Cookie header' },
  { value: 'query', label: 'Query parameter', hint: 'Added to the URL, for example api_key' },
  { value: 'variable', label: 'Variable', hint: 'Only used through a ${cred:name} placeholder' }
];

export const ENVIRONMENTS = ['local', 'dev', 'test', 'staging', 'production'];

export function newField(kind: CredentialKind = 'bearer'): CredentialField {
  const names: Record<CredentialKind, string> = { bearer: 'token', basic: 'login', header: 'tenant', cookie: 'session', query: 'api_key', variable: 'value' };
  const targets: Record<CredentialKind, string> = { bearer: '', basic: '', header: 'X-Tenant-Id', cookie: 'session', query: 'api_key', variable: '' };
  return {
    name: names[kind],
    kind,
    target: targets[kind],
    secret: kind !== 'header' && kind !== 'variable',
    value: '',
    hasValue: false,
    keep: false,
    expiresAt: 0,
    updatedAt: 0
  };
}

export function emptyRecipe(): CredentialRecipe {
  return {
    method: 'POST',
    url: '/auth/login',
    headers: {},
    body: '{"email": "${cred:email}", "password": "${cred:password}"}',
    extract: [{ source: 'json', path: 'access_token', field: 'token', kind: 'bearer', target: '', expiresInPath: 'expires_in' }],
    ttlSeconds: 0
  };
}

export function emptyDraft(): CredentialDraft {
  return { id: '', name: '', description: '', environment: 'test', default: false, apps: [], fields: [newField('bearer')], recipe: null, captureCookies: true };
}

export function usesTarget(kind: CredentialKind): boolean {
  return kind === 'header' || kind === 'cookie' || kind === 'query';
}

export function allowsCustomHeader(kind: CredentialKind): boolean {
  return kind === 'bearer' || kind === 'basic';
}

export const SECRETISH_RE = /(key|token|secret|passw|pwd|auth|session|credential|signature|jwt|bearer|cookie)/i;

export function forcedSecret(kind: CredentialKind): boolean {
  return kind === 'bearer' || kind === 'basic' || kind === 'cookie';
}

export function looksSecret(f: Pick<CredentialField, 'kind' | 'name' | 'target'>): boolean {
  return forcedSecret(f.kind) || SECRETISH_RE.test(f.name) || SECRETISH_RE.test(f.target);
}

export interface DraftIssues {
  name: string;
  fields: string[];
  recipe: string;
  extract: string[];
  ok: boolean;
}

export function validateDraft(d: CredentialDraft): DraftIssues {
  const issues: DraftIssues = { name: '', fields: [], recipe: '', extract: [], ok: true };
  if (!NAME_RE.test(d.name.trim())) issues.name = 'Use 1-64 letters, digits, dot, dash or underscore';
  const seen = new Set<string>();
  for (const f of d.fields) {
    const name = f.name.trim();
    let msg = '';
    if (!NAME_RE.test(name)) msg = 'Name: letters, digits, dot, dash, underscore';
    else if (seen.has(name.toLowerCase())) msg = 'Duplicate field name';
    else if ((f.kind === 'header' || (allowsCustomHeader(f.kind) && f.target.trim() !== '')) && !HEADER_RE.test(f.target.trim())) msg = 'Enter a valid header name';
    else if ((f.kind === 'cookie' || f.kind === 'query') && f.target.trim() === '') msg = f.kind === 'cookie' ? 'Enter the cookie name' : 'Enter the parameter name';
    else if (f.kind === 'basic' && f.value !== '' && !f.value.includes(':')) msg = 'Use user:password';
    seen.add(name.toLowerCase());
    issues.fields.push(msg);
  }
  const r = d.recipe;
  if (r) {
    if (!r.url.trim()) issues.recipe = 'Enter the login URL';
    else if (!/^(\/|https?:\/\/)/.test(r.url.trim())) issues.recipe = 'Use a path such as /auth/login or a full http(s) URL';
    for (const e of r.extract) {
      let msg = '';
      if (!e.path.trim() || !e.field.trim()) msg = 'Path and target field are required';
      else if (!NAME_RE.test(e.field.trim())) msg = 'Target field: letters, digits, dot, dash, underscore';
      else if (e.source === 'regex') {
        try {
          new RegExp(e.path);
        } catch {
          msg = 'Invalid regular expression';
        }
      }
      issues.extract.push(msg);
    }
    if (r.extract.length === 0 && !d.captureCookies) issues.recipe = issues.recipe || 'Add something to extract, or keep cookie capture on';
  }
  issues.ok = !issues.name && !issues.recipe && issues.fields.every((x) => !x) && issues.extract.every((x) => !x);
  return issues;
}

export function appliedAs(f: Pick<CredentialField, 'kind' | 'target' | 'name'>): string {
  switch (f.kind) {
    case 'bearer':
      return `${f.target || 'Authorization'}: Bearer`;
    case 'basic':
      return `${f.target || 'Authorization'}: Basic`;
    case 'header':
      return `Header ${f.target}`;
    case 'cookie':
      return `Cookie ${f.target}`;
    case 'query':
      return `?${f.target}=`;
    default:
      return '${cred:' + f.name + '}';
  }
}

export type Tone = 'ok' | 'warn' | 'err' | 'neutral' | 'info';

export function statusTone(status: string): Tone {
  switch (status) {
    case 'ok':
      return 'ok';
    case 'expiring':
      return 'warn';
    case 'expired':
    case 'empty':
    case 'unreadable':
      return 'err';
    default:
      return 'neutral';
  }
}

export function statusLabel(status: string, hasRecipe = false): string {
  switch (status) {
    case 'ok':
      return 'Ready';
    case 'expiring':
      return 'Expiring';
    case 'expired':
      return hasRecipe ? 'Expired, refreshes on use' : 'Expired';
    case 'empty':
      return hasRecipe ? 'Needs login' : 'Needs a value';
    case 'unreadable':
      return 'Key mismatch';
    default:
      return status;
  }
}

export function expiryText(expiresAtSec: number, nowMs: number): string {
  if (!expiresAtSec) return '';
  const diff = expiresAtSec * 1000 - nowMs;
  const abs = Math.abs(diff);
  const mins = Math.round(abs / 60000);
  const text = abs < 90_000 ? `${Math.max(1, Math.round(abs / 1000))}s` : mins < 90 ? `${mins}m` : abs < 36 * 3_600_000 ? `${Math.round(abs / 3_600_000)}h` : `${Math.round(abs / 86_400_000)}d`;
  return diff >= 0 ? `expires in ${text}` : `expired ${text} ago`;
}

export function isProduction(environment: string): boolean {
  return environment.trim().toLowerCase().startsWith('prod');
}

export function headersToText(h: Record<string, string>): string {
  return Object.entries(h)
    .map(([k, v]) => `${k}: ${v}`)
    .join('\n');
}

export function textToHeaders(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split(/\r?\n/)) {
    const i = line.indexOf(':');
    if (i <= 0) continue;
    const k = line.slice(0, i).trim();
    if (k) out[k] = line.slice(i + 1).trim();
  }
  return out;
}

export function parseApps(text: string): string[] {
  return [...new Set(text.split(/[,\s]+/).map((s) => s.trim()).filter(Boolean))];
}

export function defaultProfile(profiles: CredentialProfile[]): CredentialProfile | undefined {
  return profiles.find((p) => p.default);
}

export function sendAsOptions(profiles: CredentialProfile[]): { value: string; label: string }[] {
  const def = defaultProfile(profiles);
  const out: { value: string; label: string }[] = [{ value: '', label: def ? `Default (${def.name})` : 'No credentials' }];
  for (const p of profiles) out.push({ value: p.name, label: p.environment ? `${p.name} · ${p.environment}` : p.name });
  if (profiles.length > 0) out.push({ value: 'none', label: 'None (anonymous)' });
  return out;
}

export function maskedValue(f: Pick<CredentialField, 'secret' | 'hasValue' | 'value'>): string {
  if (!f.secret) return f.value || '—';
  return f.hasValue ? '••••••••' : 'not set';
}
