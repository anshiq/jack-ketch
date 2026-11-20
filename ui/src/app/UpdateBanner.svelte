<script lang="ts">
  import Download from '@lucide/svelte/icons/download';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import Copy from '@lucide/svelte/icons/copy';
  import X from '@lucide/svelte/icons/x';
  import { connection } from '../lib/state/connection.svelte';
  import { toasts } from '../lib/toasts.svelte';

  const KEY = 'jk.update.dismissed';
  const COMMAND = 'jack-ketch daemon restart';

  function readDismissed(): string {
    try {
      return localStorage.getItem(KEY) ?? '';
    } catch {
      return '';
    }
  }

  let dismissed = $state(readDismissed());
  let expanded = $state(false);

  const update = $derived(connection.update);
  const marker = $derived(update ? `${update.runningVersion}->${update.installedVersion ?? 'unknown'}` : '');
  const visible = $derived(!!update?.pending && dismissed !== marker);
  const count = $derived(update?.runningProcesses ?? 0);
  const processes = $derived(update?.processes ?? []);
  const target = $derived(update?.installedVersion ? `jack-ketch ${update.installedVersion}` : 'A new jack-ketch version');
  const impact = $derived(
    count === 0
      ? 'No managed processes are running, so restarting now is safe.'
      : count === 1
        ? 'Restarting the daemon finishes the update and stops the 1 running process.'
        : `Restarting the daemon finishes the update and stops all ${count} running processes.`
  );

  function dismiss() {
    dismissed = marker;
    try {
      localStorage.setItem(KEY, marker);
    } catch {
      return;
    }
  }

  async function copyCommand() {
    try {
      await navigator.clipboard.writeText(COMMAND);
      toasts.ok('Copied restart command');
    } catch {
      toasts.info(COMMAND);
    }
  }
</script>

{#if visible && update}
  <div class="update" class:busy={count > 0} role="status" aria-live="polite">
    <div class="row">
      <span class="ico"><Download size={14} /></span>
      <div class="text">
        <strong>{target} is installed.</strong>
        <span>The daemon is still running {update.runningVersion}. {impact}</span>
      </div>
      <div class="actions">
        {#if count > 0}
          <button type="button" class="btn sm ghost" aria-expanded={expanded} onclick={() => (expanded = !expanded)}>
            <ChevronDown size={13} class={expanded ? 'flip' : ''} />
            {expanded ? 'Hide' : 'Show'} processes
          </button>
        {/if}
        <button type="button" class="btn sm" onclick={() => void copyCommand()} title="Run this in a terminal when you are ready">
          <Copy size={13} /><code>{COMMAND}</code>
        </button>
        <button type="button" class="x" aria-label="Dismiss until the next update" onclick={dismiss}><X size={14} /></button>
      </div>
    </div>
    {#if expanded && processes.length > 0}
      <ul class="procs">
        {#each processes as p (p.processId)}
          <li>
            <span class="dot" data-status={p.status}></span>
            <span class="name">{p.app || p.command}</span>
            <span class="path" title={p.workspacePath}>{p.workspacePath}</span>
            <span class="id">{p.processId}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}

<style>
  .update {
    --c: var(--info);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: 6px var(--space-4);
    border-bottom: 1px solid color-mix(in srgb, var(--c) 40%, var(--border));
    background: color-mix(in srgb, var(--c) 10%, var(--bg-1));
    color: var(--text-1);
    font-size: var(--fs-sm);
    flex: none;
  }
  .update.busy {
    --c: var(--warn);
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  .ico {
    display: inline-flex;
    color: var(--c);
    flex: none;
  }
  .text {
    flex: 1;
    min-width: 220px;
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    align-items: baseline;
  }
  .text strong {
    color: var(--text-0);
    font-weight: 600;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .actions code {
    font-family: var(--font-mono);
    font-size: var(--fs-xs, 11px);
  }
  .actions :global(.flip) {
    transform: rotate(180deg);
  }
  .x {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    padding: 0;
    border: none;
    background: transparent;
    border-radius: 4px;
    color: var(--text-2);
    cursor: pointer;
  }
  .x:hover {
    background: var(--bg-hover);
    color: var(--text-0);
  }
  .procs {
    list-style: none;
    margin: 0 0 4px 26px;
    padding: 0;
    display: grid;
    gap: 2px;
    max-height: 160px;
    overflow: auto;
  }
  .procs li {
    display: grid;
    grid-template-columns: 8px minmax(80px, max-content) minmax(0, 1fr) max-content;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-xs, 11px);
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--ok);
  }
  .dot[data-status='starting'],
  .dot[data-status='stopping'] {
    background: var(--warn);
  }
  .name {
    color: var(--text-0);
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .path {
    color: var(--text-2);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    direction: rtl;
    text-align: left;
  }
  .id {
    font-family: var(--font-mono);
    color: var(--text-2);
  }
  @media (max-width: 640px) {
    .procs li {
      grid-template-columns: 8px minmax(0, 1fr) max-content;
    }
    .path {
      display: none;
    }
  }
</style>
