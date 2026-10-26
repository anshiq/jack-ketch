import { test, expect } from '@playwright/test';
import { baseUrl, e2eInfo, loginUrl } from './state';

test('auto-login via token fragment lands on the processes page', async ({ page }) => {
  const info = e2eInfo();
  await page.goto(loginUrl(info));
  await expect(page.getByRole('button', { name: 'Start process' })).toBeVisible();
  await expect(page.locator('input[aria-label="Bearer token"]')).toHaveCount(0);
});

test('rejects a bad token and shows the login screen', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('input[aria-label="Bearer token"]')).toBeVisible();
  await page.locator('input[aria-label="Bearer token"]').fill('not-the-real-token');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByText(/unauthorized/i)).toBeVisible();
});

test('start a process from the dialog and see it appear live', async ({ page }) => {
  const info = e2eInfo();
  await page.goto(loginUrl(info));
  await page.getByRole('button', { name: 'Workspace scope' }).click();
  await page.locator('input[aria-label="Workspace path"]').fill(info.workspace);
  await page.getByRole('button', { name: 'Open', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Workspace scope' })).toContainText('ws', { timeout: 10000 });

  await page.getByRole('button', { name: 'Start process' }).click();
  await page.getByRole('button', { name: 'Command', exact: true }).click();
  await page.getByRole('textbox', { name: 'Command' }).fill('sh -c "echo hello-e2e; sleep 30"');
  await page.getByRole('button', { name: 'Start', exact: true }).click();

  await page.getByRole('button', { name: 'Back to processes' }).first().click();
  await expect(page.locator('tr', { hasText: 'echo hello-e2e' })).toBeVisible({ timeout: 10000 });
  await expect(page.getByText('Running 1', { exact: false })).toBeVisible();
});

test('stop the running process and see its status change live', async ({ page }) => {
  const info = e2eInfo();
  const headers = { Authorization: `Bearer ${info.token}`, 'Content-Type': 'application/json' };
  const resolved = await page.request.post(`${baseUrl(info)}/api/jackketch.v1.ProjectService/Resolve`, {
    headers,
    data: { path: info.workspace }
  });
  const { workspaceId } = (await resolved.json()) as { workspaceId: string };
  await page.request.post(`${baseUrl(info)}/api/jackketch.v1.ProcessService/Start`, {
    headers,
    data: { workspaceId, command: ['sh', '-c', 'echo stop-e2e; sleep 60'], workdir: info.workspace }
  });

  await page.goto(loginUrl(info));
  const row = page.locator('tr', { hasText: 'stop-e2e' });
  await expect(row).toBeVisible({ timeout: 10000 });
  await row.locator('button[title="Stop"]').click();
  await expect(row.locator('.dot.off')).toBeVisible({ timeout: 10000 });
});

test('command palette opens with Ctrl+K and navigates', async ({ page }) => {
  const info = e2eInfo();
  await page.goto(loginUrl(info));
  await page.keyboard.press('Control+k');
  const paletteInput = page.getByPlaceholder('Search pages, processes…');
  await expect(paletteInput).toBeVisible();
  await paletteInput.fill('Settings');
  await page.keyboard.press('Enter');
  await expect(page.getByText('Daemon settings')).toBeVisible();
});

test('config edit, plan and apply round-trips through the daemon', async ({ page }) => {
  const info = e2eInfo();
  await page.goto(loginUrl(info));
  await page.getByRole('button', { name: 'Workspace scope' }).click();
  await page.locator('input[aria-label="Workspace path"]').fill(info.workspace);
  await page.getByRole('button', { name: 'Open', exact: true }).click();

  await page.getByRole('link', { name: 'Config', exact: true }).click();
  const editor = page.locator('.monaco-editor').first();
  await expect(editor).toBeVisible({ timeout: 10000 });
  await editor.click();
  await page.keyboard.press('Control+a');
  await page.keyboard.type('apps:\n  e2eapp:\n    command: ["sleep", "5"]\n');

  await page.getByRole('button', { name: 'Preview changes' }).click();
  await expect(page.locator('.changes strong', { hasText: 'e2eapp' })).toBeVisible({ timeout: 10000 });
  await page.getByRole('button', { name: 'Apply' }).first().click();
  await expect(page.getByText('Applied · revision')).toBeVisible({ timeout: 10000 });
});
