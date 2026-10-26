<script lang="ts">
  import { getContext, untrack } from 'svelte';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Plug from '@lucide/svelte/icons/plug';
  import SearchX from '@lucide/svelte/icons/search-x';
  import { IntegrationService, SessionService } from '../../lib/api';
  import type { HarnessInfo, IntegrationChange, IntegrationSnippet, ScopeName, Session, VerifyCheck } from '../../lib/api/types';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { router } from '../../lib/router.svelte';
  import { query as createQuery } from '../../lib/data/query.svelte';
  import { ROUTE_CONTEXT, type RouteView } from '../../lib/routes';
  import { sessions as sessionState } from '../../lib/state/sessions.svelte';
  import { toastError, toasts } from '../../lib/toasts.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import SearchField from '../../lib/ui/SearchField.svelte';
  import SegmentedControl from '../../lib/ui/SegmentedControl.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Switch from '../../lib/ui/Switch.svelte';
  import HarnessCard from '../../features/integrations/HarnessCard.svelte';
  import ChangeDialog from '../../features/integrations/ChangeDialog.svelte';
  import SnippetDialog from '../../features/integrations/SnippetDialog.svelte';
  import VerifyDialog from '../../features/integrations/VerifyDialog.svelte';
  import SkillsPanel from '../../features/integrations/SkillsPanel.svelte';
  import { actionLabel, filterHarnesses, needsAttention, scopeOf, sortHarnesses, type HarnessFilter } from '../../features/integrations/harness';

  function stored(key: string): string {
    try {
      return localStorage.getItem(key) ?? '';
    } catch {
      return '';
    }
  }

  function readScope(): ScopeName {
    const v = stored('ar.integrations.scope');
    return v === 'project' || v === 'local' ? v : 'user';
  }

  const route = getContext<RouteView | undefined>(ROUTE_CONTEXT);
  let hydrated = false;

  const sessions = $derived(sessionState.list);
  let refreshing = $state(false);
  let error = $state('');
  let scope = $state<ScopeName>(readScope());
  let projectPath = $state(scopeState.path);
  let query = $state('');
  let filter = $state<HarnessFilter>('all');
  let showManual = $state(stored('ar.integrations.manual') === '1');
  let busy = $state('');
  let skillsFocus = $state('');
  let skillsEl = $state<HTMLElement | null>(null);
  let token = 0;

  let changeOpen = $state(false);
  let changeTitle = $state('');
  let changeAction = $state<'install' | 'remove'>('install');
  let changeChanges = $state<IntegrationChange[]>([]);
  let changeLoading = $state(false);
  let changeError = $state('');
  let changeBusy = $state(false);
  let changeTarget = $state<HarnessInfo | null>(null);
  let batch = $state<HarnessInfo[]>([]);

  let snippetOpen = $state(false);
  let snippetLoading = $state(false);
  let snippetError = $state('');
  let snippetData = $state<IntegrationSnippet | null>(null);

  let verifyOpen = $state(false);
  let verifyTitle = $state('');
  let verifyChecks = $state<VerifyCheck[]>([]);
  let verifyLoading = $state(false);
  let verifyError = $state('');
  let verifyTarget = $state<HarnessInfo | null>(null);

  $effect(() => {
    const path = route?.path ?? router.path;
    if (route && !route.active) return;
    const params = new URLSearchParams(path.split('?')[1] ?? '');
    const rawScope = params.get('scope');
    const nextScope: ScopeName = rawScope === 'project' || rawScope === 'local' ? rawScope : 'user';
    const q = params.get('q') ?? '';
    const rawFilter = params.get('filter') ?? 'all';
    const nextFilter: HarnessFilter = ['all', 'detected', 'connected', 'attention'].includes(rawFilter) ? (rawFilter as HarnessFilter) : 'all';
    untrack(() => {
      if (nextScope !== scope) scope = nextScope;
      if (q !== query) query = q;
      if (nextFilter !== filter) filter = nextFilter;
    });
    hydrated = true;
  });

  $effect(() => {
    const s = scope;
    const q = query;
    const f = filter;
    if (route && !route.active) return;
    if (!hydrated) return;
    const t = setTimeout(() => router.updateParams({
      scope: s === 'user' ? null : s,
      q: q.trim() || null,
      filter: f === 'all' ? null : f
    }), 160);
    return () => clearTimeout(t);
  });

  const effectiveProject = $derived(scope === 'user' ? '' : projectPath);
  const harnessQuery = createQuery<HarnessInfo[]>(
    () => `harnesses:${scope}:${effectiveProject}`,
    () => IntegrationService.listHarnesses(effectiveProject),
    { staleMs: 20000, refetchOnFocus: true, persist: true }
  );
  const harnesses = $derived(harnessQuery.data ?? []);
  const loading = $derived(harnessQuery.data === undefined && (harnessQuery.status === 'loading' || harnessQuery.fetching));
  const supportsLocal = $derived(harnesses.some((h) => h.scopes.some((s) => s.scope === 'local')));

  const scopeOptions = $derived([
    { value: 'user', label: 'All projects' },
    { value: 'project', label: 'This project · shared' },
    ...(supportsLocal ? [{ value: 'local', label: 'This project · only me' }] : [])
  ]);

  const filterOptions = [
    { value: 'all', label: 'All' },
    { value: 'detected', label: 'Detected' },
    { value: 'connected', label: 'Connected' },
    { value: 'attention', label: 'Needs attention' }
  ];

  const workspaceOptions = $derived(scopeState.workspaces.map((w) => ({ value: w.path, label: scopeState.workspaceLabel(w.id) })));

  const list = $derived.by(() => {
    const live: Record<string, number> = {};
    for (const s of sessions) {
      if (!s.closed && s.harness) live[s.harness] = (live[s.harness] ?? 0) + 1;
    }
    return harnesses.map((h) => ({ ...h, connected: Math.max(h.connected, live[h.id] ?? 0) }));
  });

  const shown = $derived(sortHarnesses(filterHarnesses(list, scope, query, filter, showManual), scope));
  const detectedCount = $derived(harnesses.filter((h) => h.detected).length);
  const connectedCount = $derived(harnesses.filter((h) => scopeOf(h, scope)?.status === 'installed').length);
  const attentionCount = $derived(harnesses.filter((h) => needsAttention(h, scope)).length);
  const filtersActive = $derived(query.trim() !== '' || filter !== 'all' || showManual);

  $effect(() => {
    try {
      localStorage.setItem('ar.integrations.scope', scope);
    } catch {
      return;
    }
  });

  $effect(() => {
    try {
      localStorage.setItem('ar.integrations.manual', showManual ? '1' : '0');
    } catch {
      return;
    }
  });

  $effect(() => {
    const path = scopeState.path;
    if (path) projectPath = path;
  });

  $effect(() => {
    if (scope === 'user' || projectPath) return;
    const first = workspaceOptions[0]?.value;
    if (first) projectPath = first;
  });

  $effect(() => {
    if (scope !== 'user' && !effectiveProject) return;
    void load();
  });

  $effect(() => {
    if (!loading && scope === 'local' && !supportsLocal) scope = 'project';
  });

  async function load(manual = false) {
    if (manual) refreshing = true;
    await harnessQuery.refetch();
    error = harnessQuery.error;
    refreshing = false;
  }

  function clearFilters() {
    query = '';
    filter = 'all';
    showManual = false;
  }

  function focusSkills(h: HarnessInfo) {
    skillsFocus = h.id;
    skillsEl?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  async function connect(h: HarnessInfo) {
    busy = h.id;
    try {
      const changes = await IntegrationService.preview(h.id, scope, effectiveProject, 'install');
      changeTarget = h;
      batch = [];
      changeAction = 'install';
      changeTitle = `${actionLabel(scopeOf(h, scope)?.status ?? 'absent')} ${h.displayName}`;
      changeChanges = changes;
      changeError = '';
      changeOpen = true;
    } catch (err) {
      toastError(err);
    } finally {
      busy = '';
    }
  }

  async function remove(h: HarnessInfo) {
    busy = h.id;
    try {
      const changes = await IntegrationService.preview(h.id, scope, effectiveProject, 'remove');
      changeTarget = h;
      batch = [];
      changeAction = 'remove';
      changeTitle = `Disconnect ${h.displayName}`;
      changeChanges = changes;
      changeError = '';
      changeOpen = true;
    } catch (err) {
      toastError(err);
    } finally {
      busy = '';
    }
  }

  async function connectAll() {
    const pending = harnesses.filter((h) => {
      if (!h.detected || h.tier !== 1) return false;
      const s = scopeOf(h, scope)?.status;
      return s !== 'installed' && s !== 'unavailable';
    });
    if (pending.length === 0) {
      toasts.info('All detected agents are already connected');
      return;
    }
    busy = '__all';
    changeTarget = null;
    batch = pending;
    changeAction = 'install';
    changeTitle = `Connect ${pending.length} detected ${pending.length === 1 ? 'agent' : 'agents'}`;
    changeChanges = [];
    changeError = '';
    changeLoading = true;
    changeOpen = true;
    const previews = await Promise.allSettled(
      pending.map((h) => IntegrationService.preview(h.id, scope, effectiveProject, 'install'))
    );
    const merged: IntegrationChange[] = [];
    for (const r of previews) {
      if (r.status === 'fulfilled') merged.push(...r.value);
      else changeError = r.reason instanceof Error ? r.reason.message : String(r.reason);
    }
    changeChanges = merged;
    changeLoading = false;
  }

  async function applyChange() {
    changeBusy = true;
    try {
      if (batch.length > 0) {
        await Promise.all(batch.map((h) => IntegrationService.install(h.id, scope, effectiveProject)));
        toasts.ok(`Connected ${batch.length} ${batch.length === 1 ? 'agent' : 'agents'}`);
        const hint = batch.find((h) => h.restartHint)?.restartHint ?? '';
        if (hint) toasts.info(hint);
        batch = [];
      } else if (changeTarget) {
        const h = changeTarget;
        if (changeAction === 'install') {
          await IntegrationService.install(h.id, scope, effectiveProject);
          toasts.ok(`Connected ${h.displayName}${h.restartHint ? `\n${h.restartHint}` : ''}`);
        } else {
          await IntegrationService.remove(h.id, scope, effectiveProject);
          toasts.ok(`Disconnected ${h.displayName}${h.restartHint ? `\n${h.restartHint}` : ''}`);
        }
      }
      changeOpen = false;
      await load();
    } catch (err) {
      changeError = err instanceof Error ? err.message : String(err);
      toastError(err);
    } finally {
      changeBusy = false;
      busy = '';
    }
  }

  async function openSnippet(h: HarnessInfo) {
    snippetOpen = true;
    snippetLoading = true;
    snippetError = '';
    snippetData = null;
    busy = h.id;
    try {
      snippetData = await IntegrationService.snippet(h.id, scope, effectiveProject);
    } catch (err) {
      snippetError = err instanceof Error ? err.message : String(err);
    } finally {
      snippetLoading = false;
      busy = '';
    }
  }

  async function runVerify(h: HarnessInfo) {
    verifyLoading = true;
    verifyError = '';
    try {
      const r = await IntegrationService.verify(h.id, scope, effectiveProject);
      verifyChecks = r.checks;
    } catch (err) {
      verifyError = err instanceof Error ? err.message : String(err);
    } finally {
      verifyLoading = false;
    }
  }

  async function openVerify(h: HarnessInfo) {
    verifyTarget = h;
    verifyTitle = `Verify ${h.displayName}`;
    verifyChecks = [];
    verifyOpen = true;
    busy = h.id;
    await runVerify(h);
    busy = '';
  }
</script>

<div class="page">
  <header class="page-header">
    <div class="titles">
      <h1>Integrations</h1>
      <p class="subtitle">Connect AI coding agents to jack-ketch. Each agent gets the MCP server, wait limits tuned to that agent, and optional skills.</p>
    </div>
    <div class="actions">
      <Button onclick={() => void load(true)} loading={refreshing}><RefreshCw size={14} />Refresh</Button>
      <Button variant="primary" disabled={loading || busy !== '' || (scope !== 'user' && !effectiveProject)} onclick={() => void connectAll()}><Plug size={14} />Connect all detected</Button>
    </div>
  </header>

  <div class="toolbar">
    <SegmentedControl size="sm" label="Install scope" options={scopeOptions} value={scope} onchange={(v) => (scope = v as ScopeName)} />
    {#if scope !== 'user'}
      {#if workspaceOptions.length > 0}
        <div class="ws">
          <Select bind:value={projectPath} options={workspaceOptions} label="Project" size="sm" />
        </div>
      {:else}
        <span class="muted">Open a project from the top bar first.</span>
      {/if}
    {/if}
    <span class="grow"></span>
    <div class="search">
      <SearchField bind:value={query} placeholder="Search agents" label="Search agents" size="sm" />
    </div>
    <SegmentedControl size="sm" label="Filter agents" options={filterOptions} value={filter} onchange={(v) => (filter = v as HarnessFilter)} />
    <Switch bind:checked={showManual} label="Show manual-setup agents">Show manual-setup agents</Switch>
  </div>

  {#if !loading}
    <div class="summary">
      <button class="chip" class:active={filter === 'detected'} onclick={() => (filter = filter === 'detected' ? 'all' : 'detected')}>Detected <span class="count">{detectedCount}</span></button>
      <button class="chip" class:active={filter === 'connected'} onclick={() => (filter = filter === 'connected' ? 'all' : 'connected')}><span class="dot ok"></span>Connected <span class="count">{connectedCount}</span></button>
      <button class="chip" class:active={filter === 'attention'} onclick={() => (filter = filter === 'attention' ? 'all' : 'attention')}><span class="dot warn"></span>Needs attention <span class="count">{attentionCount}</span></button>
    </div>
  {/if}

  {#if error}
    <div class="banner err" role="alert">
      <span>Could not load integrations: {error}</span>
      <button class="btn sm" onclick={() => void load(true)}>Retry</button>
    </div>
  {/if}

  {#if loading}
    <div class="grid" aria-busy="true">
      {#each Array(6) as _, i (i)}
        <div class="card skel">
          <div class="skel-head">
            <Skeleton width="36px" height="36px" radius="10px" />
            <div class="skel-lines">
              <Skeleton width="120px" height="14px" />
              <Skeleton width="180px" height="12px" />
            </div>
          </div>
          <Skeleton width="100%" height="42px" />
          <Skeleton width="220px" height="28px" />
        </div>
      {/each}
    </div>
  {:else if shown.length === 0}
    <div class="card">
      <EmptyState
        title={filtersActive && list.length > 0 ? 'No matching agents' : 'No agents found'}
        body={filtersActive && list.length > 0 ? 'Nothing matches the current filters.' : 'The daemon reports no installable agents in this scope.'}
        icon={filtersActive && list.length > 0 ? SearchX : Plug}
      >
        {#snippet action()}
          {#if filtersActive && list.length > 0}
            <Button variant="secondary" onclick={clearFilters}>Clear filters</Button>
          {:else}
            <Button variant="secondary" onclick={() => void load(true)}>Refresh</Button>
          {/if}
        {/snippet}
      </EmptyState>
    </div>
  {:else}
    <div class="grid">
      {#each shown as h (h.id)}
        <HarnessCard
          harness={h}
          {scope}
          busy={busy === h.id || busy === '__all'}
          onconnect={() => void connect(h)}
          onremove={() => void remove(h)}
          onsnippet={() => void openSnippet(h)}
          onverify={() => void openVerify(h)}
          onskills={() => focusSkills(h)}
        />
      {/each}
    </div>
  {/if}

  <div class="skills-anchor" bind:this={skillsEl}>
    {#if !loading}
      <SkillsPanel {harnesses} {scope} projectPath={effectiveProject} focus={skillsFocus} />
    {/if}
  </div>
</div>

{#if changeOpen}
  <ChangeDialog
    title={changeTitle}
    action={changeAction}
    changes={changeChanges}
    loading={changeLoading}
    error={changeError}
    busy={changeBusy}
    onconfirm={() => void applyChange()}
    onclose={() => {
      changeOpen = false;
      busy = '';
    }}
  />
{/if}

{#if snippetOpen}
  <SnippetDialog snippet={snippetData} loading={snippetLoading} error={snippetError} onclose={() => (snippetOpen = false)} />
{/if}

{#if verifyOpen}
  <VerifyDialog
    title={verifyTitle}
    checks={verifyChecks}
    loading={verifyLoading}
    error={verifyError}
    onrerun={() => {
      if (verifyTarget) void runVerify(verifyTarget);
    }}
    onclose={() => (verifyOpen = false)}
  />
{/if}

<style>
  .toolbar {
    flex-wrap: wrap;
  }
  .ws {
    width: 230px;
    max-width: 100%;
  }
  .search {
    width: 220px;
    max-width: 100%;
  }
  .summary {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .summary .chip {
    height: 28px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: var(--space-4);
  }
  .skel {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .skel-head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .skel-lines {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .skills-anchor {
    scroll-margin-top: var(--space-6);
  }
</style>
