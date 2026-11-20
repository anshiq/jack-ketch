<script lang="ts">
  import { getContext, onMount } from 'svelte';
  import ChartGantt from '@lucide/svelte/icons/chart-gantt';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Bot from '@lucide/svelte/icons/bot';
  import FileCog from '@lucide/svelte/icons/file-cog';
  import Lightbulb from '@lucide/svelte/icons/lightbulb';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import { AuditService, EventService, TimelineService, type AuditEntry, type DaemonEvent, type ErrorRate } from '../../lib/api';
  import { ROUTE_CONTEXT, type RouteView } from '../../lib/routes';
  import { router } from '../../lib/router.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { events as liveEvents } from '../../lib/state/events.svelte';
  import { clock } from '../../lib/state/clock.svelte';
  import { eventSentence, eventTone, procLabel, scopedEvents } from '../../lib/activity';
  import { WINDOWS, auditLabel, auditTone, eventLane, insights, ticks, type Mark } from '../../lib/timeline';
  import SegmentedControl from '../../lib/ui/SegmentedControl.svelte';
  import Switch from '../../lib/ui/Switch.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';

  const LANE_H = 34;
  const AXIS_H = 26;

  const route = getContext<RouteView | undefined>(ROUTE_CONTEXT);
  let windowKey = $state('1h');
  let showAgent = $state(true);
  let live = $state(true);
  let rate = $state<ErrorRate | null>(null);
  let history = $state<DaemonEvent[]>([]);
  let audit = $state<AuditEntry[]>([]);
  let error = $state('');
  let loading = $state(false);
  let width = $state(900);
  let hover = $state<{ x: number; y: number; marks: Mark[]; t: number } | null>(null);
  let cursorX = $state<number | null>(null);

  onMount(() => clock.retain());

  const labelW = $derived(width < 640 ? 112 : 190);

  const windowMs = $derived(WINDOWS.find((w) => w.value === windowKey)?.ms ?? 3_600_000);
  const wsId = $derived(scopeState.all ? '' : scopeState.workspaceId);

  async function load() {
    loading = true;
    try {
      const buckets = Math.max(20, Math.min(120, Math.floor((width - labelW) / 8)));
      const [r, ev, au] = await Promise.all([
        TimelineService.errorRate(wsId, windowMs, buckets),
        EventService.list(wsId, 1000, { order: 'desc' }),
        AuditService.list(wsId, 500)
      ]);
      rate = r;
      history = ev.events;
      audit = au;
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    if (route && !route.active) return;
    void windowMs;
    void wsId;
    void load();
    if (!live) return;
    const t = setInterval(() => {
      if (document.visibilityState === 'visible') void load();
    }, 15000);
    return () => clearInterval(t);
  });

  const from = $derived(rate?.from ?? clock.now - windowMs);
  const to = $derived(rate?.to ?? clock.now);
  const plotW = $derived(Math.max(120, width - labelW - 16));

  function x(ts: number) {
    return labelW + ((ts - from) / Math.max(1, to - from)) * plotW;
  }

  const allEvents = $derived.by(() => {
    const seen = new Set<number>();
    const out: DaemonEvent[] = [];
    for (const e of [...scopedEvents(liveEvents.items), ...scopedEvents(history)]) {
      if (seen.has(e.id) || e.type === 'process.stdout' || e.type === 'process.stderr') continue;
      seen.add(e.id);
      if (e.ts >= from && e.ts <= to + 60_000) out.push(e);
    }
    return out;
  });

  const marks = $derived.by(() => {
    const out: Mark[] = allEvents.map((e) => ({
      ts: e.ts,
      lane: eventLane(e),
      tone: eventTone(e),
      label: eventSentence(e),
      detail: e.type,
      href: e.processId ? `/processes/${e.processId}` : '/events',
      kind: 'event' as const
    }));
    if (showAgent) {
      for (const a of audit) {
        if (a.time < from || a.time > to + 60_000) continue;
        out.push({ ts: a.time, lane: 'agent', tone: auditTone(a), label: auditLabel(a), detail: `${a.result}${a.duration ? ` · ${a.duration}ms` : ''}`, href: '/audit', kind: 'audit' });
      }
    }
    return out.sort((a, b) => a.ts - b.ts);
  });

  const series = $derived(rate?.series ?? []);

  const lanes = $derived.by(() => {
    const out: { id: string; label: string; sub: string; status?: string; counts?: number[]; total?: number; icon?: 'agent' | 'config' }[] = [];
    if (showAgent) out.push({ id: 'agent', label: 'Agents & API', sub: `${marks.filter((m) => m.lane === 'agent').length} actions`, icon: 'agent' });
    if (marks.some((m) => m.lane === 'config')) out.push({ id: 'config', label: 'Config', sub: 'changes', icon: 'config' });
    const seen = new Set<string>();
    for (const s of series) {
      seen.add(s.processId);
      out.push({ id: s.processId, label: s.app || procLabel(s.processId), sub: s.total ? `${s.total} error line${s.total === 1 ? '' : 's'}` : s.status, status: s.status, counts: s.counts, total: s.total });
    }
    for (const m of marks) {
      if (m.lane === 'agent' || m.lane === 'config' || m.lane === 'other' || seen.has(m.lane)) continue;
      seen.add(m.lane);
      out.push({ id: m.lane, label: procLabel(m.lane), sub: 'removed' });
    }
    return out;
  });

  const laneIndex = $derived(new Map(lanes.map((l, i) => [l.id, i])));
  const height = $derived(AXIS_H + lanes.length * LANE_H + 8);
  const maxBucket = $derived(Math.max(1, ...series.flatMap((s) => s.counts)));
  const bucketW = $derived(rate && rate.series[0] ? plotW / rate.series[0].counts.length : 0);
  const tickList = $derived(ticks(from, to, Math.max(3, Math.floor(plotW / 110))));
  const found = $derived(rate ? insights(series, marks, rate.from, rate.bucketMs) : []);

  function timeLabel(t: number) {
    const span = to - from;
    return new Date(t).toLocaleTimeString(undefined, span > 6 * 3_600_000 ? { hour: '2-digit', minute: '2-digit' } : { hour: '2-digit', minute: '2-digit', second: span <= 15 * 60_000 ? '2-digit' : undefined });
  }

  function gap(ms: number) {
    if (ms < 60_000) return `${Math.round(ms / 1000)}s`;
    return `${Math.round(ms / 60_000)}m`;
  }

  function onMove(e: PointerEvent) {
    const el = e.currentTarget as HTMLElement;
    const rect = el.getBoundingClientRect();
    const px = e.clientX - rect.left;
    const py = e.clientY - rect.top;
    if (px < labelW) {
      cursorX = null;
      hover = null;
      return;
    }
    cursorX = px;
    const t = from + ((px - labelW) / plotW) * (to - from);
    const lane = lanes[Math.floor((py - AXIS_H) / LANE_H)];
    const near = lane ? marks.filter((m) => m.lane === lane.id && Math.abs(x(m.ts) - px) <= 6) : [];
    hover = { x: px, y: py, marks: near, t };
  }

  function onLeave() {
    cursorX = null;
    hover = null;
  }

  function toneVar(t: Mark['tone']) {
    return t === 'err' ? 'var(--err)' : t === 'warn' ? 'var(--warn)' : t === 'ok' ? 'var(--ok)' : t === 'info' ? 'var(--info)' : 'var(--text-2)';
  }
</script>

<div class="page">
  <header class="page-header">
    <div class="titles">
      <h1>Timeline</h1>
      <p class="subtitle">Restarts, crashes, config changes, agent actions and error spikes on one time axis, so you can see what happened right before something broke.</p>
    </div>
    <div class="actions">
      <Switch bind:checked={live} label="Live">Live</Switch>
      <button class="btn" onclick={() => void load()} disabled={loading} aria-label="Refresh timeline">
        {#if loading}<Spinner size={14} />{:else}<RefreshCw size={14} />{/if}Refresh
      </button>
    </div>
  </header>

  <div class="toolbar">
    <SegmentedControl size="sm" label="Time window" bind:value={windowKey} options={WINDOWS.map((w) => ({ value: w.value, label: w.label }))} />
    <Switch bind:checked={showAgent} label="Show agent actions">Agent actions</Switch>
    <span class="legend">
      <span><i class="lg bar"></i>error lines</span>
      <span><i class="lg dot" style="--c: var(--ok)"></i>started / healthy</span>
      <span><i class="lg dot" style="--c: var(--info)"></i>restart / action</span>
      <span><i class="lg dot" style="--c: var(--err)"></i>crash / failure</span>
    </span>
  </div>

  {#if error}
    <div class="banner err" role="alert"><TriangleAlert size={16} /><span>Could not load the timeline: {error}</span><button class="btn sm" onclick={() => void load()}>Retry</button></div>
  {/if}

  {#if found.length > 0}
    <div class="insights">
      {#each found as f (f.processId)}
        <div class="insight">
          <Lightbulb size={15} />
          <span>
            <strong>{procLabel(f.processId)}</strong> started logging errors at {timeLabel(f.spikeAt)}
            ({f.errors} line{f.errors === 1 ? '' : 's'} in the first minutes){#if f.cause}, {gap(f.gapMs ?? 0)} after <em>{f.cause.label}</em>{:else}, with nothing recorded just before it{/if}.
          </span>
          <a class="btn sm ghost" href="/processes/{f.processId}?tab=errors" use:router.link={`/processes/${f.processId}?tab=errors`}>Errors<ArrowRight size={13} /></a>
        </div>
      {/each}
    </div>
  {/if}

  <div class="chart card" bind:clientWidth={width}>
    {#if !rate && !error}
      <div class="sk">
        {#each Array(4) as _, i (i)}<Skeleton width="100%" height="26px" />{/each}
      </div>
    {:else if lanes.length === 0}
      <div class="empty-state">
        <span class="icon-wrap"><ChartGantt size={20} /></span>
        <h3>Nothing happened in this window</h3>
        <p>Start a process or widen the time window. Events and errors from loaded workspaces show up here.</p>
      </div>
    {:else}
      <div class="plot" role="presentation" onpointermove={onMove} onpointerleave={onLeave}>
      <svg role="img" aria-label="Timeline of processes, agent actions and errors" {width} {height}>
        {#each tickList as t (t)}
          <line class="grid" x1={x(t)} x2={x(t)} y1={AXIS_H - 6} y2={height} />
          <text class="tick" x={x(t)} y={14} text-anchor="middle">{timeLabel(t)}</text>
        {/each}
        {#each lanes as lane, i (lane.id)}
          {@const y0 = AXIS_H + i * LANE_H}
          <rect class="lane" class:alt={i % 2 === 1} x={0} y={y0} width={width} height={LANE_H} />
          <foreignObject x={8} y={y0} width={labelW - 16} height={LANE_H}>
            <div class="lane-label" title={lane.label}>
              {#if lane.icon === 'agent'}<Bot size={13} />{:else if lane.icon === 'config'}<FileCog size={13} />{:else}<span class="st" data-status={lane.status ?? 'gone'}></span>{/if}
              <span class="ln">{lane.label}</span>
              <span class="ls">{lane.sub}</span>
            </div>
          </foreignObject>
          {#if lane.counts}
            {#each lane.counts as c, b (b)}
              {#if c > 0}
                {@const h = Math.max(3, (c / maxBucket) * (LANE_H - 8))}
                <rect class="errbar" x={labelW + b * bucketW + 0.5} y={y0 + LANE_H - 4 - h} width={Math.max(1, bucketW - 1)} height={h} opacity={0.35 + 0.65 * (c / maxBucket)}>
                  <title>{c} error line{c === 1 ? '' : 's'}</title>
                </rect>
              {/if}
            {/each}
          {/if}
        {/each}
        {#each marks as m, i (i)}
          {@const li = laneIndex.get(m.lane)}
          {#if li !== undefined}
            {@const cy = AXIS_H + li * LANE_H + LANE_H / 2}
            <a href={m.href ?? '/events'} aria-label="{new Date(m.ts).toLocaleTimeString()}: {m.label}" onclick={(e) => { e.preventDefault(); void router.navigate(m.href ?? '/events'); }}>
              {#if m.kind === 'audit'}
                <rect class="mk" x={x(m.ts) - 4} y={cy - 4} width={8} height={8} transform="rotate(45 {x(m.ts)} {cy})" fill={toneVar(m.tone)} />
              {:else}
                <circle class="mk" cx={x(m.ts)} cy={cy} r={5} fill={toneVar(m.tone)} />
              {/if}
            </a>
          {/if}
        {/each}
        <line class="now" x1={x(to)} x2={x(to)} y1={AXIS_H - 6} y2={height} />
        {#if cursorX !== null}
          <line class="cursor" x1={cursorX} x2={cursorX} y1={AXIS_H - 6} y2={height} />
        {/if}
      </svg>
      </div>
      {#if hover}
        <div class="tip" style="left: {Math.min(hover.x + 12, width - 280)}px; top: {hover.y + 14}px">
          <div class="tip-time">{new Date(hover.t).toLocaleTimeString()}</div>
          {#if hover.marks.length}
            {#each hover.marks.slice(0, 5) as m, i (i)}
              <div class="tip-row"><i class="lg dot" style="--c: {toneVar(m.tone)}"></i><span>{m.label}</span></div>
              <div class="tip-sub mono">{new Date(m.ts).toLocaleTimeString()} · {m.detail}</div>
            {/each}
            {#if hover.marks.length > 5}<div class="tip-sub">+{hover.marks.length - 5} more</div>{/if}
          {:else}
            <div class="tip-sub">Hover a marker for details, click to open it.</div>
          {/if}
        </div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .toolbar {
    flex-wrap: wrap;
  }
  .legend {
    margin-left: auto;
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-4);
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .legend span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .lg {
    display: inline-block;
    flex: none;
  }
  .lg.dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--c);
  }
  .lg.bar {
    width: 6px;
    height: 10px;
    border-radius: 1px;
    background: var(--err);
    opacity: 0.7;
  }
  .insights {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .insight {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius);
    border: 1px solid color-mix(in srgb, var(--warn) 40%, var(--border));
    background: color-mix(in srgb, var(--warn) 8%, var(--bg-1));
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  .insight > :global(svg) {
    color: var(--warn);
    flex: none;
  }
  .insight span {
    flex: 1;
    min-width: 0;
  }
  .insight strong {
    color: var(--text-0);
  }
  .insight em {
    color: var(--text-0);
    font-style: normal;
    font-weight: 500;
  }
  .chart {
    position: relative;
    padding: 0;
    overflow: hidden;
  }
  .sk {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: var(--space-4);
  }
  svg {
    display: block;
    user-select: none;
  }
  .plot {
    position: relative;
  }
  svg a {
    cursor: pointer;
  }
  svg a:focus-visible .mk {
    stroke: var(--accent);
    stroke-width: 3;
  }
  .grid {
    stroke: var(--border);
    stroke-dasharray: 2 4;
  }
  .tick {
    fill: var(--text-2);
    font-size: 10px;
    font-variant-numeric: tabular-nums;
  }
  .lane {
    fill: transparent;
  }
  .lane.alt {
    fill: color-mix(in srgb, var(--bg-2) 60%, transparent);
  }
  .lane-label {
    height: 100%;
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: var(--fs-xs);
    color: var(--text-1);
    overflow: hidden;
  }
  .lane-label :global(svg) {
    flex: none;
    color: var(--text-2);
  }
  .ln {
    color: var(--text-0);
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }
  .ls {
    margin-left: auto;
    color: var(--text-2);
    white-space: nowrap;
    font-size: 10px;
  }
  .st {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--text-2);
    flex: none;
  }
  .st[data-status='running'] {
    background: var(--ok);
  }
  .st[data-status='starting'],
  .st[data-status='stopping'] {
    background: var(--warn);
  }
  .st[data-status='failed'],
  .st[data-status='crashed'] {
    background: var(--err);
  }
  .errbar {
    fill: var(--err);
  }
  .mk {
    stroke: var(--bg-1);
    stroke-width: 1.5;
  }
  .now {
    stroke: var(--accent);
    stroke-width: 1;
    opacity: 0.6;
  }
  .cursor {
    stroke: var(--text-2);
    stroke-width: 1;
    pointer-events: none;
  }
  .tip {
    position: absolute;
    z-index: 5;
    width: 270px;
    padding: 8px 10px;
    border-radius: var(--radius);
    border: 1px solid var(--border-strong, var(--border));
    background: var(--bg-1);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.25);
    font-size: var(--fs-xs);
    color: var(--text-1);
    pointer-events: none;
  }
  .tip-time {
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
    margin-bottom: 4px;
  }
  .tip-row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    color: var(--text-0);
  }
  .tip-sub {
    margin: 0 0 4px 14px;
    color: var(--text-2);
    font-size: 10px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (max-width: 760px) {
    .legend {
      margin-left: 0;
    }
  }
</style>
