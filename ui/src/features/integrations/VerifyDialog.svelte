<script lang="ts">
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CircleX from '@lucide/svelte/icons/circle-x';
  import type { VerifyCheck } from '../../lib/api/types';
  import Banner from '../../lib/ui/Banner.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';

  let {
    title,
    checks,
    loading,
    error,
    onrerun,
    onclose
  }: { title: string; checks: VerifyCheck[]; loading: boolean; error: string; onrerun: () => void; onclose: () => void } = $props();

  const failed = $derived(checks.filter((c) => !c.ok).length);
</script>

<Dialog {title} width={560} onClose={onclose}>
  <div class="body">
    {#if error}
      <Banner tone="err" title="Verification failed to run" message={error} />
    {:else if loading && checks.length === 0}
      <div class="skeletons">
        {#each Array(3) as _, i (i)}
          <Skeleton width="100%" height="40px" />
        {/each}
      </div>
    {:else if checks.length === 0}
      <p class="none">No checks were produced.</p>
    {:else}
      {#if !loading}
        <p class="summary" class:bad={failed > 0}>
          {failed === 0 ? 'All checks passed.' : `${failed} of ${checks.length} checks failed.`}
        </p>
      {/if}
      <ul class="checks">
        {#each checks as check, i (check.name + i)}
          <li class:bad={!check.ok}>
            <span class="ico" class:ok={check.ok}>
              {#if check.ok}<CircleCheck size={15} />{:else}<CircleX size={15} />{/if}
            </span>
            <div class="text">
              <strong>{check.name}</strong>
              {#if check.detail}<span class="detail">{check.detail}</span>{/if}
              {#if check.path}<span class="path" title={check.path}>{check.path}</span>{/if}
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
  {#snippet footer()}
    <div class="foot">
      <Button variant="ghost" onclick={onclose} disabled={loading}>Close</Button>
      <Button variant="primary" loading={loading} onclick={onrerun}>Run again</Button>
    </div>
  {/snippet}
</Dialog>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    max-height: 60vh;
    overflow: auto;
  }
  .skeletons {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .summary {
    margin: 0;
    color: var(--ok);
    font-size: var(--fs-sm);
    font-weight: 600;
  }
  .summary.bad {
    color: var(--err);
  }
  .none {
    margin: 0;
    color: var(--text-2);
  }
  .checks {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .checks li {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-1);
    min-width: 0;
  }
  .checks li.bad {
    border-color: color-mix(in srgb, var(--err) 45%, var(--border));
  }
  .ico {
    flex: none;
    padding-top: 1px;
    color: var(--err);
  }
  .ico.ok {
    color: var(--ok);
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .text strong {
    font-size: var(--fs-sm);
    color: var(--text-0);
  }
  .detail {
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    color: var(--text-1);
    overflow-wrap: anywhere;
  }
  .path {
    font-size: var(--fs-micro);
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-2);
    width: 100%;
  }
</style>
