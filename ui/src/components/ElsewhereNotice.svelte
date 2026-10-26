<script lang="ts">
  import Layers from '@lucide/svelte/icons/layers';
  import Banner from '../lib/ui/Banner.svelte';
  import { processes } from '../lib/state/processes.svelte';
  import { scopeState } from '../lib/state/scope.svelte';
  import { isLive } from './processView';

  let { compact = false }: { compact?: boolean } = $props();

  const elsewhere = $derived.by(() => {
    if (scopeState.all) return [];
    const byWs = new Map<string, number>();
    for (const p of processes.list) {
      if (p.workspaceId === scopeState.workspaceId || !isLive(p)) continue;
      byWs.set(p.workspaceId, (byWs.get(p.workspaceId) ?? 0) + 1);
    }
    return [...byWs.entries()].map(([id, count]) => ({ id, count, label: scopeState.workspaceLabel(id) })).sort((a, b) => b.count - a.count || a.label.localeCompare(b.label));
  });
  const total = $derived(elsewhere.reduce((n, w) => n + w.count, 0));
  const hereLive = $derived(processes.scoped.some(isLive));
  const show = $derived(total > 0 && (!compact || !hereLive));
</script>

{#if show}
  <Banner tone="info" title={hereLive ? `${total} more ${total === 1 ? 'process is' : 'processes are'} running in other workspaces` : `Nothing is running in ${scopeState.label}, but ${total} ${total === 1 ? 'process is' : 'processes are'} running elsewhere`}>
    <span class="where">
      {#each elsewhere.slice(0, 4) as w (w.id)}
        <button type="button" class="ws" onclick={() => scopeState.select(w.id)} title={scopeState.workspacePath(w.id)}>
          {w.label}<span class="n">{w.count}</span>
        </button>
      {/each}
      {#if elsewhere.length > 4}<span class="more">+{elsewhere.length - 4} more</span>{/if}
    </span>
    {#snippet actions()}
      <button type="button" class="btn sm" onclick={() => scopeState.select('')}><Layers size={12} /> Show all workspaces</button>
    {/snippet}
  </Banner>
{/if}

<style>
  .where {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
    margin-top: 4px;
  }
  .ws {
    height: 20px;
    padding: 0 4px 0 8px;
    gap: 6px;
    font-size: var(--fs-xs);
    background: var(--bg-1);
  }
  .n {
    font-family: var(--font-mono);
    font-size: var(--fs-micro);
    padding: 0 5px;
    border-radius: 3px;
    background: color-mix(in srgb, var(--ok) 16%, transparent);
    color: var(--ok);
    line-height: 1.6;
  }
  .more {
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
</style>
