<script lang="ts">
  import { untrack } from 'svelte';
  import GitBranch from '@lucide/svelte/icons/git-branch';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import { AssistService } from '../../lib/api';
  import type { GitignoreStatus } from '../../lib/api/types';
  import { getPlatform } from '../../lib/platform';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';

  let { workspaceId, status, compact = false, onDone }: { workspaceId: string; status: GitignoreStatus; compact?: boolean; onDone?: (added: boolean) => void } = $props();

  let chosen = $state<Set<string>>(untrack(() => new Set(status.missing.map((m) => m.path))));
  let busy = $state<'' | 'add' | 'dismiss'>('');
  let copied = $state(false);

  const KIND: Record<string, string> = { env: 'env file', state: 'jack-ketch state', skill: 'agent skill', integration: 'MCP config' };

  function toggle(path: string) {
    const next = new Set(chosen);
    if (next.has(path)) next.delete(path);
    else next.add(path);
    chosen = next;
  }

  async function add() {
    busy = 'add';
    try {
      const res = await AssistService.updateGitignore(workspaceId, [...chosen], 'add');
      toasts.ok(res.added.length > 0 ? `Added ${res.added.length} entr${res.added.length === 1 ? 'y' : 'ies'} to .gitignore` : '.gitignore already covers these');
      const rest = status.missing.map((m) => m.path).filter((p) => !chosen.has(p));
      if (rest.length > 0) await AssistService.updateGitignore(workspaceId, rest, 'dismiss');
      onDone?.(true);
    } catch (err) {
      toastError(err);
    } finally {
      busy = '';
    }
  }

  async function dismiss() {
    busy = 'dismiss';
    try {
      await AssistService.updateGitignore(workspaceId, status.missing.map((m) => m.path), 'dismiss');
      onDone?.(false);
    } catch (err) {
      toastError(err);
    } finally {
      busy = '';
    }
  }

  const untrackCmd = $derived(status.tracked.length > 0 ? `git rm --cached -r ${status.tracked.join(' ')}` : '');

  async function copyUntrack() {
    try {
      await getPlatform().copyText(untrackCmd);
      copied = true;
      setTimeout(() => (copied = false), 1200);
    } catch {
      copied = false;
    }
  }
</script>

<section class="gi" class:compact aria-label="Keep jack-ketch files out of git">
  <header>
    <GitBranch size={15} />
    <strong>Keep jack-ketch files out of git?</strong>
  </header>
  <p class="lead">These files are created or used by jack-ketch in this repository and git would track them. Pick what to add to <span class="mono">.gitignore</span>.</p>
  <ul>
    {#each status.missing as m (m.path)}
      <li>
        <label>
          <input type="checkbox" checked={chosen.has(m.path)} onchange={() => toggle(m.path)} />
          <span class="mono path">{m.path}</span>
          <span class="kind">{KIND[m.kind] ?? m.kind}</span>
        </label>
        <span class="why">{m.reason}</span>
      </li>
    {/each}
  </ul>
  {#if untrackCmd}
    <div class="tracked">
      <TriangleAlert size={13} />
      <span>Already committed — ignoring won't untrack them. Run</span>
      <code class="mono">{untrackCmd}</code>
      <button type="button" class="icon-btn" aria-label="Copy command" onclick={copyUntrack}>{#if copied}<Check size={12} />{:else}<Copy size={12} />{/if}</button>
    </div>
  {/if}
  <div class="acts">
    <button type="button" class="btn sm ghost" disabled={!!busy} onclick={dismiss}>{#if busy === 'dismiss'}<Spinner size={12} />{/if}Don't ask again</button>
    <button type="button" class="btn sm primary" disabled={!!busy || chosen.size === 0} onclick={add}>{#if busy === 'add'}<Spinner size={12} />{/if}Add to .gitignore</button>
  </div>
</section>

<style>
  .gi {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4) var(--space-5);
    border-radius: var(--radius-lg);
    border: 1px solid color-mix(in srgb, var(--info) 40%, var(--border));
    background: color-mix(in srgb, var(--info) 6%, var(--bg-1));
  }
  .gi.compact {
    padding: var(--space-3) var(--space-4);
  }
  header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    color: var(--info);
  }
  header strong {
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .lead {
    margin: 0;
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  li {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  label {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    cursor: pointer;
    font-size: var(--fs-sm);
  }
  .path {
    color: var(--text-0);
  }
  .kind {
    padding: 0 7px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-2);
    font-size: 11px;
  }
  .why {
    margin-left: 26px;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .tracked {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    color: var(--warn);
    font-size: var(--fs-xs);
  }
  .tracked code {
    padding: 1px 6px;
    border-radius: var(--radius-sm);
    background: var(--bg-0);
    color: var(--text-0);
  }
  .icon-btn {
    display: inline-grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border: none;
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
  }
  .acts {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
</style>
