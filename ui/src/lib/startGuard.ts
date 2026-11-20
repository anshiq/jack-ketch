import { ProcessService } from './api';
import { dialogs } from './state/dialogs.svelte';

export function duplicateOwner(err: unknown): string | null {
  const msg = err instanceof Error ? err.message : String(err ?? '');
  const m = /already running in (project .+?) with the same command/.exec(msg);
  return m?.[1] ?? null;
}

export async function startGuarded(req: Record<string, unknown>): Promise<{ processId: string; instanceId: string } | null> {
  try {
    return await ProcessService.start(req);
  } catch (err) {
    const owner = duplicateOwner(err);
    if (!owner) throw err;
    const ok = await dialogs.confirm(`The same command is already running in ${owner}. Starting it again can fight over ports and files.`, {
      title: 'Already running in another project',
      confirmLabel: 'Start anyway',
      danger: true
    });
    if (!ok) return null;
    return ProcessService.start({ ...req, force: true });
  }
}
