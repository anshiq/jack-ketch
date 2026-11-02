<script lang="ts">
  import { untrack } from 'svelte';
  import Send from '@lucide/svelte/icons/send';
  import { AssistService } from '../../lib/api';
  import type { CredentialProfile, ProbeResult, Process } from '../../lib/api/types';
  import { processDisplayName } from '../../lib/format';
  import { toastError } from '../../lib/toasts.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import { prettyBody } from '../assist/assistView';

  let { profile, processes, onResult }: { profile: CredentialProfile; processes: Process[]; onResult?: () => void } = $props();

  const METHODS = ['GET', 'HEAD', 'OPTIONS', 'POST', 'PUT', 'PATCH', 'DELETE'];
  let processId = $state(untrack(() => processes[0]?.id ?? ''));
  let method = $state('GET');
  let url = $state('/');
  let sending = $state(false);
  let result = $state<ProbeResult | null>(null);

  $effect(() => {
    if (!processes.some((p) => p.id === processId)) processId = processes[0]?.id ?? '';
  });

  const pretty = $derived(result ? prettyBody(result.body) : { text: '', json: false });

  async function run(e?: Event) {
    e?.preventDefault();
    if (!processId) return;
    sending = true;
    try {
      result = await AssistService.probe({ processId, method, url: url.trim() || '/', credential: profile.name, confirmed: true });
      onResult?.();
    } catch (err) {
      toastError(err);
    } finally {
      sending = false;
    }
  }
</script>

<div class="test">
  {#if processes.length === 0}
    <p class="muted small">Start a process in this project to send a test request with this profile.</p>
  {:else}
    <form class="line" onsubmit={run}>
      <select bind:value={processId} aria-label="Process to test against">
        {#each processes as p (p.id)}<option value={p.id}>{processDisplayName(p)}{p.mode ? ` · ${p.mode}` : ''}</option>{/each}
      </select>
      <select bind:value={method} aria-label="Method">{#each METHODS as m (m)}<option value={m}>{m}</option>{/each}</select>
      <input class="mono grow" bind:value={url} placeholder="/api/me" aria-label="Path or URL" spellcheck="false" />
      <button type="submit" class="btn primary sm" disabled={sending}>{#if sending}<Spinner size={12} />{:else}<Send size={12} />{/if}Send as {profile.name}</button>
    </form>
  {/if}
  {#if result}
    <div class="out">
      <div class="sum">
        {#if result.status}<span class="status" class:ok={result.status < 400} class:bad={result.status >= 400}>{result.status}</span>{/if}
        <span class="mono truncate">{result.method} {result.url || result.tcp}</span>
        <span class="muted">{result.durationMs} ms</span>
        {#if result.credentialRefreshed}<span class="badge info">token refreshed</span>{/if}
        {#if result.credentialStatus === 'rejected'}<span class="badge err">rejected</span>{/if}
      </div>
      {#if result.credentialHint}<p class="hint warn">{result.credentialHint}</p>{/if}
      {#if result.error}<p class="hint warn">{result.error}</p>{/if}
      <pre class="body mono">{pretty.text || '(empty body)'}</pre>
      <p class="muted small">Secret values are replaced with <span class="mono">[credential:name]</span> in this output.</p>
    </div>
  {/if}
</div>

<style>
  .test {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .line {
    display: flex;
    gap: var(--space-2);
    align-items: center;
    flex-wrap: wrap;
  }
  select,
  input {
    height: 28px;
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    color: var(--text-0);
    padding: 0 8px;
    font-size: var(--fs-xs);
    min-width: 0;
  }
  select:focus,
  input:focus {
    outline: none;
    border-color: var(--accent);
  }
  .grow {
    flex: 1;
    min-width: 140px;
  }
  .out {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .sum {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--fs-xs);
    min-width: 0;
  }
  .status {
    padding: 1px 8px;
    border-radius: 999px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .status.ok {
    color: var(--ok);
    background: color-mix(in srgb, var(--ok) 14%, transparent);
  }
  .status.bad {
    color: var(--err);
    background: color-mix(in srgb, var(--err) 14%, transparent);
  }
  .body {
    margin: 0;
    padding: var(--space-3);
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    font-size: var(--fs-xs);
    color: var(--text-1);
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 220px;
    overflow: auto;
  }
  .hint.warn {
    color: var(--warn);
    margin: 0;
    font-size: var(--fs-xs);
  }
  .muted {
    color: var(--text-2);
  }
  .small {
    font-size: var(--fs-xs);
    margin: 0;
  }
</style>
