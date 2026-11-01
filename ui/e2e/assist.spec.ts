import { test, expect, type Page } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import path from 'node:path';
import { baseUrl, e2eInfo, loginUrl } from './state';

const SERVER = `
import http from 'node:http';
const port = Number(process.env.PORT);
http.createServer((req, res) => {
  console.log('request handled method=' + req.method + ' path=' + req.url + ' greeting=' + process.env.GREETING);
  res.setHeader('Content-Type', 'application/json');
  res.end(JSON.stringify({ path: req.url, greeting: process.env.GREETING }));
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

test('save a working run as a mode, probe it live, and start it again by mode', async ({ page }) => {
  const info = e2eInfo();
  const ws = path.join(info.root, 'assistdemo');
  mkdirSync(ws, { recursive: true });
  writeFileSync(path.join(ws, 'package.json'), JSON.stringify({ name: 'assistdemo', private: true }));
  writeFileSync(path.join(ws, 'server.mjs'), SERVER);
  writeFileSync(path.join(ws, '.env.local'), 'GREETING=from-file\n');
  const port = await freePort();

  const { workspaceId } = (await api(page, 'ProjectService/Resolve', { path: ws })) as { workspaceId: string };
  const started = (await api(page, 'ProcessService/Start', {
    workspaceId,
    command: ['node', 'server.mjs'],
    workdir: ws,
    env: { PORT: String(port) },
    envFile: '.env.local'
  })) as { processId: string };

  await page.goto(loginUrl(info));
  await openWorkspace(page, ws);
  await page.goto(`${baseUrl(info)}/processes/${started.processId}?tab=probe&ws=${encodeURIComponent(workspaceId)}`);

  const banner = page.getByRole('status').filter({ hasText: 'This run is working.' });
  await expect(banner).toBeVisible({ timeout: 15000 });
  await expect(banner).toContainText('assistdemo · dev');

  await page.getByRole('textbox', { name: 'Path or URL' }).fill('/hello');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.locator('.probe .status.ok')).toHaveText('200', { timeout: 10000 });
  await expect(page.locator('pre.logs')).toContainText('path=/hello greeting=from-file');
  await expect(page.locator('pre.body')).toContainText('"greeting": "from-file"');

  await banner.getByRole('button', { name: 'Save as mode…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Save this run as an app mode' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('combobox', { name: 'App' })).toHaveValue('assistdemo');
  await expect(dialog.getByRole('textbox', { name: 'Mode' })).toHaveValue('dev');
  await dialog.getByRole('textbox', { name: 'Mode' }).fill('local');
  await expect(dialog.getByRole('textbox', { name: 'Env file path' })).toHaveValue('.env.local');
  await dialog.getByRole('button', { name: 'Save mode' }).click();
  await expect(dialog).toBeHidden({ timeout: 10000 });
  await expect(banner).toBeHidden();

  const cfg = (await api(page, 'ConfigService/GetConfig', { workspaceId })) as { raw: { project: string } };
  expect(cfg.raw.project).toContain('local:');
  expect(cfg.raw.project).toContain('file: .env.local');
  expect(cfg.raw.project).toContain('default_mode: local');

  await page.getByRole('button', { name: 'Save as check' }).click();
  await page.getByRole('textbox', { name: 'Check name' }).fill('hello');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Check name' })).toBeHidden({ timeout: 10000 });
  const withCheck = (await api(page, 'ConfigService/GetConfig', { workspaceId })) as { raw: { project: string } };
  expect(withCheck.raw.project).toContain('name: hello');
  expect(withCheck.raw.project).toContain('url: /hello');

  await api(page, 'ProcessService/Stop', { processId: started.processId });

  await page.goto(`${baseUrl(info)}/apps?ws=${encodeURIComponent(workspaceId)}`);
  const card = page.getByRole('article', { name: 'App assistdemo' });
  await expect(card).toBeVisible({ timeout: 10000 });
  await expect(card.getByRole('radio', { name: /local/ })).toHaveAttribute('aria-checked', 'true');
  await expect(card.locator('.envchain')).toContainText('.env.local');
  await card.getByRole('button', { name: 'Start assistdemo' }).click();
  await expect(card.locator('.status.ok')).toBeVisible({ timeout: 15000 });
});

test('a failing run explains what is missing and who has to act', async ({ page }) => {
  const info = e2eInfo();
  const ws = path.join(info.root, 'brokendemo');
  mkdirSync(ws, { recursive: true });
  writeFileSync(path.join(ws, 'package.json'), JSON.stringify({ name: 'brokendemo', dependencies: { express: '^4.0.0' } }));
  writeFileSync(path.join(ws, '.env.example'), '# Stripe secret from the dashboard\nSTRIPE_KEY=sk_test_123\n');
  writeFileSync(path.join(ws, 'boom.mjs'), "console.error(\"Error: Cannot find module 'express'\"); process.exit(1);\n");
  writeFileSync(path.join(ws, 'env.mjs'), "console.error('Error: Missing required environment variable: STRIPE_KEY'); process.exit(1);\n");

  const { workspaceId } = (await api(page, 'ProjectService/Resolve', { path: ws })) as { workspaceId: string };
  const dep = (await api(page, 'ProcessService/Start', { workspaceId, command: ['node', 'boom.mjs'], workdir: ws })) as { processId: string };
  const env = (await api(page, 'ProcessService/Start', { workspaceId, command: ['node', 'env.mjs'], workdir: ws })) as { processId: string };

  await page.goto(loginUrl(info));
  await openWorkspace(page, ws);

  await page.goto(`${baseUrl(info)}/processes/${dep.processId}?tab=diagnose&ws=${encodeURIComponent(workspaceId)}`);
  await expect(page.getByText('Fixable with your approval')).toBeVisible({ timeout: 15000 });
  await expect(page.locator('.fix code').first()).toHaveText('npm install');
  await expect(page.getByText('needs approval').first()).toBeVisible();

  await page.goto(`${baseUrl(info)}/processes/${env.processId}?tab=diagnose&ws=${encodeURIComponent(workspaceId)}`);
  await expect(page.getByText('You need to act')).toBeVisible({ timeout: 15000 });
  const row = page.locator('tr', { hasText: 'STRIPE_KEY' });
  await expect(row).toContainText('Stripe secret from the dashboard');
  await expect(row).toContainText('sk_test_123');
  await expect(page.getByRole('alert').filter({ hasText: 'STRIPE_KEY' })).toBeVisible();
});
