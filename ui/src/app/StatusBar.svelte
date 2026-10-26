<script lang="ts">
  import { connection } from '../lib/state/connection.svelte';
  import { processes } from '../lib/state/processes.svelte';
  import { scopeState } from '../lib/state/scope.svelte';

  const version = $derived(connection.version ? (connection.version.startsWith('v') ? connection.version : `v${connection.version}`) : '');
</script>

<footer class="statusbar" class:offline={!connection.reachable} aria-live="polite">
  <span class="item">
    <span class="dot" class:ok={connection.reachable} class:warn={!connection.reachable}></span>
    {connection.reachable ? 'Connected' : 'Disconnected'}
  </span>
  <span class="item">
    <span class="dot" class:ok={processes.counts.running > 0}></span>{processes.counts.running} running
  </span>
  <span class="item">
    <span class="dot" class:err={processes.counts.failed > 0}></span>{processes.counts.failed} failed
  </span>
  {#if !scopeState.all && (processes.totals.running !== processes.counts.running || processes.totals.failed !== processes.counts.failed)}
    <button type="button" class="item all" onclick={() => scopeState.select('')} title="Show all workspaces">
      {processes.totals.running} running · {processes.totals.failed} failed in all workspaces
    </button>
  {/if}
  <span class="spacer"></span>
  <span class="item scope mono" title={scopeState.path}>{scopeState.all ? 'All workspaces' : scopeState.path}</span>
  {#if version}<span class="item mono">{version}</span>{/if}
</footer>

<style>
  .statusbar {
    display: flex;
    gap: 12px;
    align-items: center;
    height: var(--statusbar-h);
    flex: none;
    padding: 0 12px;
    background: var(--bg-1);
    border-top: 1px solid var(--border);
    font-size: var(--fs-micro);
    color: var(--text-2);
  }
  .statusbar.offline {
    background: color-mix(in srgb, var(--err) 12%, var(--bg-1));
  }
  .item {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
    font-family: var(--font-mono);
  }
  .item .dot {
    width: 6px;
    height: 6px;
    box-shadow: none;
  }
  .all {
    height: auto;
    padding: 1px 6px;
    border: none;
    background: transparent;
    color: var(--accent);
    font-size: inherit;
    font-weight: 400;
  }
  .all:hover:not(:disabled) {
    background: var(--bg-hover);
    color: var(--accent-hover);
  }
  .spacer {
    flex: 1;
  }
  .scope {
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 50vw;
  }
</style>
