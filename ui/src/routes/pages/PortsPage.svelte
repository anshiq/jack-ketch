<script lang="ts">
  import { getContext } from 'svelte';
  import Network from '@lucide/svelte/icons/network';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import CircleStop from '@lucide/svelte/icons/circle-stop';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import ArrowUpRight from '@lucide/svelte/icons/arrow-up-right';
  import { SystemService, type PortEntry, type PortsView } from '../../lib/api';
  import { ROUTE_CONTEXT, type RouteView } from '../../lib/routes';
  import { router } from '../../lib/router.svelte';
  import { dialogs } from '../../lib/state/dialogs.svelte';
  import { procActions } from '../../components/processActions.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import Switch from '../../lib/ui/Switch.svelte';
  import { portKind, portMatches, shortCommand, browserURL, countKinds, type PortKind } from '../../lib/portsView';

  type Filter = 'all' | PortKind;

  const route = getContext<RouteView | undefined>(ROUTE_CONTEXT);
  let view = $state<PortsView | null>(null);
  let error = $state('');
  let refreshing = $state(false);
  let query = $state('');
  let filter = $state<Filter>('all');
  let auto = $state(true);
  let busy = $state(new Set<string>());

  async function load(manual = false) {
    if (manual) refreshing = true;
    try {
      view = await SystemService.ports();
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      refreshing = false;
    }
  }

  $effect(() => {
    if (route && !route.active) return;
    void load();
    if (!auto) return;
    const t = setInterval(() => {
      if (document.visibilityState === 'visible') void load();
    }, 5000);
    return () => clearInterval(t);
  });

  const ports = $derived(view?.ports ?? []);
  const counts = $derived(countKinds(ports));
  const rows = $derived(ports.filter((p) => (filter === 'all' || portKind(p) === filter) && portMatches(p, query)));
  const filtersActive = $derived(query.trim() !== '' || filter !== 'all');

  function key(p: PortEntry) {
    return `${p.port}/${p.pid ?? 0}`;
  }

  async function stopForeign(p: PortEntry) {
    if (!p.pid) return;
    const ok = await dialogs.confirm(
      `Send SIGTERM to pid ${p.pid} (${shortCommand(p.command ?? '', 120)}) so port ${p.port} is freed? jack-ketch does not manage this process, so make sure nothing you need is running there.`,
      { title: `Stop the process on :${p.port}?`, confirmLabel: 'Stop process', danger: true }
    );
    if (!ok) return;
    const k = key(p);
    busy = new Set(busy).add(k);
    try {
      await SystemService.killPortHolder(p.port, p.pid);
      toasts.ok(`Asked pid ${p.pid} to stop`);
      setTimeout(() => void load(), 700);
    } catch (err) {
      toastError(err);
    } finally {
      const next = new Set(busy);
      next.delete(k);
      busy = next;
    }
  }

  async function stopManaged(p: PortEntry) {
    if (!p.managed) return;
    await procActions.stop([p.managed.processId]);
    setTimeout(() => void load(), 700);
  }

  function clear() {
    query = '';
    filter = 'all';
  }

  const CHIPS: { id: Filter; label: string; dot?: string }[] = [
    { id: 'all', label: 'All' },
    { id: 'managed', label: 'Managed', dot: 'ok' },
    { id: 'conflict', label: 'Conflicts', dot: 'err' },
    { id: 'declared', label: 'Declared, free', dot: 'info' },
    { id: 'other', label: 'Other', dot: '' }
  ];
</script>

<div class="page">
  <header class="page-header">
    <div class="titles">
      <h1>Ports</h1>
      <p class="subtitle">Who is listening where: managed processes, the ports your apps declare, and everything else running as you.</p>
    </div>
    <div class="actions">
      <Switch bind:checked={auto} label="Auto-refresh">Auto-refresh</Switch>
      <button class="btn" onclick={() => void load(true)} disabled={refreshing} aria-label="Refresh ports">
        {#if refreshing}<Spinner size={14} />{:else}<RefreshCw size={14} />{/if}Refresh
      </button>
    </div>
  </header>

  <div class="toolbar">
    <div class="chips" role="group" aria-label="Port kind">
      {#each CHIPS as c (c.id)}
        {#if c.id === 'all' || c.id === 'managed' || counts[c.id] > 0}
          <button class="chip" class:active={filter === c.id} onclick={() => (filter = c.id)}>
            {#if c.dot !== undefined}<span class="dot {c.dot}"></span>{/if}{c.label} <span class="count">{counts[c.id]}</span>
          </button>
        {/if}
      {/each}
    </div>
    <div class="input-wrap search">
      <Search size={14} />
      <input type="search" placeholder="Port, app, command, pid" aria-label="Search ports" bind:value={query} />
    </div>
    {#if filtersActive}
      <button class="btn ghost sm" onclick={clear}><X size={14} />Clear filters</button>
    {/if}
  </div>

  {#if error}
    <div class="banner err" role="alert"><TriangleAlert size={16} /><span>Could not load ports: {error}</span><button class="btn sm" onclick={() => void load(true)}>Retry</button></div>
  {:else if view && !view.supported}
    <div class="banner warn" role="status"><TriangleAlert size={16} /><span>Listing listening sockets is only supported on Linux for now. Declared ports from your app config are still shown.</span></div>
  {/if}

  {#if counts.conflict > 0 && filter !== 'conflict'}
    <button type="button" class="conflict-callout" onclick={() => (filter = 'conflict')}>
      <TriangleAlert size={15} />
      <span><strong>{counts.conflict} declared port{counts.conflict === 1 ? ' is' : 's are'} taken by something else.</strong> The app that declares it will fail to start with "address already in use".</span>
      <ArrowUpRight size={14} />
    </button>
  {/if}

  {#if !view && !error}
    <div class="table-wrap" aria-busy="true">
      <table class="data">
        <thead><tr><th class="num">Port</th><th>Listening process</th><th>Declared by</th><th>State</th><th></th></tr></thead>
        <tbody>
          {#each Array(5) as _, i (i)}
            <tr>
              <td class="num"><Skeleton width="44px" height="14px" /></td>
              <td><Skeleton width="220px" height="12px" /></td>
              <td><Skeleton width="100px" height="12px" /></td>
              <td><Skeleton width="60px" height="18px" /></td>
              <td></td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else if rows.length === 0}
    <div class="card">
      <div class="empty-state">
        <span class="icon-wrap"><Network size={20} /></span>
        {#if filtersActive}
          <h3>No matching ports</h3>
          <p>Nothing matches the current filters.</p>
          <button class="btn" onclick={clear}><X size={14} />Clear filters</button>
        {:else}
          <h3>Nothing is listening</h3>
          <p>Ports appear here as soon as a dev server starts. Declare ports in app config (<span class="mono">ports:</span>) to reserve them per workspace.</p>
        {/if}
      </div>
    </div>
  {:else}
    <div class="table-wrap">
      <table class="data">
        <thead>
          <tr>
            <th class="num">Port</th>
            <th>Listening process</th>
            <th>Declared by</th>
            <th>State</th>
            <th class="act"><span class="sr">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each rows as p (key(p))}
            {@const kind = portKind(p)}
            <tr class:conflict={kind === 'conflict'} class:free={kind === 'declared'}>
              <td class="num">
                <div class="portcell">
                  <span class="port mono">{p.port}</span>
                  {#if p.address}<span class="addr mono" title="Bound address">{p.address === '*' ? 'all interfaces' : p.address}</span>{/if}
                </div>
              </td>
              <td>
                {#if p.managed}
                  <div class="owner">
                    <a class="app" href="/processes/{p.managed.processId}" use:router.link={`/processes/${p.managed.processId}`}>{p.managed.app || shortCommand(p.command ?? '')}</a>
                    {#if p.managed.mode}<span class="modechip">{p.managed.mode}</span>{/if}
                    <span class="sub mono" title={p.managed.workspacePath}>{p.managed.processId} · pid {p.pid} · {p.managed.workspacePath}</span>
                  </div>
                {:else if p.listening}
                  <div class="owner">
                    <span class="cmd mono" title={p.command}>{shortCommand(p.command ?? '') || 'unknown process'}</span>
                    <span class="sub mono">pid {p.pid}{p.self ? ' · this daemon' : ''}</span>
                  </div>
                {:else}
                  <span class="muted">Not listening</span>
                {/if}
              </td>
              <td>
                {#if p.declared?.length}
                  <div class="decl">
                    {#each p.declared as d (d.workspaceId + d.app + d.name)}
                      <span class="declchip" title={d.workspacePath}>{d.app}<span class="muted">/{d.name}</span>{#if d.default && d.default !== d.port}<span class="muted"> (default {d.default})</span>{/if}</span>
                    {/each}
                  </div>
                {:else}
                  <span class="muted">—</span>
                {/if}
              </td>
              <td>
                {#if kind === 'conflict'}
                  <span class="badge err" title={p.conflict}>conflict</span>
                {:else if kind === 'managed'}
                  <span class="badge ok">managed</span>
                {:else if kind === 'declared'}
                  <span class="badge info">free</span>
                {:else if kind === 'daemon'}
                  <span class="badge accent">jack-ketch</span>
                {:else}
                  <span class="badge">other</span>
                {/if}
              </td>
              <td class="act">
                <div class="row-actions">
                  {#if p.listening}
                    <a class="btn sm ghost icon" href={browserURL(p)} target="_blank" rel="noopener noreferrer" title="Open {browserURL(p)}" aria-label="Open port {p.port} in the browser"><ExternalLink size={14} /></a>
                  {/if}
                  {#if p.managed}
                    <button class="btn sm" onclick={() => void stopManaged(p)} title="Stop this managed process"><CircleStop size={13} />Stop</button>
                  {:else if p.listening && p.pid && !p.self}
                    <button class="btn sm danger-ghost" disabled={busy.has(key(p))} onclick={() => void stopForeign(p)} title="Send SIGTERM to free this port">
                      {#if busy.has(key(p))}<Spinner size={12} />{:else}<CircleStop size={13} />{/if}Free port
                    </button>
                  {/if}
                </div>
              </td>
            </tr>
            {#if p.conflict}
              <tr class="conflict-detail">
                <td></td>
                <td colspan="4"><TriangleAlert size={12} />{p.conflict}</td>
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
  .dot.info {
    background: var(--info);
  }
  .conflict-callout {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius);
    border: 1px solid color-mix(in srgb, var(--err) 40%, var(--border));
    background: color-mix(in srgb, var(--err) 8%, var(--bg-1));
    color: var(--text-1);
    font-size: var(--fs-sm);
    text-align: left;
    cursor: pointer;
  }
  .conflict-callout > :global(svg:first-child) {
    color: var(--err);
    flex: none;
  }
  .conflict-callout span {
    flex: 1;
  }
  .conflict-callout strong {
    color: var(--text-0);
  }
  .portcell {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
  }
  .port {
    font-size: var(--fs-md, 14px);
    font-weight: 600;
    color: var(--text-0);
    font-variant-numeric: tabular-nums;
  }
  .addr {
    font-size: var(--fs-micro, 10px);
    color: var(--text-2);
  }
  .owner {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 8px;
    min-width: 0;
  }
  .app {
    font-weight: 600;
    color: var(--text-0);
    text-decoration: none;
  }
  .app:hover {
    color: var(--accent);
    text-decoration: underline;
  }
  .modechip {
    padding: 0 7px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--accent) 16%, var(--bg-3));
    color: var(--text-0);
    font-size: 11px;
  }
  .cmd {
    color: var(--text-0);
    font-size: var(--fs-xs);
    max-width: 520px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    flex-basis: 100%;
    color: var(--text-2);
    font-size: var(--fs-micro, 11px);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .decl {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .declchip {
    padding: 1px 8px;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    font-size: var(--fs-xs);
    color: var(--text-0);
  }
  tr.conflict td {
    background: color-mix(in srgb, var(--err) 5%, transparent);
  }
  tr.free td {
    color: var(--text-2);
  }
  tr.conflict-detail td {
    padding-top: 0;
    font-size: var(--fs-xs);
    color: var(--err);
    background: color-mix(in srgb, var(--err) 5%, transparent);
  }
  tr.conflict-detail :global(svg) {
    vertical-align: -1px;
    margin-right: 6px;
  }
  .row-actions {
    display: flex;
    justify-content: flex-end;
    gap: 6px;
  }
  .danger-ghost:hover:not(:disabled) {
    color: var(--err);
    border-color: color-mix(in srgb, var(--err) 45%, var(--border));
  }
  @media (max-width: 760px) {
    .cmd {
      max-width: 220px;
    }
    .sub {
      white-space: normal;
      word-break: break-all;
    }
  }
</style>
