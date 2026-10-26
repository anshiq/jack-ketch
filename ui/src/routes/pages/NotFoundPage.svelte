<script lang="ts">
  import Compass from '@lucide/svelte/icons/compass';
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';
  import LayoutDashboard from '@lucide/svelte/icons/layout-dashboard';
  import { router } from '../../lib/router.svelte';

  const path = $derived(router.path.split('?')[0] ?? '/');
</script>

<div class="page">
  <div class="empty-state notfound">
    <span class="icon-wrap"><Compass size={20} /></span>
    <h1>Page not found</h1>
    <p>There is nothing at <code class="mono">{path}</code>. It may have moved, or the link is out of date.</p>
    <div class="row">
      <button type="button" class="btn" onclick={() => history.back()}><ArrowLeft size={14} /> Go back</button>
      <a class="btn primary" href="/" use:router.link={'/'}><LayoutDashboard size={14} /> Open overview</a>
    </div>
  </div>
</div>

<style>
  .notfound {
    flex: 1;
  }
  .notfound h1 {
    font-size: var(--fs-lg);
    color: var(--text-0);
  }
  code {
    padding: 1px 5px;
    border-radius: 3px;
    background: var(--bg-3);
    color: var(--text-0);
    font-size: var(--fs-xs);
  }
  .row {
    display: flex;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }
  .row a:hover {
    text-decoration: none;
  }
</style>
