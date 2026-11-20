<script lang="ts">
  import { onMount } from 'svelte';
  import GitMerge from '@lucide/svelte/icons/git-merge';
  import { ProjectService } from '../../lib/api';
  import type { MergeReport, NestedAppStatus, NestedProject } from '../../lib/api';
  import { projectsStore } from '../../lib/state/projects.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { nestedStore } from '../../lib/state/nested.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Banner from '../../lib/ui/Banner.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';

  let { nested, onClose }: { nested: NestedProject; onClose: () => void } = $props();

  let report = $state<MergeReport | null>(null);
  let error = $state('');
  let busy = $state(false);

  const blocked = $derived((report?.running ?? nested.running) > 0);

  const tones: Record<NestedAppStatus, 'ok' | 'warn' | 'info' | 'accent'> = {
    duplicate: 'ok',
    conflict: 'warn',
    unique: 'info',
    added: 'info'
  };

  const verbs: Record<NestedAppStatus, string> = {
    duplicate: 'already in parent, extras merged',
    conflict: 'same app, different command: parent kept',
    unique: 'copied into parent',
    added: 'copied into parent'
  };

  onMount(async () => {
    try {
      report = await ProjectService.mergeNested(nested.child.projectId, nested.parent.projectId, true);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  });

  async function merge() {
    if (busy || blocked) return;
    busy = true;
    try {
      const wasChild = scopeState.workspaceId === nested.child.workspaceId;
      const res = await ProjectService.mergeNested(nested.child.projectId, nested.parent.projectId, false);
      await projectsStore.load(true);
      if (wasChild) scopeState.select(nested.parent.workspaceId);
      else await scopeState.refresh();
      await nestedStore.load(true);
      toasts.ok(`Merged ${nested.child.projectName} into ${nested.parent.projectName} (config revision ${res.revision})`);
      onClose();
    } catch (err) {
      toastError(err);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog
  title={`Merge ${nested.child.projectName} into ${nested.parent.projectName}`}
  description="One config per codebase: the nested project's apps move into the project that already encloses them."
  icon={GitMerge}
  width={620}
  {onClose}
>
  {#if error}
    <Banner tone="err" message={error} />
  {:else if !report}
    <div class="loading"><Spinner size={16} /> Comparing configs…</div>
  {:else}
    <div class="paths">
      <div><span class="k">Keeps</span><code class="mono">{nested.parent.path}</code></div>
      <div><span class="k">Merges</span><code class="mono">{nested.child.path}</code></div>
    </div>
    <ul class="apps">
      {#each report.apps as a (a.name)}
        <li>
          <Badge tone={tones[a.status]} size="sm">{a.status}</Badge>
          <span class="name mono">{a.name}</span>
          <span class="why">{verbs[a.status]}{a.parentApp && a.parentApp !== a.name ? ` (${a.parentApp})` : ''}</span>
          {#if a.detail}<span class="detail">{a.detail}</span>{/if}
        </li>
      {:else}
        <li class="muted">The nested project defines no apps.</li>
      {/each}
    </ul>
    <p class="note">
      After the merge {nested.child.projectName} moves to the 30-day trash and {nested.child.path} resolves to {nested.parent.projectName}.
      {#if report.credentials > 0}{report.credentials} credential profile{report.credentials === 1 ? '' : 's'} move to the parent; a profile whose name already exists there stays behind.{/if}
      Config history, logs and process history of the nested project are kept in the trash.
    </p>
    {#if blocked}
      <Banner tone="warn" message={`Stop the ${report.running} running process${report.running === 1 ? '' : 'es'} in ${nested.child.projectName} first.`} />
    {/if}
  {/if}
  {#snippet footer()}
    <Button variant="secondary" onclick={onClose}>Cancel</Button>
    <Button variant="primary" loading={busy} disabled={!report || blocked} onclick={() => void merge()}>
      <GitMerge size={14} />Merge into {nested.parent.projectName}
    </Button>
  {/snippet}
</Dialog>

<style>
  .loading {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-2);
    padding: var(--space-4) 0;
  }
  .paths {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: var(--space-4);
    font-size: var(--fs-sm);
  }
  .paths div {
    display: flex;
    gap: var(--space-3);
    align-items: baseline;
  }
  .k {
    width: 52px;
    flex: none;
    color: var(--text-2);
  }
  code {
    overflow-wrap: anywhere;
  }
  .apps {
    list-style: none;
    margin: 0 0 var(--space-4);
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .apps li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: 6px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-1);
  }
  .name {
    font-weight: 600;
  }
  .why {
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .detail {
    flex-basis: 100%;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .note {
    margin: 0 0 var(--space-3);
    color: var(--text-2);
    font-size: var(--fs-sm);
    line-height: 1.5;
  }
  .muted {
    color: var(--text-2);
  }
</style>
