<script lang="ts">
  import Save from '@lucide/svelte/icons/save';
  import type { IntegrationChange } from '../../lib/api/types';
  import Badge from '../../lib/ui/Badge.svelte';
  import Banner from '../../lib/ui/Banner.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import DiffView from '../../lib/ui/DiffView.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';

  let {
    title,
    action,
    changes,
    loading,
    error,
    busy,
    onconfirm,
    onclose
  }: {
    title: string;
    action: 'install' | 'remove';
    changes: IntegrationChange[];
    loading: boolean;
    error: string;
    busy: boolean;
    onconfirm: () => void;
    onclose: () => void;
  } = $props();

  const anyChanged = $derived(changes.some((c) => c.changed));
  const backupPath = $derived(changes.find((c) => c.backup)?.backup ?? (changes[0] ? `${changes[0].path}.jack-ketch.bak` : ''));
</script>

<Dialog {title} width={720} flush onClose={onclose}>
  <div class="body">
    {#if loading}
      <div class="skeletons">
        {#each Array(3) as _, i (i)}
          <Skeleton width="180px" height="14px" />
          <Skeleton width="100%" height="72px" />
        {/each}
      </div>
    {:else if error}
      <Banner tone="err" title="Preview failed" message={error} />
    {:else if changes.length === 0}
      <p class="none">Nothing to change.</p>
    {:else}
      {#each changes as change, i (change.path + i)}
        <section class="change">
          <div class="head">
            <code class="path" title={change.path}>{change.path}</code>
            <Badge tone="neutral" size="sm">{change.existed ? 'Edit' : 'Create'}</Badge>
          </div>
          {#if change.note}<p class="note">{change.note}</p>{/if}
          <DiffView diff={change.diff} emptyText="Already up to date" />
        </section>
      {/each}
    {/if}
  </div>
  {#snippet footer()}
    <div class="foot">
      {#if backupPath}
        <span class="backup"><Save size={13} />A backup is saved as <code>{backupPath}</code> before any edit</span>
      {/if}
      <div class="buttons">
        <Button variant="ghost" onclick={onclose} disabled={busy}>Cancel</Button>
        <Button
          variant={action === 'remove' ? 'danger' : 'primary'}
          loading={busy}
          disabled={loading || error !== '' || !anyChanged}
          onclick={onconfirm}>{action === 'remove' ? 'Disconnect' : 'Apply'}</Button>
      </div>
    </div>
  {/snippet}
</Dialog>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    padding: var(--space-5);
    max-height: 56vh;
    overflow: auto;
  }
  .skeletons {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .change {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }
  .path {
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    color: var(--text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .note {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .none {
    margin: 0;
    color: var(--text-2);
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    width: 100%;
  }
  .backup {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-2);
    font-size: var(--fs-xs);
    min-width: 0;
  }
  .backup code {
    font-family: var(--font-mono);
    color: var(--text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .buttons {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: none;
    margin-left: auto;
  }
</style>
