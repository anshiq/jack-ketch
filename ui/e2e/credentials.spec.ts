import { test, expect, type Page } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import path from 'node:path';
import { baseUrl, e2eInfo, loginUrl } from './state';

const SERVER = `
import http from 'node:http';
const port = Number(process.env.PORT);
http.createServer((req, res) => {
  console.log('request handled method=' + req.method + ' path=' + req.url + ' auth=' + req.headers.authorization);
  res.setHeader('Content-Type', 'application/json');
  res.end(JSON.stringify({ path: req.url, auth: req.headers.authorization || null, tenant: req.headers['x-tenant-id'] || null }));
}).listen(port, '127.0.0.1', () => console.log('listening on http://127.0.0.1:' + port));
`;

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      srv.close(() => (addr && typeof addr === 'object' ? resolve(addr.port) : reject(new Error('no port'))));
    });
  });
}

async function api(page: Page, method: string, data: Record<string, unknown>) {
  const info = e2eInfo();
  const res = await page.request.post(`${baseUrl(info)}/api/jackketch.v1.${method}`, {
    headers: { Authorization: `Bearer ${info.token}`, 'Content-Type': 'application/json' },
    data
  });
  expect(res.ok(), `${method} ${res.status()} ${await res.text()}`).toBeTruthy();
  return res.json();
}

async function openWorkspace(page: Page, dir: string) {
  await page.getByRole('button', { name: 'Workspace scope' }).click();
  await page.locator('input[aria-label="Workspace path"]').fill(dir);
  await page.getByRole('button', { name: 'Open', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Workspace scope' })).toContainText(path.basename(dir), { timeout: 10000 });
}

test('add a credential profile, probe with it, and never see the secret in the output', async ({ page }) => {
  const info = e2eInfo();
  const ws = path.join(info.root, 'creddemo');
  mkdirSync(ws, { recursive: true });
  writeFileSync(path.join(ws, 'package.json'), JSON.stringify({ name: 'creddemo', private: true }));
  writeFileSync(path.join(ws, 'server.mjs'), SERVER);
  const port = await freePort();
  const secret = 'eyJhbGciOiJIUzI1NiJ9.e2e-secret-payload.signature-abc';

  const { workspaceId } = (await api(page, 'ProjectService/Resolve', { path: ws })) as { workspaceId: string };
  const started = (await api(page, 'ProcessService/Start', {
    workspaceId,
    command: ['node', 'server.mjs'],
    workdir: ws,
    env: { PORT: String(port) }
  })) as { processId: string };

  await page.goto(loginUrl(info));
  await openWorkspace(page, ws);
  await page.goto(`${baseUrl(info)}/credentials?ws=${encodeURIComponent(workspaceId)}`);

  await expect(page.getByRole('heading', { name: 'No credential profiles yet' })).toBeVisible({ timeout: 10000 });
  await page.getByRole('button', { name: 'New profile' }).first().click();
  const editor = page.getByRole('dialog', { name: 'New credential profile' });
  await expect(editor).toBeVisible();

  await editor.getByRole('textbox', { name: 'Name', exact: true }).fill('admin-a');
  await editor.getByRole('textbox', { name: 'Value 1' }).fill(secret);
  await editor.getByRole('button', { name: 'Header', exact: true }).click();
  await editor.getByRole('textbox', { name: 'Value 2' }).fill('tenant-a');
  await editor.getByRole('switch', { name: 'Use as the project default' }).click();
  await editor.getByRole('button', { name: 'Add profile' }).click();
  await expect(editor).toBeHidden({ timeout: 10000 });

  const card = page.getByRole('article', { name: 'Credential profile admin-a' });
  await expect(card).toBeVisible();
  await expect(card).toContainText('default');
  await expect(card).toContainText('Ready');
  await expect(card).toContainText('tenant-a');
  await expect(card).not.toContainText(secret);

  const listed = (await api(page, 'CredentialService/List', { workspaceId })) as { profiles: { fields: { name: string; value?: string }[] }[] };
  expect(JSON.stringify(listed)).not.toContain(secret);

  await card.getByRole('button', { name: 'Reveal token' }).click();
  await expect(card).toContainText(secret);
  await card.getByRole('button', { name: 'Hide token' }).click();
  await expect(card).not.toContainText(secret);

  await page.goto(`${baseUrl(info)}/processes/${started.processId}?tab=probe&ws=${encodeURIComponent(workspaceId)}`);
  await expect(page.getByRole('combobox', { name: 'Send as' })).toBeVisible({ timeout: 10000 });
  await expect(page.getByRole('combobox', { name: 'Send as' }).locator('option:checked')).toHaveText('Default (admin-a)');
  await page.getByRole('textbox', { name: 'Path or URL' }).fill('/me');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.locator('.probe .status.ok')).toHaveText('200', { timeout: 10000 });
  await expect(page.locator('pre.body')).toContainText('[credential:admin-a.token]');
  await expect(page.locator('pre.body')).toContainText('tenant-a');
  await expect(page.locator('pre.logs')).toContainText('path=/me auth=Bearer [credential:admin-a.token]');
  await expect(page.locator('.probe')).not.toContainText(secret);

  await page.getByRole('combobox', { name: 'Send as' }).selectOption('none');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.locator('pre.body')).toContainText('"auth": null', { timeout: 10000 });

  await api(page, 'ProcessService/Stop', { processId: started.processId });
});

test('import a cURL command into a profile and run a proxy that attaches it', async ({ page }) => {
  const info = e2eInfo();
  const ws = path.join(info.root, 'credproxy');
  mkdirSync(ws, { recursive: true });
  writeFileSync(path.join(ws, 'package.json'), JSON.stringify({ name: 'credproxy', private: true }));
  writeFileSync(path.join(ws, 'server.mjs'), SERVER);
  const port = await freePort();

  const { workspaceId } = (await api(page, 'ProjectService/Resolve', { path: ws })) as { workspaceId: string };
  const started = (await api(page, 'ProcessService/Start', {
    workspaceId,
    command: ['node', 'server.mjs'],
    workdir: ws,
    env: { PORT: String(port) }
  })) as { processId: string };

  await page.goto(loginUrl(info));
  await openWorkspace(page, ws);
  await page.goto(`${baseUrl(info)}/credentials?ws=${encodeURIComponent(workspaceId)}`);
  await expect(page.getByRole('heading', { name: 'No credential profiles yet' })).toBeVisible({ timeout: 10000 });

  await page.getByRole('button', { name: 'Import cURL' }).first().click();
  const imp = page.getByRole('dialog', { name: 'Import from cURL' });
  await imp.getByRole('textbox', { name: 'cURL command' }).fill(
    `curl 'http://127.0.0.1:${port}/api/me' -H 'accept: application/json' -H 'authorization: Bearer curl-token-0123456789' -H 'x-tenant-id: tenant-c' -H 'user-agent: Mozilla'`
  );
  await expect(imp.getByRole('table', { name: 'Detected fields' })).toContainText('token');
  await expect(imp.getByRole('table', { name: 'Detected fields' })).toContainText('x_tenant_id');
  await expect(imp.getByRole('table', { name: 'Detected fields' })).not.toContainText('user_agent');
  await imp.getByRole('button', { name: 'Continue' }).click();

  const editor = page.getByRole('dialog', { name: 'New credential profile' });
  await expect(editor.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue('127.0.0.1');
  await editor.getByRole('textbox', { name: 'Name', exact: true }).fill('from-curl');
  await editor.getByRole('button', { name: 'Add profile' }).click();
  await expect(editor).toBeHidden({ timeout: 10000 });

  const card = page.getByRole('article', { name: 'Credential profile from-curl' });
  await expect(card).toBeVisible();
  await expect(card).toContainText('tenant-c');

  await page.getByRole('button', { name: 'Start proxy' }).first().click();
  const dlg = page.getByRole('dialog', { name: 'Start a credential proxy' });
  await expect(dlg).toBeVisible();
  await dlg.getByRole('button', { name: 'Start proxy' }).click();
  await expect(dlg).toBeHidden({ timeout: 10000 });

  const proxies = page.getByRole('region', { name: 'Credential proxies' });
  const addr = (await proxies.locator('.url').first().innerText()).trim();
  expect(addr).toMatch(/^http:\/\/127\.0\.0\.1:\d+$/);

  const res = await page.request.get(`${addr}/hello`);
  const body = await res.text();
  expect(res.status()).toBe(200);
  expect(body).toContain('tenant-c');
  expect(body).toContain('[credential:from-curl.token]');
  expect(body).not.toContain('curl-token-0123456789');

  await proxies.getByRole('button', { name: 'Stop' }).first().click();
  await expect(proxies.locator('.url')).toHaveCount(0, { timeout: 10000 });

  await api(page, 'ProcessService/Stop', { processId: started.processId });
});

test('revealing a credential through the web API is audited without the value', async ({ page }) => {
  const info = e2eInfo();
  const ws = path.join(info.root, 'credweb');
  mkdirSync(ws, { recursive: true });
  const { workspaceId } = (await api(page, 'ProjectService/Resolve', { path: ws })) as { workspaceId: string };
  const saved = (await api(page, 'CredentialService/Upsert', {
    workspaceId,
    profile: { name: 'only-web', fields: [{ name: 'token', kind: 'bearer', value: 'web-only-secret-111', secret: true }] }
  })) as { id: string };
  const revealed = (await api(page, 'CredentialService/Reveal', { workspaceId, id: saved.id, field: 'token' })) as { value: string };
  expect(revealed.value).toBe('web-only-secret-111');
  const audit = (await api(page, 'AuditService/ListAudit', { workspaceId, limit: 20 })) as { entries: { action: string; args: string }[] };
  const actions = audit.entries.map((e) => e.action);
  expect(actions).toContain('CredentialService/Reveal');
  expect(JSON.stringify(audit)).not.toContain('web-only-secret-111');
});
