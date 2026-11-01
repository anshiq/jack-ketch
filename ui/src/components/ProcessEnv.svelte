<script lang="ts">
  import Eye from '@lucide/svelte/icons/eye';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import Search from '@lucide/svelte/icons/search';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import { ProcessService } from '../lib/api';
  import { query } from '../lib/data/query.svelte';
  import { getPlatform } from '../lib/platform';
  import Switch from '../lib/ui/Switch.svelte';
  import { splitEnvLine } from './processView';

  let { processId }: { processId: string } = $props();

  let revealed = $state(false);
  let configuredOnly = $state(false);
  let filter = $state('');
  let copied = $state('');
  const envQuery = query(
    () => (processId ? `env:${processId}:${revealed}` : ''),
    () => ProcessService.getEnv(processId, revealed, false),
    { staleMs: 30000, persist: true }
  );
  const env = $derived((envQuery.data?.env as string[] | undefined) ?? []);
  const redacted = $derived(envQuery.data?.redacted ?? 0);
  const sources = $derived((envQuery.data?.source as Record<string, string> | undefined) ?? {});
  const BASE_LAYERS = new Set(['shell', 'parent']);
  const configuredCount = $derived(Object.values(sources).filter((v) => !BASE_LAYERS.has(v)).length);

  function sourceText(layer: string): string {
    if (!layer) return '';
    if (layer === 'shell') return 'login shell';
    if (layer === 'parent') return 'daemon env';
    if (layer === 'request.env') return 'start request';
    if (layer === 'request.env_file') return 'request env file';
    if (layer === 'app.env') return 'app env';
    return layer.replace(/^file:/, '').replace(/^mode:([^/]+)\/file:/, '$1 › ').replace(/^mode:([^/]+)\//, '$1 › ');
  }
  const loading = $derived(envQuery.data === undefined && (envQuery.status === 'loading' || envQuery.fetching));

  const entries = $derived(
    env
      .map(splitEnvLine)
      .sort((a, b) => a.key.localeCompare(b.key))
      .filter((e) => {
        if (configuredOnly && BASE_LAYERS.has(sources[e.key] ?? 'shell')) return false;
        const q = filter.trim().toLowerCase();
        return !q || e.key.toLowerCase().includes(q) || e.value.toLowerCase().includes(q) || (sources[e.key] ?? '').toLowerCase().includes(q);
      })
  );

  async function copy(text: string, key: string) {
    try {
      await getPlatform().copyText(text);
      copied = key;
      setTimeout(() => {
        if (copied === key) copied = '';
      }, 1200);
    } catch {
      copied = '';
    }
  }
</script>

<div class="env">
  <div class="bar">
    <div class="input-wrap grow">
      <Search size={14} />
      <input placeholder="Filter variables…" bind:value={filter} aria-label="Filter environment variables" />
    </div>
    {#if configuredCount > 0}
      <div class="reveal">
        <Switch bind:checked={configuredOnly} label="Only configured variables" />
        <span class="lbl">From config ({configuredCount})</span>
      </div>
    {/if}
    <div class="reveal">
      <Switch bind:checked={revealed} label="Reveal secrets" />
      <span class="lbl">{#if revealed}<Eye size={14} />{:else}<EyeOff size={14} />{/if} Reveal secrets</span>
    </div>
  </div>
  <div class="list">
    {#if loading && env.length === 0}
      <div class="state muted">Loading environment…</div>
    {:else if entries.length === 0}
      <div class="state">
        <KeyRound size={20} />
        <span class="muted">{env.length === 0 ? 'No environment variables.' : 'No variables match the filter.'}</span>
      </div>
    {:else}
      <ul>
        {#each entries as e (e.key)}
          <li>
            <span class="k mono">{e.key}</span>
            <span class="s" class:cfg={!!sources[e.key] && !BASE_LAYERS.has(sources[e.key] ?? '')} title={sources[e.key] ? `Winning source: ${sources[e.key]}` : ''}>{sourceText(sources[e.key] ?? '')}</span>
            <span class="v mono" title={e.value}>{e.value || ' '}</span>
            <button type="button" class="copy" aria-label="Copy {e.key}" title="Copy value" onclick={() => void copy(e.value, e.key)}>
              {#if copied === e.key}<Check size={12} />{:else}<Copy size={12} />{/if}
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
  <div class="foot">
    <span>{env.length} variables</span>
    {#if !revealed && redacted > 0}<span>{redacted} values hidden</span>{/if}
  </div>
</div>

<style>
  .env {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-3) var(--space-5);
    border-bottom: 1px solid var(--border);
  }
  .grow {
    flex: 1;
    min-width: 0;
  }
  .grow input {
    padding-top: 4px;
    padding-bottom: 4px;
    font-size: var(--fs-xs);
  }
  .reveal {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    white-space: nowrap;
  }
  .lbl {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-xs);
    color: var(--text-1);
  }
  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    display: grid;
    grid-template-columns: minmax(96px, 32%) minmax(60px, 18%) minmax(0, 1fr) 24px;
    align-items: center;
    gap: var(--space-4);
    padding: 6px var(--space-5);
    border-bottom: 1px solid var(--border);
    font-size: var(--fs-xs);
  }
  li:hover {
    background: var(--bg-hover);
  }
  .k {
    color: var(--accent);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
  }
  .s {
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11px;
  }
  .s.cfg {
    color: var(--info);
  }
  .v {
    color: var(--text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .copy {
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-2);
    border-radius: 5px;
    opacity: 0;
  }
  li:hover .copy,
  .copy:focus-visible {
    opacity: 1;
  }
  .copy:hover {
    background: var(--bg-3);
    color: var(--text-0);
  }
  .state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    height: 100%;
    min-height: 160px;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .foot {
    display: flex;
    justify-content: space-between;
    padding: 5px var(--space-5);
    border-top: 1px solid var(--border);
    font-size: var(--fs-xs);
    color: var(--text-2);
    min-height: 26px;
  }
</style>
