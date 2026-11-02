import { describe, expect, it } from 'vitest';
import { curlToFields, parseCurl, tokenize } from './curl';

const chrome = `curl 'http://localhost:3000/api/me?api_key=k-12345&page=2' \\
  -H 'accept: application/json' \\
  -H 'authorization: Bearer eyJhbGciOi.payload.sig' \\
  -H 'x-tenant-id: tenant-a' \\
  -H 'cookie: sid=abc123; theme=dark' \\
  -H 'user-agent: Mozilla/5.0' \\
  -H 'sec-fetch-mode: cors' \\
  --compressed`;

describe('tokenize', () => {
  it('handles quotes, continuations and ANSI-C strings', () => {
    expect(tokenize(`curl -H 'a: b c' "d e" \\\n f`)).toEqual(['curl', '-H', 'a: b c', 'd e', 'f']);
    expect(tokenize(`curl $'it\\'s' x`)).toEqual(['curl', "it's", 'x']);
    expect(tokenize(`a "" b`)).toEqual(['a', '', 'b']);
  });
});

describe('parseCurl', () => {
  it('parses a Chrome copy', () => {
    const p = parseCurl(chrome)!;
    expect(p.url).toContain('/api/me');
    expect(p.method).toBe('GET');
    expect(p.cookies).toEqual([
      { name: 'sid', value: 'abc123' },
      { name: 'theme', value: 'dark' }
    ]);
    expect(p.headers.find((h) => h.name === 'cookie')).toBeUndefined();
  });

  it('infers POST from a body and reads -u, -b and attached flags', () => {
    const p = parseCurl(`curl -u alice:pw -b 'a=1; b=2' -XPUT --data-raw '{"x":1}' --url=http://x/y`)!;
    expect(p.method).toBe('PUT');
    expect(p.basic).toBe('alice:pw');
    expect(p.cookies.map((c) => c.name)).toEqual(['a', 'b']);
    expect(p.body).toBe('{"x":1}');
    expect(p.url).toBe('http://x/y');
    expect(parseCurl(`curl -d x http://h`)!.method).toBe('POST');
  });

  it('returns null for text without a request', () => {
    expect(parseCurl('')).toBeNull();
    expect(parseCurl('curl --compressed')).toBeNull();
  });
});

describe('curlToFields', () => {
  it('maps auth, tenant header, cookies and secret query params, dropping browser noise', () => {
    const fields = curlToFields(parseCurl(chrome)!);
    const by = Object.fromEntries(fields.map((f) => [f.name, f]));
    expect(by['token']).toMatchObject({ kind: 'bearer', secret: true, value: 'eyJhbGciOi.payload.sig' });
    expect(by['x_tenant_id']).toMatchObject({ kind: 'header', target: 'x-tenant-id', secret: false, value: 'tenant-a' });
    expect(by['sid']).toMatchObject({ kind: 'cookie', target: 'sid', secret: true });
    expect(by['theme']).toMatchObject({ kind: 'cookie', target: 'theme' });
    expect(by['api_key']).toMatchObject({ kind: 'query', target: 'api_key', value: 'k-12345', secret: true });
    expect(fields.some((f) => /accept|user_agent|sec_fetch/.test(f.name))).toBe(false);
  });

  it('decodes basic auth and keeps unknown schemes as a header', () => {
    const basic = curlToFields(parseCurl(`curl http://h -H 'Authorization: Basic ${btoa('bob:s3cret')}'`)!);
    expect(basic[0]).toMatchObject({ kind: 'basic', value: 'bob:s3cret', secret: true });
    const other = curlToFields(parseCurl(`curl http://h -H 'Authorization: Token abc'`)!);
    expect(other[0]).toMatchObject({ kind: 'header', target: 'Authorization', value: 'Token abc' });
  });

  it('keeps names unique', () => {
    const fields = curlToFields(parseCurl(`curl http://h -H 'X-Id: 1' -H 'x-id: 2'`)!);
    expect(new Set(fields.map((f) => f.name)).size).toBe(fields.length);
  });
});
