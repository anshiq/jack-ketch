import { describe, expect, it } from 'vitest';
import type { CredentialProfile } from './api/types';
import { draftToWire, profileToDraft } from './api/credentials';
import { looksSecret, appliedAs, emptyDraft, emptyRecipe, expiryText, headersToText, isProduction, newField, parseApps, sendAsOptions, statusLabel, statusTone, textToHeaders, validateDraft } from './credentialsView';

function profile(over: Partial<CredentialProfile> = {}): CredentialProfile {
  return {
    id: 'cred_1',
    name: 'admin',
    description: '',
    environment: 'test',
    default: true,
    apps: ['api'],
    fields: [
      { name: 'token', kind: 'bearer', target: '', secret: true, value: '', hasValue: true, keep: false, expiresAt: 0, updatedAt: 0 },
      { name: 'tenant', kind: 'header', target: 'X-Tenant-Id', secret: false, value: 'tenant-a', hasValue: true, keep: false, expiresAt: 0, updatedAt: 0 }
    ],
    recipe: null,
    cookies: [],
    captureCookies: true,
    status: 'ok',
    expiresAt: 0,
    createdAt: 0,
    updatedAt: 0,
    lastUsedAt: 0,
    lastRefreshAt: 0,
    lastError: '',
    ...over
  };
}

describe('validateDraft', () => {
  it('accepts the starter draft once named', () => {
    const d = emptyDraft();
    expect(validateDraft(d).ok).toBe(false);
    d.name = 'admin';
    expect(validateDraft(d).ok).toBe(true);
  });

  it('flags bad names, duplicates, missing targets and malformed basic auth', () => {
    const d = emptyDraft();
    d.name = 'bad name';
    d.fields = [
      { ...newField('header'), name: 'tenant', target: 'bad header!' },
      { ...newField('cookie'), name: 'TENANT', target: '' },
      { ...newField('basic'), name: 'login', value: 'nocolon' },
      { ...newField('query'), name: 'bad name' }
    ];
    const v = validateDraft(d);
    expect(v.name).not.toBe('');
    expect(v.fields[0]).toMatch(/header/i);
    expect(v.fields[1]).toMatch(/duplicate/i);
    expect(v.fields[2]).toMatch(/user:password/);
    expect(v.fields[3]).toMatch(/Name/);
    expect(v.ok).toBe(false);
  });

  it('validates the login recipe', () => {
    const d = emptyDraft();
    d.name = 'login';
    d.recipe = emptyRecipe();
    expect(validateDraft(d).ok).toBe(true);
    d.recipe.url = 'login';
    expect(validateDraft(d).recipe).toMatch(/path/);
    d.recipe.url = '/login';
    d.recipe.extract = [{ source: 'regex', path: '(', field: 'token', kind: 'bearer', target: '', expiresInPath: '' }];
    expect(validateDraft(d).extract[0]).toMatch(/regular/);
    d.recipe.extract = [];
    d.captureCookies = false;
    expect(validateDraft(d).recipe).toMatch(/extract/);
    d.captureCookies = true;
    expect(validateDraft(d).ok).toBe(true);
  });
});

describe('profile to draft and wire', () => {
  it('keeps stored secrets when the value is left empty', () => {
    const draft = profileToDraft(profile());
    expect(draft.fields[0]).toMatchObject({ value: '', keep: true });
    expect(draft.fields[1]).toMatchObject({ value: 'tenant-a', keep: false });
    const wire = draftToWire(draft) as { fields: { name: string; keep: boolean; value: string; secret: boolean }[] };
    expect(wire.fields[0]).toMatchObject({ name: 'token', keep: true, value: '', secret: true });
    draft.fields[0]!.value = 'new-token';
    const next = draftToWire(draft) as { fields: { keep: boolean; value: string }[] };
    expect(next.fields[0]).toMatchObject({ keep: false, value: 'new-token' });
  });

  it('sends recipes in the snake_case wire shape', () => {
    const d = emptyDraft();
    d.name = 'login';
    d.recipe = emptyRecipe();
    const wire = draftToWire(d) as { recipe: { extract: { expires_in_path?: string; source: string }[]; url: string } };
    expect(wire.recipe.url).toBe('/auth/login');
    expect(wire.recipe.extract[0]).toMatchObject({ source: 'json', expires_in_path: 'expires_in' });
  });
});

describe('looksSecret', () => {
  it('keeps credential-like fields secret', () => {
    expect(looksSecret({ kind: 'bearer', name: 'x', target: '' })).toBe(true);
    expect(looksSecret({ kind: 'cookie', name: 'theme', target: 'theme' })).toBe(true);
    expect(looksSecret({ kind: 'header', name: 'gateway', target: 'X-Api-Key' })).toBe(true);
    expect(looksSecret({ kind: 'variable', name: 'password', target: '' })).toBe(true);
    expect(looksSecret({ kind: 'header', name: 'tenant', target: 'X-Tenant-Id' })).toBe(false);
  });
});

describe('labels and helpers', () => {
  it('describes how a field is applied', () => {
    expect(appliedAs({ kind: 'bearer', target: '', name: 't' })).toBe('Authorization: Bearer');
    expect(appliedAs({ kind: 'cookie', target: 'sid', name: 's' })).toBe('Cookie sid');
    expect(appliedAs({ kind: 'variable', target: '', name: 'pw' })).toBe('${cred:pw}');
  });

  it('maps statuses', () => {
    expect(statusTone('ok')).toBe('ok');
    expect(statusTone('expiring')).toBe('warn');
    expect(statusTone('empty')).toBe('err');
    expect(statusLabel('empty', true)).toBe('Needs login');
    expect(statusLabel('expired', true)).toMatch(/refreshes/);
  });

  it('formats expiry relative to now', () => {
    const now = Date.UTC(2026, 0, 1, 12, 0, 0);
    expect(expiryText(0, now)).toBe('');
    expect(expiryText(now / 1000 + 30, now)).toBe('expires in 30s');
    expect(expiryText(now / 1000 + 3600, now)).toBe('expires in 60m');
    expect(expiryText(now / 1000 - 7200, now)).toBe('expired 2h ago');
  });

  it('round trips headers and app lists', () => {
    const h = textToHeaders('X-A: 1\nbad line\nContent-Type: application/json');
    expect(h).toEqual({ 'X-A': '1', 'Content-Type': 'application/json' });
    expect(textToHeaders(headersToText(h))).toEqual(h);
    expect(parseApps('api, web  api')).toEqual(['api', 'web']);
  });

  it('builds the send-as options', () => {
    expect(sendAsOptions([])).toEqual([{ value: '', label: 'No credentials' }]);
    const opts = sendAsOptions([profile(), profile({ id: 'b', name: 'viewer', default: false, environment: '' })]);
    expect(opts[0]).toEqual({ value: '', label: 'Default (admin)' });
    expect(opts.map((o) => o.value)).toEqual(['', 'admin', 'viewer', 'none']);
    expect(isProduction('Production')).toBe(true);
    expect(isProduction('staging')).toBe(false);
  });
});
