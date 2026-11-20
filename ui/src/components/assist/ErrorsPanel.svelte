<script lang="ts">
  import Bug from '@lucide/svelte/icons/bug';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import FileCode from '@lucide/svelte/icons/file-code';
  import Globe from '@lucide/svelte/icons/globe';
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import CircleStop from '@lucide/svelte/icons/circle-stop';
  import Radio from '@lucide/svelte/icons/radio';
  import { ErrorsService, BrowserProxyService, type ErrorGroup, type ErrorsResult, type BrowserProxy } from '../../lib/api';
  import { getPlatform } from '../../lib/platform';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import SegmentedControl from '../../lib/ui/SegmentedControl.svelte';
  import RelativeTime from '../../lib/ui/RelativeTime.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import { kindInfo, errorsDigest, locationLabel } from './errorsView';

  let {
    processId,
    workspaceId = '',
    live = false,
    instanceKey = ''
  }: { processId: string; workspaceId?: string; live?: boolean; instanceKey?: string } = $props();

  let since = $state('instance');
  let result = $state<ErrorsResult | null>(null);
  let loading = $state(false);
  let error = $state('');
  let open = $state<Record<string, boolean>>({});
  let copied = $state('');
  let proxy = $state<BrowserProxy | null>(null);
  let proxyBusy = $state(false);
  let warnings = $state(false);
  let inflight = false;

  async function load(quiet = false) {
    if (inflight) return;
    inflight = true;
    if (!quiet) loading = true;
    try {
      result = await ErrorsService.get(processId, since);
      error = '';
    } catch (err) {
      if (!quiet) error = err instanceof Error ? err.message : String(err);
    } finally {
      inflight = false;
      loading = false;
    }
  }

  async function loadProxy() {
    try {
      const all = await BrowserProxyService.list(workspaceId);
      proxy = all.find((p) => p.processId === processId) ?? null;
    } catch {
      proxy = null;
    }
  }

  $effect(() => {
    void processId;
    void since;
    void instanceKey;
    open = {};
    void load();
  });

  $effect(() => {
    void processId;
    void loadProxy();
  });

  $effect(() => {
    if (!live) return;
    const t = setInterval(() => {
      if (typeof document !== 'undefined' && document.visibilityState !== 'visible') return;
      void load(true);
      if (proxy) void loadProxy();
    }, 4000);
    return () => clearInterval(t);
  });

  async function copy(text: string, key: string) {
    try {
      await getPlatform().copyText(text);
      copied = key;
      setTimeout(() => {
        if (copied === key) copied = '';
      }, 1200);
    } catch {
      toasts.err('Could not copy to the clipboard');
    }
  }

  async function startProxy() {
    proxyBusy = true;
    try {
      proxy = await BrowserProxyService.start(processId, { warnings });
      toasts.ok('Browser capture is on. Open the proxy URL to record frontend errors.');
    } catch (err) {
      toastError(err);
    } finally {
      proxyBusy = false;
    }
  }

  async function stopProxy() {
    if (!proxy) return;
    proxyBusy = true;
    try {
      await BrowserProxyService.stop(proxy.workspaceId, proxy.id);
      proxy = null;
    } catch (err) {
      toastError(err);
    } finally {
      proxyBusy = false;
    }
  }

  function openProxy() {
    if (proxy) window.open(proxy.url, '_blank', 'noopener');
  }

  function toggle(g: ErrorGroup) {
    open = { ...open, [g.fingerprint]: !open[g.fingerprint] };
  }

  const groups = $derived(result?.groups ?? []);
  const projectHits = $derived(groups.filter((g) => g.locations.some((l) => l.inProject)).length);
</script>

<div class="errs">
  <div class="bar">
    <Bug size={15} />
    <strong>Errors</strong>
    {#if result}
      <span class="count" class:zero={groups.length === 0}>{groups.length}{result.truncated ? '+' : ''} group{groups.length === 1 ? '' : 's'}</span>
      {#if result.totalErrors > groups.length}<span class="muted small">{result.totalErrors} occurrences</span>{/if}
    {/if}
    <span class="grow"></span>
    {#if live}<span class="livechip" title="Refreshes every few seconds while the process runs"><Radio size={12} />live</span>{/if}
    <SegmentedControl
      size="sm"
      label="Scan range"
      bind:value={since}
      options={[
        { value: 'instance', label: 'This run' },
        { value: 'all', label: 'All retained' }
      ]}
    />
    <button type="button" class="btn sm ghost icon" title="Refresh" aria-label="Refresh errors" disabled={loading} onclick={() => void load()}>
      <RefreshCw size={14} class={loading ? 'spin' : ''} />
    </button>
    <button
      type="button"
      class="btn sm"
      disabled={groups.length === 0}
      title="Copy a compact list of these errors with file and line, ready to paste to an agent"
      onclick={() => void copy(errorsDigest(groups), 'all')}
    >
      {#if copied === 'all'}<Check size={13} />{:else}<Copy size={13} />{/if}Copy for agent
    </button>
  </div>

  <div class="capture" class:on={!!proxy}>
    <Globe size={14} />
    {#if proxy}
      <div class="cap-text">
        <span><strong>Browser capture on.</strong> Pages opened through <button type="button" class="link mono" onclick={() => void copy(proxy!.url, 'proxy')}>{proxy.url}</button> report console errors, exceptions and failed requests here.</span>
        <span class="muted small">{proxy.pagesInstrumented} page{proxy.pagesInstrumented === 1 ? '' : 's'} instrumented · {proxy.reports} report{proxy.reports === 1 ? '' : 's'} · closes <RelativeTime ts={proxy.expiresAt} /></span>
      </div>
      <button type="button" class="btn sm" onclick={openProxy}><ExternalLink size={13} />Open</button>
      <button type="button" class="btn sm ghost" disabled={proxyBusy} onclick={() => void stopProxy()}><CircleStop size={13} />Stop</button>
    {:else}
      <div class="cap-text">
        <span>Frontend errors only show up in the browser console.</span>
        <span class="muted small">Capture them into this process's logs through a local proxy. Hot reload keeps working.</span>
      </div>
      <label class="warnopt"><input type="checkbox" bind:checked={warnings} />warnings too</label>
      <button type="button" class="btn sm" disabled={!live || proxyBusy} title={live ? 'Start a capture proxy in front of this dev server' : 'Start the process first'} onclick={() => void startProxy()}>
        {#if proxyBusy}<Spinner size={12} />{/if}Capture browser errors
      </button>
    {/if}
  </div>

  <div class="scroll">
    {#if error}
      <div class="state err">{error}</div>
    {:else if !result && loading}
      <div class="state"><Spinner size={14} /> Scanning logs…</div>
    {:else if groups.length === 0}
      <div class="empty">
        <div class="ok-icon"><CircleCheck size={22} /></div>
        <h3>No errors {since === 'instance' ? 'in this run' : 'in the retained logs'}</h3>
        <p>Compiler and type errors, exceptions, panics, failed tests, lint errors, browser errors and error-level log lines show up here, grouped and with file and line.</p>
      </div>
    {:else}
      {#if projectHits > 0}
        <p class="lead">{projectHits} of {groups.length} point into your code. Project files are highlighted.</p>
      {/if}
      <ul class="list">
        {#each groups as g (g.fingerprint)}
          {@const k = kindInfo(g.kind)}
          {@const expanded = !!open[g.fingerprint]}
          <li class="card" style="--kc: var({k.color})">
            <button type="button" class="card-head" aria-expanded={expanded} onclick={() => toggle(g)}>
              <ChevronRight size={14} class={expanded ? 'chev open' : 'chev'} />
              <span class="kind">{k.label}</span>
              <span class="msg" title={g.message}>{g.message}</span>
              {#if g.count > 1}<span class="times">×{g.count}</span>{/if}
              {#if g.lastSeen}<span class="when"><RelativeTime ts={g.lastSeen} /></span>{/if}
            </button>
            {#if g.locations.length > 0}
              <div class="locs">
                {#each g.locations as l, i (i)}
                  <button
                    type="button"
                    class="loc mono"
                    class:proj={l.inProject}
                    title={l.inProject ? 'In this project. Click to copy.' : 'Dependency or runtime frame. Click to copy.'}
                    onclick={() => void copy(locationLabel(l), g.fingerprint + i)}
                  >
                    {#if copied === g.fingerprint + i}<Check size={11} />{:else}<FileCode size={11} />{/if}{locationLabel(l)}
                  </button>
                {/each}
              </div>
            {/if}
            {#if expanded}
              <div class="sample">
                <pre class="mono">{g.sample.join('\n')}</pre>
                <button type="button" class="btn sm ghost copy-sample" onclick={() => void copy(g.sample.join('\n'), 's' + g.fingerprint)}>
                  {#if copied === 's' + g.fingerprint}<Check size={12} />{:else}<Copy size={12} />{/if}Copy
                </button>
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>

<style>
  .errs {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-5);
    border-bottom: 1px solid var(--border);
    color: var(--text-1);
    flex-wrap: wrap;
  }
  .bar strong {
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .count {
    padding: 1px 8px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--err) 14%, var(--bg-3));
    color: var(--text-0);
    font-size: var(--fs-xs);
    font-variant-numeric: tabular-nums;
  }
  .count.zero {
    background: color-mix(in srgb, var(--ok) 14%, var(--bg-3));
  }
  .small {
    font-size: var(--fs-xs);
  }
  .grow {
    flex: 1;
  }
  .livechip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: var(--fs-micro, 10px);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--ok);
  }
  .bar :global(.spin) {
    animation: spin 0.9s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .capture {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin: var(--space-3) var(--space-5) 0;
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius);
    border: 1px dashed var(--border-strong, var(--border));
    background: var(--bg-2);
    color: var(--text-1);
    font-size: var(--fs-sm);
    flex-wrap: wrap;
  }
  .capture.on {
    border-style: solid;
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
    background: color-mix(in srgb, var(--accent) 7%, var(--bg-1));
  }
  .capture > :global(svg) {
    color: var(--accent);
    flex: none;
  }
  .cap-text {
    flex: 1;
    min-width: 220px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .cap-text strong {
    color: var(--text-0);
    font-weight: 600;
  }
  .link {
    padding: 0;
    border: none;
    background: none;
    color: var(--accent);
    font-size: inherit;
    cursor: pointer;
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .warnopt {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-xs);
    color: var(--text-1);
    cursor: pointer;
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--space-4) var(--space-5) var(--space-6);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-6);
    font-size: var(--fs-sm);
  }
  .state.err {
    color: var(--err);
  }
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: var(--space-2);
    padding: var(--space-8) var(--space-4);
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .empty h3 {
    color: var(--text-0);
    font-size: var(--fs-md, 14px);
    margin: 0;
  }
  .empty p {
    margin: 0;
    max-width: 420px;
  }
  .ok-icon {
    width: 44px;
    height: 44px;
    border-radius: 12px;
    display: grid;
    place-items: center;
    background: color-mix(in srgb, var(--ok) 14%, var(--bg-3));
    color: var(--ok);
  }
  .lead {
    margin: 0;
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .card {
    border: 1px solid var(--border);
    border-left: 3px solid var(--kc);
    border-radius: var(--radius);
    background: var(--bg-1);
    overflow: hidden;
  }
  .card-head {
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 8px var(--space-3);
    border: none;
    border-radius: 0;
    background: transparent;
    color: var(--text-0);
    text-align: left;
    font-size: var(--fs-sm);
    cursor: pointer;
  }
  .card-head:hover {
    background: var(--bg-hover);
  }
  .card-head :global(.chev) {
    flex: none;
    color: var(--text-2);
    transition: transform 0.12s ease;
  }
  .card-head :global(.chev.open) {
    transform: rotate(90deg);
  }
  .kind {
    flex: none;
    padding: 0 7px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--kc) 16%, var(--bg-3));
    color: var(--text-0);
    font-size: var(--fs-micro, 10px);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    line-height: 18px;
  }
  .msg {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
  }
  .times {
    flex: none;
    padding: 0 6px;
    border-radius: 6px;
    background: var(--bg-3);
    color: var(--text-1);
    font-size: var(--fs-xs);
    font-variant-numeric: tabular-nums;
  }
  .when {
    flex: none;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .locs {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 0 var(--space-3) 8px 34px;
  }
  .loc {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    max-width: 100%;
    padding: 1px 8px;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-2);
    font-size: var(--fs-micro, 11px);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
  }
  .loc.proj {
    color: var(--text-0);
    border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
    background: color-mix(in srgb, var(--accent) 9%, var(--bg-2));
  }
  .loc:hover {
    border-color: var(--border-strong, var(--text-2));
  }
  .sample {
    position: relative;
    border-top: 1px solid var(--border);
    background: var(--bg-0);
  }
  .sample pre {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    font-size: var(--fs-xs);
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--text-1);
    max-height: 280px;
    overflow: auto;
  }
  .copy-sample {
    position: absolute;
    top: 6px;
    right: 8px;
  }
  @media (max-width: 640px) {
    .bar,
    .scroll {
      padding-left: var(--space-4);
      padding-right: var(--space-4);
    }
    .capture {
      margin-left: var(--space-4);
      margin-right: var(--space-4);
    }
    .when {
      display: none;
    }
    .locs {
      padding-left: var(--space-3);
    }
  }
</style>
