<script lang="ts">
  import { onMount } from 'svelte';
  import CloudOff from '@lucide/svelte/icons/cloud-off';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import { connection } from '../lib/state/connection.svelte';

  let remaining = $state(10);
  let checking = $state(false);

  async function retry() {
    if (checking) return;
    checking = true;
    remaining = 10;
    await connection.check();
    checking = false;
  }

  onMount(() => {
    const t = setInterval(() => {
      if (checking) return;
      remaining -= 1;
      if (remaining <= 0) remaining = 10;
      if (remaining === 10) void retry();
    }, 1000);
    return () => clearInterval(t);
  });
</script>

{#if connection.state !== 'online'}
  <div class="banner" class:offline={connection.state === 'offline'} role="status" aria-live="polite">
    <CloudOff size={14} />
    <span class="text">
      {#if connection.state === 'offline'}
        Offline. Changes are paused; your drafts are safe. Retrying in {remaining}s.
      {:else}
        Reconnecting to the daemon… your view stays available.
      {/if}
    </span>
    <button class="btn sm" type="button" onclick={() => void retry()} disabled={checking}>
      <RefreshCw size={13} /> {checking ? 'Checking…' : 'Retry now'}
    </button>
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 6px var(--space-4);
    border-bottom: 1px solid color-mix(in srgb, var(--warning, #d9a441) 40%, var(--border));
    background: color-mix(in srgb, var(--warning, #d9a441) 12%, var(--bg-1));
    color: var(--text-1);
    font-size: var(--fs-sm);
    flex: none;
  }
  .banner.offline {
    border-bottom-color: color-mix(in srgb, var(--err) 45%, var(--border));
    background: color-mix(in srgb, var(--err) 14%, var(--bg-1));
  }
  .text {
    flex: 1;
    min-width: 0;
  }
</style>
