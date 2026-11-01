import { call } from './transport';
import type {
  CredentialCookie,
  CredentialDraft,
  CredentialExtract,
  CredentialField,
  CredentialKind,
  CredentialProfile,
  CredentialProxy,
  CredentialRecipe,
  ExtractSource,
  KeystoreStatus
} from './types';

type Rec = Record<string, unknown>;

function rec(v: unknown): Rec {
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as Rec) : {};
}

function s(v: unknown, d = ''): string {
  return typeof v === 'string' ? v : d;
}

function n(v: unknown, d = 0): number {
  return typeof v === 'number' && Number.isFinite(v) ? v : d;
}

function list(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}

function pick(o: Rec, ...keys: string[]): unknown {
  for (const k of keys) if (o[k] !== undefined && o[k] !== null) return o[k];
  return undefined;
}

const KINDS: CredentialKind[] = ['bearer', 'basic', 'header', 'cookie', 'query', 'variable'];
const SOURCES: ExtractSource[] = ['json', 'header', 'cookie', 'regex'];

export function toCredentialField(raw: unknown): CredentialField {
  const o = rec(raw);
  const kind = s(o['kind']) as CredentialKind;
  return {
    name: s(o['name']),
    kind: KINDS.includes(kind) ? kind : 'variable',
    target: s(o['target']),
    secret: o['secret'] === true,
    value: s(o['value']),
    hasValue: pick(o, 'has_value', 'hasValue') === true,
    keep: false,
    expiresAt: n(pick(o, 'expires_at', 'expiresAt')),
    updatedAt: n(pick(o, 'updated_at', 'updatedAt'))
  };
}

function toExtract(raw: unknown): CredentialExtract {
  const o = rec(raw);
  const source = s(o['source']) as ExtractSource;
  const kind = s(o['kind']) as CredentialKind;
  return {
    source: SOURCES.includes(source) ? source : 'json',
    path: s(o['path']),
    field: s(o['field']),
    kind: KINDS.includes(kind) ? kind : 'bearer',
    target: s(o['target']),
    expiresInPath: s(pick(o, 'expires_in_path', 'expiresInPath'))
  };
}

function toRecipe(raw: unknown): CredentialRecipe | null {
  const o = rec(raw);
  const url = s(o['url']);
  if (!url) return null;
  const headers = rec(o['headers']);
  return {
    method: s(o['method'], 'POST'),
    url,
    headers: Object.fromEntries(Object.entries(headers).map(([k, v]) => [k, String(v)])),
    body: s(o['body']),
    extract: list(o['extract']).map(toExtract),
    ttlSeconds: n(pick(o, 'ttl_seconds', 'ttlSeconds'))
  };
}

function toCookie(raw: unknown): CredentialCookie {
  const o = rec(raw);
  return { name: s(o['name']), host: s(o['host']), path: s(o['path']), expires: n(o['expires']), secure: o['secure'] === true };
}

export function toCredentialProfile(raw: unknown): CredentialProfile {
  const o = rec(raw);
  return {
    id: s(o['id']),
    name: s(o['name']),
    description: s(o['description']),
    environment: s(o['environment']),
    default: o['default'] === true,
    apps: list(o['apps']).filter((x): x is string => typeof x === 'string'),
    fields: list(o['fields']).map(toCredentialField),
    recipe: toRecipe(o['recipe']),
    cookies: list(o['cookies']).map(toCookie),
    captureCookies: pick(o, 'capture_cookies', 'captureCookies') !== false,
    status: s(o['status'], 'ok'),
    expiresAt: n(pick(o, 'expires_at', 'expiresAt')),
    createdAt: n(pick(o, 'created_at', 'createdAt')),
    updatedAt: n(pick(o, 'updated_at', 'updatedAt')),
    lastUsedAt: n(pick(o, 'last_used_at', 'lastUsedAt')),
    lastRefreshAt: n(pick(o, 'last_refresh_at', 'lastRefreshAt')),
    lastError: s(pick(o, 'last_error', 'lastError'))
  };
}

export function toCredentialProxy(raw: unknown): CredentialProxy {
  const o = rec(raw);
  return {
    id: s(o['id']),
    profile: s(o['profile']),
    upstream: s(o['upstream']),
    url: s(o['url']),
    label: s(o['label']),
    createdAt: n(pick(o, 'created_at', 'createdAt')),
    expiresAt: n(pick(o, 'expires_at', 'expiresAt')),
    requests: n(o['requests']),
    refreshes: n(o['refreshes']),
    lastStatus: n(pick(o, 'last_status', 'lastStatus'))
  };
}

function toKeystore(raw: unknown): KeystoreStatus {
  const o = rec(raw);
  return {
    backend: s(o['backend'], 'auto'),
    active: s(o['active'], 'none'),
    keychainAvailable: pick(o, 'keychain_available', 'keychainAvailable') === true,
    keychainTool: s(pick(o, 'keychain_tool', 'keychainTool')),
    keychainError: s(pick(o, 'keychain_error', 'keychainError')),
    keyPath: s(pick(o, 'key_path', 'keyPath'))
  };
}

export function draftToWire(d: CredentialDraft): Rec {
  const r = d.recipe;
  return {
    id: d.id || undefined,
    name: d.name.trim(),
    description: d.description.trim(),
    environment: d.environment.trim(),
    default: d.default,
    apps: d.apps,
    capture_cookies: d.captureCookies,
    fields: d.fields.map((f) => ({
      name: f.name.trim(),
      kind: f.kind,
      target: f.target.trim(),
      secret: f.secret || f.kind === 'bearer' || f.kind === 'basic' || f.kind === 'cookie',
      value: f.value,
      keep: f.keep && f.value === ''
    })),
    recipe: r
      ? {
          method: r.method,
          url: r.url.trim(),
          headers: r.headers,
          body: r.body,
          ttl_seconds: r.ttlSeconds || undefined,
          extract: r.extract.map((e) => ({
            source: e.source,
            path: e.path.trim(),
            field: e.field.trim(),
            kind: e.kind,
            target: e.target.trim(),
            expires_in_path: e.expiresInPath.trim() || undefined
          }))
        }
      : undefined
  };
}

export function profileToDraft(p: CredentialProfile): CredentialDraft {
  return {
    id: p.id,
    name: p.name,
    description: p.description,
    environment: p.environment,
    default: p.default,
    apps: [...p.apps],
    captureCookies: p.captureCookies,
    fields: p.fields.map((f) => ({ ...f, value: f.secret ? '' : f.value, keep: f.secret && f.hasValue })),
    recipe: p.recipe ? { ...p.recipe, headers: { ...p.recipe.headers }, extract: p.recipe.extract.map((e) => ({ ...e })) } : null
  };
}

export const CredentialService = {
  list: async (workspaceId: string): Promise<{ profiles: CredentialProfile[]; hint: string }> => {
    const r = await call<Rec, Rec>('CredentialService', 'List', { workspaceId });
    return { profiles: list(r['profiles']).map(toCredentialProfile), hint: s(r['hint']) };
  },
  upsert: async (workspaceId: string, draft: CredentialDraft): Promise<CredentialProfile> =>
    toCredentialProfile(await call<Rec, Rec>('CredentialService', 'Upsert', { workspaceId, profile: draftToWire(draft) })),
  remove: (workspaceId: string, id: string) => call<Rec, Rec>('CredentialService', 'Delete', { workspaceId, id }),
  reveal: async (workspaceId: string, id: string, field: string): Promise<string> => {
    const r = await call<Rec, Rec>('CredentialService', 'Reveal', { workspaceId, id, field });
    return s(r['value']);
  },
  clearJar: (workspaceId: string, id: string) => call<Rec, Rec>('CredentialService', 'ClearJar', { workspaceId, id }),
  refresh: async (req: { workspaceId: string; id: string; processId?: string; upstream?: string }): Promise<CredentialProfile> =>
    toCredentialProfile(await call<Rec, Rec>('CredentialService', 'Refresh', req, { timeoutMs: 40000 })),
  proxies: async (workspaceId: string): Promise<CredentialProxy[]> => {
    const r = await call<Rec, Rec>('CredentialService', 'ListProxies', { workspaceId });
    return list(r['proxies']).map(toCredentialProxy);
  },
  startProxy: async (req: { workspaceId: string; profile: string; processId?: string; upstream?: string; port?: number; ttlMinutes?: number; label?: string }): Promise<CredentialProxy> => {
    const r = await call<Rec, Rec>('CredentialService', 'StartProxy', {
      workspaceId: req.workspaceId,
      profile: req.profile,
      process_id: req.processId,
      upstream: req.upstream,
      port: req.port,
      ttl_minutes: req.ttlMinutes,
      label: req.label,
      confirmed: true
    });
    return toCredentialProxy(r);
  },
  stopProxy: (workspaceId: string, id: string) => call<Rec, Rec>('CredentialService', 'StopProxy', { workspaceId, id }),
  keystore: async (): Promise<KeystoreStatus> => toKeystore(await call<Rec, Rec>('CredentialService', 'GetKeystore', {})),
  setKeystore: async (backend: string): Promise<KeystoreStatus> => toKeystore(await call<Rec, Rec>('CredentialService', 'SetKeystore', { backend }, { timeoutMs: 40000 })),
  resetKey: async (): Promise<number> => {
    const r = await call<Rec, Rec>('CredentialService', 'ResetKey', { confirm: 'reset' });
    return n(r['profilesCleared']);
  }
};
