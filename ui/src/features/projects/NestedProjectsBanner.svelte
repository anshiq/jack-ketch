<script lang="ts">
  import { onMount } from 'svelte';
  import GitMerge from '@lucide/svelte/icons/git-merge';
  import { nestedStore, overlapCount } from '../../lib/state/nested.svelte';
  import type { NestedProject } from '../../lib/api';
  import Banner from '../../lib/ui/Banner.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import MergeNestedDialog from './MergeNestedDialog.svelte';

  let { projectId = '' }: { projectId?: string } = $props();

  let target = $state<NestedProject | null>(null);
  const items = $derived(nestedStore.visible(projectId));

  onMount(() => void nestedStore.load(true));
</script>

{#each items as n (n.child.projectId + n.parent.projectId)}
  {@const overlap = overlapCount(n)}
  <Banner tone="warn" title={`${n.child.projectName} lives inside ${n.parent.projectName}`}>
    {overlap === 1
      ? `1 of its ${n.apps.length} apps is defined in both projects, so it appears twice and can be started twice.`
      : overlap > 1
        ? `${overlap} of its ${n.apps.length} apps are defined in both projects, so they appear twice and can be started twice.`
        : `${n.parent.projectName} already defines apps in ${n.child.path}, so the folder is managed twice.`}
    {#snippet actions()}
      <Button size="sm" variant="primary" onclick={() => (target = n)}><GitMerge size={14} />Review &amp; merge</Button>
      <Button size="sm" variant="ghost" onclick={() => nestedStore.dismiss(n)}>Keep separate</Button>
    {/snippet}
  </Banner>
{/each}

{#if target}
  <MergeNestedDialog nested={target} onClose={() => (target = null)} />
{/if}
