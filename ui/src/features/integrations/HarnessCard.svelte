<script lang="ts">
  import Copy from '@lucide/svelte/icons/copy';
  import type { HarnessInfo, ScopeName } from '../../lib/api/types';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import { getPlatform } from '../../lib/platform';
  import { toasts } from '../../lib/toasts.svelte';
  import { actionLabel, hue, initials, scopeOf, statusLabel, statusTone } from './harness';

  let {
    harness,
    scope,
    busy,
    onconnect,
    onremove,
    onsnippet,
    onverify,
    onskills
  }: {
    harness: HarnessInfo;
    scope: ScopeName;
    busy: boolean;
    onconnect: () => void;
    onremove: () => void;
    onsnippet: () => void;
    onverify: () => void;
    onskills: () => void;
  } = $props();

  const report = $derived(scopeOf(harness, scope));
  const status = $derived(report?.status ?? 'absent');
  const problems = $derived((report?.entries ?? []).flatMap((e) => e.problems));
  const connectedActions = $derived(status === 'installed' || status === 'outdated' || status === 'broken');

  async function copyPath(path: string) {
    try {
      await getPlatform().copyText(path);
      toasts.ok('Path copied');
    } catch {
      toasts.err('Copy failed');
    }
  }
</script>

<article class="hcard">
  <header>
    <span class="avatar" style={`--h: ${hue(harness.id)}`}>{initials(harness.displayName)}</span>
    <div class="head-text">
      <h3>{harness.displayName}</h3>
      <div class="badges">
        {#if harness.detected}
          <Badge tone="ok" dot>Detected{#if harness.version}<span class="ver">· v{harness.version}</span>{/if}</Badge>
        {:else}
          <Badge tone="neutral">Not detected</Badge>
        {/if}
        {#if harness.tier === 2}<Badge tone="info">Manual setup</Badge>{/if}
        {#if harness.connected > 0}<Badge tone="accent">{harness.connected} connected</Badge>{/if}
      </div>
    </div>
  </header>

  {#if harness.tier === 1}
    <div class="status">
      <Badge tone={statusTone(status)} dot>{statusLabel(status)}</Badge>
      {#if report && report.entries.length > 0}
        <ul class="entries">
          {#each report.entries as entry (entry.path)}
            <li>
              <code class="path" title={entry.path}>{entry.path}</code>
              <IconButton label="Copy path" size="sm" onclick={() => void copyPath(entry.path)}><Copy size={13} /></IconButton>
            </li>
          {/each}
        </ul>
      {/if}
      {#if problems.length > 0}
        <ul class="problems">
          {#each problems as problem (problem)}
            <li>{problem}</li>
          {/each}
        </ul>
      {/if}
      {#if report?.note}<p class="note">{report.note}</p>{/if}
      {#if status === 'unavailable' && report?.error}<p class="warn-line">{report.error}</p>{/if}
      {#if harness.maxWaitMs > 0}<p class="micro">Waits capped at {Math.round(harness.maxWaitMs / 1000)}s per call</p>{/if}
    </div>
  {:else if harness.manualSteps}
    <p class="manual">{harness.manualSteps}</p>
  {/if}

  <div class="foot">
    {#if harness.tier === 1}
      <Button variant="primary" size="sm" disabled={status === 'unavailable' || busy} onclick={onconnect}>{actionLabel(status)}</Button>
    {:else}
      <Button variant="primary" size="sm" disabled={busy} onclick={onsnippet}>Show setup</Button>
    {/if}
    <Button variant="ghost" size="sm" disabled={busy} onclick={onsnippet}>Copy config</Button>
    {#if connectedActions}
      <Button variant="ghost" size="sm" disabled={busy} onclick={onverify}>Verify</Button>
    {/if}
    {#if harness.skillsSupported}
      <Button variant="ghost" size="sm" disabled={busy} onclick={onskills}>Skills</Button>
    {/if}
    {#if connectedActions}
      <Button variant="danger" size="sm" disabled={busy} onclick={onremove}>Disconnect</Button>
    {/if}
    {#if harness.docsUrl}
      <a class="docs" href={harness.docsUrl} target="_blank" rel="noreferrer">Docs</a>
    {/if}
  </div>
</article>

<style>
  .hcard {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-5);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-1);
    transition: border-color var(--dur-fast) var(--ease), transform var(--dur-fast) var(--ease);
  }
  .hcard:hover {
    border-color: var(--border-strong);
  }
  header {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    min-width: 0;
  }
  .avatar {
    width: 36px;
    height: 36px;
    border-radius: 10px;
    display: grid;
    place-items: center;
    font-weight: 700;
    color: hsl(var(--h) 62% 46%);
    background: color-mix(in srgb, hsl(var(--h) 70% 50%) 16%, var(--bg-3));
    flex: none;
  }
  .head-text {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: var(--fs-md);
    font-weight: 600;
    color: var(--text-0);
  }
  .badges {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .ver {
    margin-left: 4px;
    font-weight: 400;
  }
  .status {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    min-width: 0;
  }
  .status > :global(*:not(.badge)) {
    align-self: stretch;
  }
  .entries {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .entries li {
    display: flex;
    align-items: center;
    gap: 4px;
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
  .problems {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    color: var(--err);
    font-size: var(--fs-xs);
  }
  .note {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .warn-line {
    margin: 0;
    color: var(--warn);
    font-size: var(--fs-xs);
  }
  .micro {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-micro);
  }
  .manual {
    margin: 0;
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  .foot {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-top: auto;
  }
  .docs {
    margin-left: auto;
    color: var(--text-2);
    font-size: var(--fs-xs);
    text-decoration: none;
  }
  .docs:hover {
    color: var(--accent);
  }
</style>
