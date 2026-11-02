<script lang="ts">
  import { untrack } from 'svelte';
  import Network from '@lucide/svelte/icons/network';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import { CredentialService } from '../../lib/api';
  import type { CredentialProfile, CredentialProxy, Process } from '../../lib/api/types';
  import { processDisplayName } from '../../lib/format';
  import { toastError } from '../../lib/toasts.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';

  let {
    workspaceId,
    profiles,
    processes,
    initialProfile = '',
    onClose,
    onStarted
  }: {
    workspaceId: string;
    profiles: CredentialProfile[];
    processes: Process[];
    initialProfile?: string;
    onClose: () => void;
    onStarted: (p: CredentialProxy) => void;
  } = $props();

  const TTLS = [
    { value: '15', label: '15 minutes' },
    { value: '30', label: '30 minutes' },
    { value: '60', label: '1 hour' },
    { value: '120', label: '2 hours' },
    { value: '240', label: '4 hours' },
    { value: '480', label: '8 hours' }
  ];

  let profile = $state(untrack(() => initialProfile || profiles.find((p) => p.default)?.name || profiles[0]?.name || ''));
  let target = $state<'process' | 'url'>(untrack(() => (processes.length > 0 ? 'process' : 'url')));
  let processId = $state(untrack(() => processes[0]?.id ?? ''));
  let port = $state('');
  let upstream = $state('http://localhost:3000');
  let ttl = $state('30');
  let label = $state('');
  let starting = $state(false);
  let error = $state('');

  const proc = $derived(processes.find((p) => p.id === processId));
  const remote = $derived(target === 'url' && !/^https?:\/\/(localhost|127\.|\[::1\]|[^/]*\.localhost)/i.test(upstream.trim()));
  const valid = $derived(!!profile && (target === 'process' ? !!processId : /^https?:\/\/.+/i.test(upstream.trim())));

  async function start() {
    if (!valid) return;
    starting = true;
    error = '';
    try {
      const got = await CredentialService.startProxy({
        workspaceId,
        profile,
        processId: target === 'process' ? processId : undefined,
        port: target === 'process' && port ? Number(port) : undefined,
        upstream: target === 'url' ? upstream.trim() : undefined,
        ttlMinutes: Number(ttl),
        label: label.trim() || undefined
      });
      onStarted(got);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      toastError(err);
    } finally {
      starting = false;
    }
  }
</script>

<Dialog title="Start a credential proxy" description="A local address that forwards to your app and adds the profile to every request, for curl, Playwright or end-to-end runs." icon={Network} width={560} {onClose}>
  {#snippet children()}
    <form
      class="body"
      onsubmit={(e) => {
        e.preventDefault();
        void start();
      }}
    >
      <div class="form-field">
        <label for="px-profile">Credential profile</label>
        <select id="px-profile" bind:value={profile}>
          {#each profiles as p (p.id)}<option value={p.name}>{p.name}{p.environment ? ` · ${p.environment}` : ''}</option>{/each}
        </select>
      </div>
      <div class="form-field">
        <span class="field-label">Forward to</span>
        <div class="seg" role="group" aria-label="Forward to">
          <button type="button" class:active={target === 'process'} aria-pressed={target === 'process'} disabled={processes.length === 0} onclick={() => (target = 'process')}>A running process</button>
          <button type="button" class:active={target === 'url'} aria-pressed={target === 'url'} onclick={() => (target = 'url')}>A URL</button>
        </div>
        {#if target === 'process'}
          <select bind:value={processId} aria-label="Process">
            {#each processes as p (p.id)}<option value={p.id}>{processDisplayName(p)}{p.mode ? ` · ${p.mode}` : ''}{p.ports.length ? ` · :${p.ports.join(', :')}` : ''}</option>{/each}
          </select>
          {#if proc && proc.ports.length > 1}
            <select bind:value={port} aria-label="Port">
              <option value="">auto</option>
              {#each proc.ports as p (p)}<option value={String(p)}>{p}</option>{/each}
            </select>
          {/if}
        {:else}
          <input class="mono" bind:value={upstream} placeholder="http://localhost:3000" aria-label="Upstream URL" spellcheck="false" />
        {/if}
        {#if remote}
          <div class="banner warn"><TriangleAlert size={14} /><span>This target is not on this machine. The profile will be sent to it.</span></div>
        {/if}
      </div>
      <div class="row2">
        <div class="form-field">
          <label for="px-ttl">Stops after</label>
          <select id="px-ttl" bind:value={ttl}>{#each TTLS as t (t.value)}<option value={t.value}>{t.label}</option>{/each}</select>
        </div>
        <div class="form-field">
          <label for="px-label">Note <span class="opt">optional</span></label>
          <input id="px-label" bind:value={label} placeholder="playwright run" />
        </div>
      </div>
      <p class="hint">The proxy listens on 127.0.0.1 only, keeps Set-Cookie headers inside jack-ketch, scrubs secret values from responses and does not support WebSockets.</p>
      {#if error}<div class="banner err"><TriangleAlert size={14} /><span>{error}</span></div>{/if}
      <button type="submit" class="hidden" tabindex="-1" aria-hidden="true"></button>
    </form>
  {/snippet}
  {#snippet footer()}
    <button type="button" class="btn" onclick={onClose}>Cancel</button>
    <button type="button" class="btn primary" disabled={starting || !valid} onclick={() => void start()}>
      {#if starting}<Spinner size={14} />{/if}Start proxy
    </button>
  {/snippet}
</Dialog>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .row2 {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--space-4);
  }
  .field-label {
    font-size: var(--fs-xs);
    color: var(--text-1);
    font-weight: 500;
  }
  .opt {
    color: var(--text-2);
    font-weight: 400;
  }
  select,
  input.mono,
  #px-label {
    height: 30px;
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    color: var(--text-0);
    padding: 0 8px;
    font-size: var(--fs-sm);
    min-width: 0;
  }
  select:focus,
  input:focus {
    outline: none;
    border-color: var(--accent);
  }
  .hidden {
    display: none;
  }
</style>
