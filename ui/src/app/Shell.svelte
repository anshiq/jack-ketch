<script lang="ts">
  import type { Snippet } from 'svelte';
  import TopBar from './TopBar.svelte';
  import Sidebar from './Sidebar.svelte';
  import StatusBar from './StatusBar.svelte';
  import OfflineBanner from './OfflineBanner.svelte';
  import TopProgress from '../lib/ui/TopProgress.svelte';

  let { children }: { children?: Snippet } = $props();
</script>

<div class="shell">
  <TopBar />
  <OfflineBanner />
  <TopProgress />
  <div class="body">
    <Sidebar />
    <main class="content">
      {#if children}{@render children()}{/if}
    </main>
    <div id="route-announcer" class="sr-only" aria-live="polite"></div>
  </div>
  <StatusBar />
</div>

<style>
  .shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
    background: var(--bg-0);
  }
  .body {
    flex: 1;
    display: flex;
    min-height: 0;
  }
  .content {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    position: relative;
  }
</style>
