<script lang="ts">
  import { getContext, onDestroy, untrack } from 'svelte';
  import { flip } from 'svelte/animate';
  import Boxes from '@lucide/svelte/icons/boxes';
  import Search from '@lucide/svelte/icons/search';
  import Play from '@lucide/svelte/icons/play';
  import Square from '@lucide/svelte/icons/square';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import FileCog from '@lucide/svelte/icons/file-cog';
  import Rocket from '@lucide/svelte/icons/rocket';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import Network from '@lucide/svelte/icons/network';
  import HeartPulse from '@lucide/svelte/icons/heart-pulse';
  import Radio from '@lucide/svelte/icons/radio';
  import Repeat from '@lucide/svelte/icons/repeat';
  import Zap from '@lucide/svelte/icons/zap';
  import Timer from '@lucide/svelte/icons/timer';
  import Link2 from '@lucide/svelte/icons/link-2';
  import X from '@lucide/svelte/icons/x';
  import Check from '@lucide/svelte/icons/check';
  import Layers from '@lucide/svelte/icons/layers';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import ListChecks from '@lucide/svelte/icons/list-checks';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CircleX from '@lucide/svelte/icons/circle-x';
  import ArrowLeftRight from '@lucide/svelte/icons/arrow-left-right';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import WorkspacePicker from '../../components/WorkspacePicker.svelte';
  import { AssistService, ConfigService, ProcessService } from '../../lib/api';
  import { realtime } from '../../lib/realtime/index.svelte';
  import type { ChecksResult, Process } from '../../lib/api/types';
  import { router } from '../../lib/router.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { processes } from '../../lib/state/processes.svelte';
  import { clock } from '../../lib/state/clock.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import { duration } from '../../lib/format';
  import { commandLine, isLive, matchProcesses, modeView, primaryProcess, sourceLabel, toConfigApps, type ConfigApp } from '../../lib/config/apps';
  import { ROUTE_CONTEXT, type RouteView } from '../../lib/routes';

  interface Row {
    app: ConfigApp;
    matches: Process[];
    primary: Process | null;
    live: boolean;
  }

  interface StackResult {
    order: string[];
    started: { app: string; processId: string }[];
    failed: string;
    error: string;
  }

  interface LayerProblem {
    layer: string;
    count: number;
  }

  const route = getContext<RouteView | undefined>(ROUTE_CONTEXT);
  let hydrated = false;
  let apps = $state<ConfigApp[]>([]);
  let loading = $state(false);
  let loadError = $state('');
  let loadedFor = $state('');
  let problems = $state<LayerProblem[]>([]);
  let query = $state('');
  let filter = $state<'all' | 'running' | 'stopped'>('all');
  let selected = $state(new Set<string>());
  let busy = $state(new Set<string>());
  let stack = $state<StackResult | null>(null);
  let starting = $state(false);
  let modeSel = $state<Record<string, string>>({});
  let checkRes = $state<Record<string, ChecksResult>>({});
  let checking = $state(new Set<string>());
  const now = $derived(clock.now);
  let watcher: { close(): void } | null = null;
  let loadToken = 0;

  const wsId = $derived(scopeState.workspaceId);

  const rows = $derived.by<Row[]>(() => {
    const pool = processes.list.filter((p) => p.workspaceId === wsId);
    return apps.map((app) => {
      const matches = matchProcesses(app, pool);
      const primary = primaryProcess(matches);
      return { app, matches, primary, live: !!primary && isLive(primary.status) };
    });
  });

  const counts = $derived({
    all: rows.length,
    running: rows.filter((r) => r.live).length,
    stopped: rows.filter((r) => !r.live).length
  });

  const visible = $derived(
    rows.filter((r) => {
      if (filter === 'running' && !r.live) return false;
      if (filter === 'stopped' && r.live) return false;
      const q = query.trim().toLowerCase();
      if (!q) return true;
      return (
        r.app.name.toLowerCase().includes(q) ||
        commandLine(r.app.command).toLowerCase().includes(q) ||
        r.app.description.toLowerCase().includes(q) ||
        r.app.modes.some((m) => m.name.toLowerCase().includes(q) || commandLine(m.command).toLowerCase().includes(q)) ||
        r.app.type.toLowerCase().includes(q) ||
        r.app.ports.some((p) => String(p.port).includes(q) || p.name.toLowerCase().includes(q))
      );
    })
  );

  const allVisibleSelected = $derived(visible.length > 0 && visible.every((r) => selected.has(r.app.name)));

  $effect(() => {
    const id = wsId;
    untrack(() => {
      selected = new Set();
      stack = null;
      if (!id) {
        apps = [];
        loadedFor = '';
        watcher?.close();
        watcher = null;
        return;
      }
      void load(id);
    });
  });

  $effect(() => {
    const path = route?.path ?? router.path;
    if (route && !route.active) return;
    const params = new URLSearchParams(path.split('?')[1] ?? '');
    const q = params.get('q') ?? '';
    const raw = params.get('filter') ?? 'all';
    const next = raw === 'running' || raw === 'stopped' ? raw : 'all';
    untrack(() => {
      if (q !== query) query = q;
      if (next !== filter) filter = next;
    });
    hydrated = true;
  });

  $effect(() => {
    const q = query;
    const status = filter;
    if (route && !route.active) return;
    if (!hydrated) return;
    const t = setTimeout(() => router.updateParams({ q: q.trim() || null, filter: status === 'all' ? null : status }), 160);
    return () => clearTimeout(t);
  });

  $effect(() => {
    if (!rows.some((r) => r.live)) return;
    return clock.retain(1000);
  });

  onDestroy(() => watcher?.close());

  async function load(id: string, silent = false) {
    const token = ++loadToken;
    if (!silent) loading = true;
    loadError = '';
    try {
      const cfg = await ConfigService.get(id);
      if (token !== loadToken) return;
      const c = cfg as unknown as { provenance?: Record<string, unknown>; revision?: number; projectId?: string };
      apps = toConfigApps(cfg.apps, c.provenance ?? {}, (c as unknown as { autoApps?: unknown }).autoApps ?? []);
      loadedFor = id;
      const layerErrors = (cfg as unknown as { layerErrors?: Record<string, unknown[]> }).layerErrors ?? {};
      const checks = Object.entries(layerErrors)
        .filter(([, errors]) => Array.isArray(errors) && errors.length > 0)
        .map(([layer, errors]) => ({ layer, count: errors.length }));
      if (token !== loadToken) return;
      problems = checks;
      if (!watcher && c.projectId) {
        const projectId = c.projectId;
        const since = Number(c.revision ?? 0);
        watcher = realtime.subscribe('config', { projectId, since }, (msg) => {
          if (msg['kind'] === 'revision' && scopeState.workspaceId) void load(scopeState.workspaceId, true);
        });
      }
    } catch (err) {
      if (token !== loadToken) return;
      loadError = err instanceof Error ? err.message : String(err);
      apps = [];
    } finally {
      if (token === loadToken) loading = false;
    }
  }

  function setBusy(name: string, on: boolean) {
    const next = new Set(busy);
    if (on) next.add(name);
    else next.delete(name);
    busy = next;
  }

  async function act(name: string, fn: () => Promise<unknown>, done: string) {
    setBusy(name, true);
    try {
      await fn();
      toasts.ok(done);
    } catch (err) {
      toastError(err);
    } finally {
      setBusy(name, false);
    }
  }

  function selectedMode(r: Row): string {
    const chosen = modeSel[r.app.name];
    if (chosen !== undefined) return chosen;
    if (r.live && r.primary?.mode) return r.primary.mode;
    return r.app.defaultMode;
  }

  function pickMode(r: Row, mode: string) {
    modeSel = { ...modeSel, [r.app.name]: mode };
  }

  function label(name: string, mode: string): string {
    return mode ? `${name} · ${mode}` : name;
  }

  const start = (r: Row) => {
    const mode = selectedMode(r);
    return act(r.app.name, () => ProcessService.start({ workspaceId: wsId, app: r.app.name, mode: mode || undefined }), `Started ${label(r.app.name, mode)}`);
  };

  const switchMode = (r: Row) => {
    const mode = selectedMode(r);
    const current = r.primary;
    return act(
      r.app.name,
      async () => {
        if (current) await ProcessService.stop(current.id);
        await ProcessService.start({ workspaceId: wsId, app: r.app.name, mode: mode || undefined });
      },
      `Switched ${r.app.name} to ${mode || 'its base command'}`
    );
  };

  async function runChecks(r: Row) {
    if (!r.primary) return;
    const name = r.app.name;
    const next = new Set(checking);
    next.add(name);
    checking = next;
    try {
      const res = await AssistService.runChecks({ processId: r.primary.id });
      checkRes = { ...checkRes, [name]: res };
      if (res.failed > 0) toasts.err(`${res.failed} of ${res.passed + res.failed} checks failed for ${name}`);
      else toasts.ok(`All ${res.passed} checks passed for ${name}`);
    } catch (err) {
      toastError(err);
    } finally {
      const done = new Set(checking);
      done.delete(name);
      checking = done;
    }
  }

  function clearChecks(name: string) {
    const next = { ...checkRes };
    delete next[name];
    checkRes = next;
  }
  const stop = (r: Row) => (r.primary ? act(r.app.name, () => ProcessService.stop(r.primary!.id), `Stopped ${r.app.name}`) : Promise.resolve());
  const restart = (r: Row) => (r.primary ? act(r.app.name, () => ProcessService.restart(r.primary!.id), `Restarted ${r.app.name}`) : Promise.resolve());

  function logs(p: Process) {
    void router.navigate(`/logs?p=${encodeURIComponent(p.id)}`);
  }

  function toggle(name: string) {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    selected = next;
  }

  function toggleAll() {
    if (allVisibleSelected) {
      const next = new Set(selected);
      for (const r of visible) next.delete(r.app.name);
      selected = next;
    } else {
      const next = new Set(selected);
      for (const r of visible) next.add(r.app.name);
      selected = next;
    }
  }

  async function startStack() {
    starting = true;
    stack = null;
    try {
      const res = await ProcessService.startStack(wsId, [...selected]);
      stack = {
        order: res.order ?? [],
        started: res.started ?? [],
        failed: res.failed ?? '',
        error: res.error ?? ''
      };
      if (stack.failed) toasts.err(`Stack stopped at ${stack.failed}`);
      else toasts.ok(`Stack started (${stack.started.length} app${stack.started.length === 1 ? '' : 's'})`);
    } catch (err) {
      toastError(err);
    } finally {
      starting = false;
    }
  }

  function stackState(name: string): 'ok' | 'failed' | 'skipped' {
    if (!stack) return 'skipped';
    if (stack.failed === name) return 'failed';
    return stack.started.some((s) => s.app === name) ? 'ok' : 'skipped';
  }

  function stackProc(name: string): string {
    return stack?.started.find((s) => s.app === name)?.processId ?? '';
  }

  function statusOf(r: Row): { label: string; dot: string; tone: string } {
    const p = r.primary;
    if (!p) return { label: 'Not running', dot: '', tone: 'muted' };
    switch (p.status) {
      case 'running':
      case 'ready':
        return { label: p.health && p.health !== 'unknown' && p.health !== 'none' ? `Running · ${p.health}` : 'Running', dot: 'ok', tone: 'ok' };
      case 'starting':
        return { label: 'Starting', dot: 'busy', tone: 'info' };
      case 'failed':
      case 'crashed':
        return { label: p.exitCode !== null ? `Crashed · exit ${p.exitCode}` : 'Crashed', dot: 'err', tone: 'err' };
      default:
        return { label: p.exitCode ? `Exited · code ${p.exitCode}` : 'Stopped', dot: '', tone: 'muted' };
    }
  }

  function uptime(p: Process): string {
    return p.startedAt ? duration(now - p.startedAt) : '';
  }

  const layerNames: Record<string, string> = { project: 'project', workspace: 'workspace' };
</script>

<div class="page">
  {#if scopeState.all}
    <div class="center">
      <WorkspacePicker
        icon={Boxes}
        title="Pick a workspace to see its apps"
        body="Apps are the services defined in a workspace's config. Choose a workspace to start, stop and inspect them."
      />
    </div>
  {:else}
    <div class="page-header">
      <div class="titles">
        <h1>Apps</h1>
        <span class="subtitle">
          {scopeState.label}
          {#if loadedFor === wsId}
            · {counts.all} configured · {counts.running} running
          {/if}
        </span>
      </div>
      <div class="actions">
        <button class="btn icon" aria-label="Refresh apps" title="Refresh" onclick={() => void load(wsId)} disabled={loading}>
          {#if loading}<Spinner size={14} />{:else}<RefreshCw size={14} />{/if}
        </button>
        <button class="btn" onclick={() => void router.navigate('/config')}><FileCog size={14} />Edit config</button>
      </div>
    </div>

    {#if loadError}
      <div class="banner err" role="alert">
        <CircleAlert size={16} />
        <span class="grow">Could not load this workspace's config: {loadError}</span>
        <button class="btn sm" onclick={() => void load(wsId)}>Retry</button>
      </div>
    {/if}

    {#if problems.length > 0}
      <div class="banner err" role="alert">
        <TriangleAlert size={16} />
        <span class="grow">
          The config has validation errors ({problems.map((p) => `${p.count} in the ${layerNames[p.layer] ?? p.layer} layer`).join(', ')}). Some apps may be missing or stale.
        </span>
        <button class="btn sm" onclick={() => void router.navigate('/config')}>Open in editor<ArrowRight size={14} /></button>
      </div>
    {/if}

    {#if loading && loadedFor !== wsId}
      <div class="grid" aria-busy="true">
        {#each Array.from({ length: 3 }) as _, i (i)}
          <div class="app skeleton">
            <span class="sk w40"></span>
            <span class="sk w90"></span>
            <span class="sk w60"></span>
            <span class="sk w30"></span>
          </div>
        {/each}
      </div>
    {:else if loadedFor === wsId && apps.length === 0 && !loadError}
      <div class="empty">
        <div class="empty-state">
          <div class="icon-wrap"><Boxes size={20} /></div>
          <h3>No apps configured</h3>
          <p>Define apps in this workspace's project config and they will show up here, ready to start with one click.</p>
          <button class="btn primary" onclick={() => void router.navigate('/config')}><FileCog size={14} />Open config editor</button>
        </div>
      </div>
    {:else if apps.length > 0}
      <div class="toolbar">
        <div class="input-wrap search">
          <Search size={14} />
          <input bind:value={query} placeholder="Search apps, commands, ports" aria-label="Search apps" spellcheck="false" />
        </div>
        <div class="chips" role="group" aria-label="Filter by status">
          <button class="chip" class:active={filter === 'all'} onclick={() => (filter = 'all')}>All <span class="count">{counts.all}</span></button>
          <button class="chip" class:active={filter === 'running'} onclick={() => (filter = 'running')}>Running <span class="count">{counts.running}</span></button>
          <button class="chip" class:active={filter === 'stopped'} onclick={() => (filter = 'stopped')}>Stopped <span class="count">{counts.stopped}</span></button>
        </div>
        <span class="grow"></span>
        <label class="selall">
          <input type="checkbox" checked={allVisibleSelected} onchange={toggleAll} disabled={visible.length === 0} aria-label="Select all visible apps" />
          Select all
        </label>
        <button class="btn primary" disabled={selected.size === 0 || starting} onclick={startStack}>
          {#if starting}<Spinner size={14} />Starting…{:else}<Rocket size={14} />Start stack{#if selected.size > 0}<span class="sel">{selected.size}</span>{/if}{/if}
        </button>
      </div>

      {#if stack}
        <section class="stack" class:bad={!!stack.failed} aria-label="Start stack result">
          <header>
            {#if stack.failed}
              <CircleAlert size={16} />
              <strong>Stack stopped at {stack.failed}</strong>
            {:else}
              <Check size={16} />
              <strong>Stack started</strong>
            {/if}
            <span class="muted">dependency order</span>
            <span class="grow"></span>
            <button class="btn icon ghost sm" aria-label="Dismiss result" onclick={() => (stack = null)}><X size={14} /></button>
          </header>
          {#if stack.order.length > 0}
            <ol class="order">
              {#each stack.order as name, i (name)}
                {@const st = stackState(name)}
                <li class={st}>
                  {#if i > 0}<ArrowRight size={14} class="arrow" />{/if}
                  <span class="step">
                    <span class="dot" class:ok={st === 'ok'} class:err={st === 'failed'}></span>
                    <span class="mono">{name}</span>
                    <span class="st">{st === 'ok' ? 'started' : st === 'failed' ? 'failed' : 'skipped'}</span>
                    {#if st === 'ok' && stackProc(name)}
                      <button class="linkbtn" onclick={() => router.navigate(`/logs?p=${encodeURIComponent(stackProc(name))}`)}>logs</button>
                    {/if}
                  </span>
                </li>
              {/each}
            </ol>
          {/if}
          {#if stack.failed}
            <div class="failure">
              <span class="mono">{stack.failed}</span>
              <span>{stack.error || 'The app did not become ready in time.'}</span>
            </div>
          {/if}
        </section>
      {/if}

      {#if visible.length === 0}
        <div class="empty">
          <div class="empty-state">
            <div class="icon-wrap"><Search size={20} /></div>
            <h3>No apps match</h3>
            <p>Nothing matches the current search or status filter.</p>
            <button class="btn sm" onclick={() => { query = ''; filter = 'all'; }}>Clear filters</button>
          </div>
        </div>
      {:else}
        <div class="grid">
          {#each visible as r (r.app.name)}
            {@const st = statusOf(r)}
            {@const isBusy = busy.has(r.app.name)}
            {@const mode = selectedMode(r)}
            {@const view = modeView(r.app, mode)}
            {@const res = checkRes[r.app.name]}
            <article class="app" class:selected={selected.has(r.app.name)} class:live={r.live} aria-label={`App ${r.app.name}`} animate:flip={{ duration: 150 }}>
              <header>
                <input type="checkbox" checked={selected.has(r.app.name)} onchange={() => toggle(r.app.name)} aria-label={`Select ${r.app.name}`} />
                <h3 class="name truncate" title={r.app.name}>{r.app.name}</h3>
                {#if r.matches.length > 1}<span class="badge" title="Matching processes">×{r.matches.length}</span>{/if}
                <span class="grow"></span>
                <span class="status {st.tone}"><span class="dot {st.dot}"></span>{st.label}</span>
              </header>

              {#if view.description}<p class="desc">{view.description}</p>{/if}

              {#if r.app.modes.length > 0}
                <div class="modes" role="radiogroup" aria-label={`Modes of ${r.app.name}`}>
                  <Layers size={13} />
                  {#each r.app.modes as m (m.name)}
                    <button
                      type="button"
                      role="radio"
                      aria-checked={mode === m.name}
                      class="mode"
                      class:on={mode === m.name}
                      class:running={r.live && r.primary?.mode === m.name}
                      title={m.description || `Run ${r.app.name} in ${m.name} mode`}
                      onclick={() => pickMode(r, m.name)}
                    >
                      {#if r.live && r.primary?.mode === m.name}<span class="dot ok"></span>{/if}
                      {m.name}
                      {#if m.isDefault}<span class="def">default</span>{/if}
                    </button>
                  {/each}
                  {#if r.app.command.length > 0}
                    <button type="button" role="radio" aria-checked={mode === ''} class="mode" class:on={mode === ''} title="The app's own command, without a mode" onclick={() => pickMode(r, '')}>base</button>
                  {/if}
                </div>
              {/if}

              <code class="cmd mono" title={commandLine(view.command)}>
                <span class="prompt">$</span><span class="truncate">{view.command.length > 0 ? commandLine(view.command) : 'no command (profile default)'}</span>
              </code>

              {#if view.envSources.length > 0}
                <div class="envchain" title="Env sources in priority order: later entries override earlier ones">
                  <KeyRound size={12} />
                  {#each view.envSources as src, i (i)}
                    {#if i > 0}<span class="sep">›</span>{/if}
                    <span class="src mono" class:file={!!src.file} title={src.file ? (src.optional ? `${src.file} (optional)` : src.file) : (src.vars ?? []).join('\n')}>{sourceLabel(src)}</span>
                  {/each}
                </div>
              {/if}

              <div class="tags">
                {#if r.app.type}<span class="badge">{r.app.type}</span>{/if}
                {#if r.app.auto}<span class="badge info" title="Auto-generated from detected run files; editing converts it to yours">auto</span>{/if}
                {#each r.app.ports as p (p.name)}
                  <span class="badge info" title={`port ${p.name}`}><Network size={12} />:{p.port}{p.name ? ` ${p.name}` : ''}</span>
                {/each}
                {#if r.app.readiness.length > 0}
                  <span class="badge" title={`Ready when logs match: ${r.app.readiness.join(', ')}`}><Radio size={12} />readiness</span>
                {/if}
                {#if r.app.healthHttp || r.app.healthTcp}
                  <span class="badge" title={r.app.healthHttp || r.app.healthTcp}><HeartPulse size={12} />health check</span>
                {/if}
                {#if r.app.restartPolicy && r.app.restartPolicy !== 'never'}
                  <span class="badge" title="Restart policy"><Repeat size={12} />{r.app.restartPolicy}</span>
                {/if}
                {#if r.app.autostart}<span class="badge accent"><Zap size={12} />autostart</span>{/if}
                {#if r.app.lifetime === 'session'}<span class="badge"><Timer size={12} />session</span>{/if}
                {#if r.primary?.stale}<span class="badge warn" title="Config changed since this process started">restart needed</span>{/if}
                {#if view.checks.length > 0}<span class="badge" title={view.checks.map((c) => c.name).join(', ')}><ListChecks size={12} />{view.checks.length} check{view.checks.length === 1 ? '' : 's'}</span>{/if}
              </div>

              {#if res}
                <section class="checks" class:bad={res.failed > 0} aria-label={`Check results for ${r.app.name}`}>
                  <header>
                    {#if res.failed > 0}<CircleX size={14} />{:else}<CircleCheck size={14} />{/if}
                    <strong>{res.passed}/{res.passed + res.failed} checks passed</strong>
                    {#if res.mode}<span class="muted">mode {res.mode}</span>{/if}
                    <span class="grow"></span>
                    <button class="btn icon ghost sm" aria-label="Dismiss check results" onclick={() => clearChecks(r.app.name)}><X size={12} /></button>
                  </header>
                  <ul>
                    {#each res.results as c (c.name)}
                      <li class:fail={!c.ok}>
                        {#if c.ok}<CircleCheck size={12} />{:else}<CircleX size={12} />{/if}
                        <span class="mono cname">{c.name}</span>
                        <span class="muted mono">{c.tcp ? c.tcp : `${c.method || 'GET'} ${c.status || '—'}`}</span>
                        <span class="muted">{c.durationMs}ms</span>
                        {#if !c.ok}<span class="why" title={[...c.failures, c.error].filter(Boolean).join('\n')}>{c.failures[0] ?? c.error}</span>{/if}
                      </li>
                    {/each}
                  </ul>
                </section>
              {/if}

              {#if r.app.dependsOn.length > 0}
                <div class="deps">
                  <Link2 size={12} />
                  <span>needs</span>
                  {#each r.app.dependsOn as d (d)}<span class="dep mono">{d}</span>{/each}
                </div>
              {/if}

              <footer>
                <div class="facts">
                  {#if r.primary && r.live}
                    <span title="Process id">pid <span class="mono">{r.primary.pid || '—'}</span></span>
                    {#if uptime(r.primary)}<span title="Uptime">{uptime(r.primary)}</span>{/if}
                    {#if r.primary.restarts > 0}<span title="Restarts">{r.primary.restarts} restart{r.primary.restarts === 1 ? '' : 's'}</span>{/if}
                  {:else if r.primary}
                    <span>last run {r.primary.exitedAt ? `${duration(now - r.primary.exitedAt)} ago` : 'finished'}</span>
                  {:else}
                    <span>never started</span>
                  {/if}
                </div>
                <div class="acts">
                  {#if r.live && view.checks.length > 0}
                    <button class="btn sm" title="Run the saved checks against the running process" aria-label={`Run checks for ${r.app.name}`} disabled={checking.has(r.app.name)} onclick={() => runChecks(r)}>
                      {#if checking.has(r.app.name)}<Spinner size={12} />{:else}<ListChecks size={14} />{/if}Checks
                    </button>
                  {/if}
                  {#if r.live && r.app.modes.length > 0 && (r.primary?.mode ?? '') !== mode}
                    <button class="btn sm" title={`Stop the running ${r.primary?.mode || 'base'} run and start ${mode || 'base'}`} aria-label={`Switch ${r.app.name} to ${mode || 'base'}`} disabled={isBusy} onclick={() => switchMode(r)}><ArrowLeftRight size={14} />Switch</button>
                  {/if}
                  {#if r.live}
                    <button class="btn sm icon" title="Restart" aria-label={`Restart ${r.app.name}`} disabled={isBusy} onclick={() => restart(r)}><RotateCw size={14} /></button>
                    <button class="btn sm danger" title="Stop" aria-label={`Stop ${r.app.name}`} disabled={isBusy} onclick={() => stop(r)}>
                      {#if isBusy}<Spinner size={12} />{:else}<Square size={12} />{/if}Stop
                    </button>
                  {:else}
                    <button class="btn sm primary" title={mode ? `Start in ${mode} mode` : 'Start'} aria-label={`Start ${r.app.name}`} disabled={isBusy} onclick={() => start(r)}>
                      {#if isBusy}<Spinner size={12} />{:else}<Play size={12} />{/if}Start{#if mode}<span class="modetag">{mode}</span>{/if}
                    </button>
                  {/if}
                  {#if r.primary}
                    <button class="btn sm" title="Open logs" aria-label={`Logs for ${r.app.name}`} onclick={() => logs(r.primary!)}><ScrollText size={14} />Logs</button>
                  {/if}
                </div>
              </footer>
            </article>
          {/each}
        </div>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .center {
    flex: 1;
    display: flex;
  }
  .grow {
    flex: 1;
  }
  .empty {
    flex: 1;
    display: flex;
  }
  .empty .empty-state {
    margin: auto;
  }
  .search {
    width: 280px;
    max-width: 100%;
  }
  .selall {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-1);
    font-size: var(--fs-sm);
    cursor: pointer;
  }
  .sel {
    display: inline-grid;
    place-items: center;
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    border-radius: 9px;
    background: color-mix(in srgb, var(--accent-fg) 24%, transparent);
    font-size: var(--fs-xs);
    font-variant-numeric: tabular-nums;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: var(--space-5);
  }
  .app {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-5);
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    min-width: 0;
    transition:
      border-color var(--dur-fast) var(--ease),
      box-shadow var(--dur-fast) var(--ease);
  }
  .app:hover {
    border-color: var(--border-strong);
  }
  .app.selected {
    border-color: color-mix(in srgb, var(--accent) 60%, var(--border));
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--accent) 35%, transparent);
  }
  .app > header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }
  .name {
    font-size: var(--fs-lg);
    font-weight: 600;
    min-width: 0;
  }
  .status {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: var(--fs-xs);
    font-weight: 500;
    white-space: nowrap;
    color: var(--text-2);
  }
  .status.ok {
    color: var(--ok);
  }
  .status.info {
    color: var(--info);
  }
  .status.err {
    color: var(--err);
  }
  .cmd {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 7px var(--space-4);
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    font-size: var(--fs-sm);
    color: var(--text-1);
    min-width: 0;
  }
  .prompt {
    color: var(--accent);
    flex: none;
    user-select: none;
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .desc {
    margin: calc(var(--space-2) * -1) 0 0;
    color: var(--text-2);
    font-size: var(--fs-sm);
    line-height: 1.45;
  }
  .modes {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    color: var(--text-2);
  }
  .mode {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 10px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-1);
    font-size: var(--fs-xs);
    font-family: var(--font-mono);
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease),
      background var(--dur-fast) var(--ease),
      color var(--dur-fast) var(--ease);
  }
  .mode:hover {
    border-color: var(--border-strong);
    color: var(--text-0);
  }
  .mode.on {
    border-color: color-mix(in srgb, var(--accent) 65%, var(--border));
    background: color-mix(in srgb, var(--accent) 14%, var(--bg-2));
    color: var(--text-0);
  }
  .mode.running:not(.on) {
    border-color: color-mix(in srgb, var(--ok) 45%, var(--border));
  }
  .mode:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .def {
    font-family: inherit;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-2);
  }
  .envchain {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 5px;
    color: var(--text-2);
    font-size: var(--fs-xs);
    min-width: 0;
  }
  .envchain .src {
    padding: 0 7px;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-1);
    line-height: 1.7;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .envchain .src.file {
    background: color-mix(in srgb, var(--info) 12%, var(--bg-3));
  }
  .envchain .sep {
    color: var(--text-2);
  }
  .modetag {
    margin-left: 4px;
    padding: 0 6px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--accent-fg) 22%, transparent);
    font-size: 10px;
    font-family: var(--font-mono);
  }
  .checks {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius);
    border: 1px solid color-mix(in srgb, var(--ok) 35%, var(--border));
    background: color-mix(in srgb, var(--ok) 6%, var(--bg-1));
    font-size: var(--fs-xs);
  }
  .checks.bad {
    border-color: color-mix(in srgb, var(--err) 40%, var(--border));
    background: color-mix(in srgb, var(--err) 6%, var(--bg-1));
  }
  .checks > header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    color: var(--ok);
  }
  .checks.bad > header {
    color: var(--err);
  }
  .checks > header strong {
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .checks ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .checks li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    color: var(--ok);
    min-width: 0;
  }
  .checks li.fail {
    color: var(--err);
  }
  .checks .cname {
    color: var(--text-0);
  }
  .checks .why {
    color: var(--err);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    flex: 1;
  }
  .tags:empty {
    display: none;
  }
  .badge :global(svg) {
    flex: none;
  }
  .deps {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .dep {
    padding: 0 7px;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-1);
    line-height: 1.7;
  }
  .app > footer {
    margin-top: auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding-top: var(--space-4);
    border-top: 1px solid var(--border);
    flex-wrap: wrap;
  }
  .facts {
    display: flex;
    gap: var(--space-4);
    color: var(--text-2);
    font-size: var(--fs-xs);
    min-width: 0;
    flex-wrap: wrap;
  }
  .acts {
    display: flex;
    gap: var(--space-2);
    margin-left: auto;
  }
  .app.skeleton {
    gap: var(--space-4);
  }
  .sk {
    height: 12px;
    border-radius: 5px;
    background: linear-gradient(90deg, var(--bg-3), var(--bg-hover), var(--bg-3));
    background-size: 200% 100%;
    animation: shimmer 1.4s linear infinite;
  }
  .w30 {
    width: 30%;
  }
  .w40 {
    width: 40%;
  }
  .w60 {
    width: 60%;
  }
  .w90 {
    width: 90%;
  }
  @keyframes shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .stack {
    background: var(--bg-1);
    border: 1px solid color-mix(in srgb, var(--ok) 35%, var(--border));
    border-radius: var(--radius-lg);
    padding: var(--space-4) var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .stack.bad {
    border-color: color-mix(in srgb, var(--err) 45%, var(--border));
  }
  .stack > header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    color: var(--ok);
  }
  .stack.bad > header {
    color: var(--err);
  }
  .stack > header strong {
    color: var(--text-0);
  }
  .order {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }
  .order li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .order :global(.arrow) {
    color: var(--text-2);
  }
  .step {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    padding: 4px 10px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    font-size: var(--fs-sm);
  }
  li.ok .step {
    border-color: color-mix(in srgb, var(--ok) 35%, transparent);
  }
  li.failed .step {
    border-color: color-mix(in srgb, var(--err) 50%, transparent);
    background: color-mix(in srgb, var(--err) 10%, transparent);
  }
  li.skipped .step {
    opacity: 0.6;
  }
  .st {
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .linkbtn {
    padding: 0;
    border: none;
    background: transparent;
    color: var(--accent);
    font-size: var(--fs-xs);
    text-decoration: underline;
    text-underline-offset: 2px;
    cursor: pointer;
  }
  .failure {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--space-3) var(--space-4);
    background: color-mix(in srgb, var(--err) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--err) 30%, transparent);
    border-radius: var(--radius);
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .failure .mono {
    color: var(--err);
    font-weight: 600;
  }
  .banner {
    flex: none;
  }
  .banner .grow {
    min-width: 0;
  }
  @media (max-width: 900px) {
    .grid {
      grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    }
    .search {
      width: 100%;
    }
  }
</style>
