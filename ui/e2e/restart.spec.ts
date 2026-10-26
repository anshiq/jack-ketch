import { spawn } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
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

async function startProcess(page: Page, info: E2EInfo, marker: string) {
  const ws = await api(page, info, 'ProjectService/Resolve', { path: info.workspace });
  const out = await api(page, info, 'ProcessService/Start', {
    workspaceId: ws.workspaceId,
    command: ['sh', '-c', `echo ${marker}; sleep 120`],
    workDir: info.workspace
  });
  return String(out.processId);
}

async function waitReady(info: E2EInfo) {
  const deadline = Date.now() + 20000;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${baseUrl(info)}/api/jackketch.v1.SystemService/GetVersion`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${info.token}`, 'Content-Type': 'application/json' },
        body: '{}'
      });
      if (res.ok) return;
    } catch {
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error('daemon did not restart');
}

function saveState(info: E2EInfo) {
  const file = process.env.E2E_STATE_FILE;
  if (file) writeFileSync(file, JSON.stringify(info));
}

test('daemon restart keeps drafts and buffers and emits new events once', async ({ page }) => {
  const info = e2eInfo();
  const before = `before-${Date.now()}`;
  const after = `after-${Date.now()}`;
  const first = await startProcess(page, info, before);
  await openWorkspace(page, info);

  await page.getByRole('link', { name: 'Logs', exact: true }).click();
  await expect(page.getByText(before, { exact: true })).toBeVisible({ timeout: 15000 });

  await page.getByRole('link', { name: 'Config', exact: true }).click();
  const editor = page.locator('.monaco-editor').first();
  await expect(editor).toBeVisible({ timeout: 15000 });
  await editor.click();
  await page.keyboard.press('Control+a');
  await page.keyboard.type('apps:\n  restartdraft:\n    command: ["sleep", "5"]\n');

  process.kill(info.pid, 'SIGTERM');
  await expect(page.getByText(/Offline\. Changes are paused|Reconnecting to the daemon/)).toBeVisible({ timeout: 15000 });
  const downDeadline = Date.now() + 15000;
  while (Date.now() < downDeadline) {
    try {
      await fetch(`${baseUrl(info)}/api/jackketch.v1.SystemService/GetVersion`, { method: 'POST' });
    } catch {
      break;
    }
    await new Promise((r) => setTimeout(r, 150));
  }

  const child = spawn(
    info.jackketchd,
    [
      '-foreground',
      '-data',
      `${info.root}/d`,
      '-socket',
      `${info.root}/a.sock`,
      '-tcp',
      `127.0.0.1:${info.port}`,
      '-token-file',
      `${info.root}/token`
    ],
    {
      env: {
        ...process.env,
        PATH: `${info.root}:${process.env.PATH}`,
        XDG_RUNTIME_DIR: `${info.root}/runtime`,
        XDG_CONFIG_HOME: `${info.root}/cfg`,
        XDG_DATA_HOME: `${info.root}/data`,
        XDG_STATE_HOME: `${info.root}/state`
      },
      stdio: 'ignore'
    }
  );
  info.pid = child.pid ?? 0;
  saveState(info);
  await waitReady(info);
  await expect(page.getByText(/Offline\. Changes are paused|Reconnecting to the daemon/)).toHaveCount(0, { timeout: 30000 });

  await page.getByRole('link', { name: 'Logs', exact: true }).click();
  await expect(page.getByText(before, { exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Config', exact: true }).click();
  await expect(page.locator('.view-lines').first()).toContainText('restartdraft');

  const second = await startProcess(page, info, after);
  await page.getByRole('link', { name: 'Events', exact: true }).click();
  await page.getByRole('searchbox', { name: 'Search events' }).fill(after);
  await expect(page.locator('li').filter({ hasText: after })).toHaveCount(1, { timeout: 15000 });

  await api(page, info, 'ProcessService/Remove', { processId: first, force: true }).catch(() => {});
  await api(page, info, 'ProcessService/Remove', { processId: second, force: true }).catch(() => {});
});
