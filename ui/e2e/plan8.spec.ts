import { test, expect, type Page } from '@playwright/test';
import { baseUrl, e2eInfo, loginUrl, type E2EInfo } from './state';

async function openWorkspace(page: Page, info: E2EInfo) {
  await page.goto(loginUrl(info));
  await page.getByRole('button', { name: 'Workspace scope' }).click();
  await page.locator('input[aria-label="Workspace path"]').fill(info.workspace);
  await page.getByRole('button', { name: 'Open', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Workspace scope' })).toContainText('ws', { timeout: 10000 });
}

async function api(page: Page, info: E2EInfo, method: string, data: Record<string, unknown>) {
  const res = await page.request.post(`${baseUrl(info)}/api/jackketch.v1.${method}`, {
    headers: { Authorization: `Bearer ${info.token}`, 'Content-Type': 'application/json' },
    data
  });
  expect(res.ok()).toBeTruthy();
  return (await res.json()) as Record<string, unknown>;
}

async function workspaceId(page: Page, info: E2EInfo) {
  const out = await api(page, info, 'ProjectService/Resolve', { path: info.workspace });
  return String(out.workspaceId);
}

async function startProcess(page: Page, info: E2EInfo, marker: string) {
  const ws = await workspaceId(page, info);
  const out = await api(page, info, 'ProcessService/Start', {
    workspaceId: ws,
    command: ['sh', '-c', `echo ${marker}; sleep 120`],
    workDir: info.workspace
  });
  return String(out.processId);
}

async function stats(page: Page, info: E2EInfo) {
  return api(page, info, 'SystemService/GetStats', {});
}

test('revisiting Logs and Config keeps the buffer and draft', async ({ page }) => {
  const info = e2eInfo();
  const marker = `keepalive-${Date.now()}`;
  const id = await startProcess(page, info, marker);
  await openWorkspace(page, info);

  await page.getByRole('link', { name: 'Logs', exact: true }).click();
  await expect(page.getByText(marker, { exact: true })).toBeVisible({ timeout: 15000 });

  await page.getByRole('link', { name: 'Config', exact: true }).click();
  const editor = page.locator('.monaco-editor').first();
  await expect(editor).toBeVisible({ timeout: 15000 });
  await editor.click();
  await page.keyboard.press('Control+a');
  await page.keyboard.type('apps:\n  draftapp:\n    command: ["sleep", "5"]\n');

  await page.getByRole('link', { name: 'Logs', exact: true }).click();
  await expect(page.getByText(marker, { exact: true })).toBeVisible();

  await page.getByRole('link', { name: 'Config', exact: true }).click();
  await expect(page.locator('.view-lines').first()).toContainText('draftapp', { timeout: 10000 });

  await api(page, info, 'ProcessService/Remove', { processId: id, force: true });
});

test('visibility churn keeps one stream and restart responds quickly', async ({ page }) => {
  const info = e2eInfo();
  const id = await startProcess(page, info, `stream-${Date.now()}`);
  await openWorkspace(page, info);

  await expect.poll(async () => {
    const out = await stats(page, info);
    return Number((out['streams'] as Record<string, unknown>)['total'] ?? 0);
  }, { timeout: 10000 }).toBe(1);

  for (let i = 0; i < 30; i++) {
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  }

  const started = Date.now();
  await api(page, info, 'ProcessService/Restart', { processId: id });
  expect(Date.now() - started).toBeLessThan(300);

  const total = ((await stats(page, info))['streams'] as Record<string, unknown>)['total'];
  expect(total).toBe(1);
  await api(page, info, 'ProcessService/Remove', { processId: id, force: true });
});

test('restart flips the row before the RPC resolves and does not move it', async ({ page }) => {
  const info = e2eInfo();
  const id = await startProcess(page, info, `optimistic-${Date.now()}`);
  await openWorkspace(page, info);

  const row = page.locator('tr', { hasText: 'optimistic-' });
  await expect(row).toBeVisible({ timeout: 10000 });
  const before = await row.boundingBox();

  await page.route('**/jackketch.v1.ProcessService/Restart', async (route) => {
    await new Promise((r) => setTimeout(r, 900));
    await route.continue();
  });

  await row.locator('button[title="Restart"]').click();
  await expect(row.getByText('restarting…')).toBeVisible({ timeout: 500 });
  const during = await row.boundingBox();
  expect(during?.y).toBe(before?.y);

  await expect(row.getByText('running')).toBeVisible({ timeout: 10000 });
  await api(page, info, 'ProcessService/Remove', { processId: id, force: true });
});

test('back and forward restore scope, filters and detail tab', async ({ page }) => {
  const info = e2eInfo();
  const id = await startProcess(page, info, `history-${Date.now()}`);
  await openWorkspace(page, info);

  const filter = page.getByRole('textbox', { name: 'Filter processes' });
  await filter.fill('nothing-matches');
  await expect(page).toHaveURL(/q=nothing-matches/);

  await page.getByRole('link', { name: 'Logs', exact: true }).click();
  await expect(page).toHaveURL(/\/logs/);
  await page.goBack();
  await expect(filter).toHaveValue('nothing-matches');
  await expect(page.getByRole('button', { name: 'Workspace scope' })).toContainText('ws');

  await filter.fill('');
  await page.goto(`${baseUrl(info)}/processes/${id}?tab=info&ws=${encodeURIComponent(await workspaceId(page, info))}`);
  await expect(page.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true');
  await page.getByRole('button', { name: 'Back to processes' }).first().click();
  await page.goBack();
  await expect(page.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true');

  await api(page, info, 'ProcessService/Remove', { processId: id, force: true });
});

test('two tabs share one daemon stream', async ({ page }) => {
  const info = e2eInfo();
  const second = await page.context().newPage();
  await page.goto(loginUrl(info));
  await second.goto(loginUrl(info));
  await expect(page.getByRole('button', { name: 'Start process' })).toBeVisible();
  await expect(second.getByRole('button', { name: 'Start process' })).toBeVisible();

  await expect.poll(async () => {
    const out = await stats(page, info);
    return ((out['streams'] as Record<string, unknown>)['total'] as number) ?? 0;
  }, { timeout: 10000 }).toBe(1);

  await second.close();
});

test('first load does not request Monaco or xterm chunks', async ({ page }) => {
  const info = e2eInfo();
  const requests: string[] = [];
  page.on('request', (r) => requests.push(new URL(r.url()).pathname));
  await page.goto(loginUrl(info));
  await expect(page.getByRole('button', { name: 'Start process' })).toBeVisible();
  await page.waitForTimeout(1200);
  expect(requests.filter((p) => /ConfigEditor|monaco|xterm|Terminal-/i.test(p))).toEqual([]);
});
