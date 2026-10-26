<script lang="ts">
  import { getContext } from 'svelte';
  import Cpu from '@lucide/svelte/icons/cpu';
  import MemoryStick from '@lucide/svelte/icons/memory-stick';
  import Activity from '@lucide/svelte/icons/activity';
  import { resources } from '../lib/state/resources.svelte';
  import { ROUTE_CONTEXT, type RouteView } from '../lib/routes';
  import { bytes } from '../lib/format';
  import MiniChart from './MiniChart.svelte';

  let { processId, live }: { processId: string; live: boolean } = $props();

  const CAPACITY = 60;
  const route = getContext<RouteView | undefined>(ROUTE_CONTEXT);

  $effect(() => {
    if (!live || (route && !route.active)) return;
    return resources.watch([processId], `resources:${processId}`);
  });

  const history = $derived(resources.history(processId));
  const latest = $derived(resources.live(processId));
  const cpu = $derived(history.slice(-CAPACITY).map((s) => s.cpuPercent));
  const mem = $derived(history.slice(-CAPACITY).map((s) => s.rssBytes || s.memoryBytes));
  const loaded = $derived(latest !== null);
  const failed = false;
  const threads = $derived(latest?.threads ?? 0);
  const fds = $derived(latest?.openFds ?? 0);
  const children = $derived(latest?.children ?? 0);
  const ports = $derived(latest?.ports ?? []);

  const cpuNow = $derived(cpu[cpu.length - 1] ?? 0);
  const memNow = $derived(mem[mem.length - 1] ?? 0);
  const memPeak = $derived(Math.max(0, ...mem));
</script>

<div class="res">
  {#if !live}
    <div class="empty">
      <Activity size={20} />
      <h4>Not running</h4>
      <p>Resource usage is sampled while the process is running.</p>
    </div>
  {:else if !loaded}
    <div class="empty muted">{failed ? 'Resource usage is unavailable for this process.' : 'Sampling…'}</div>
  {:else}
    <div class="card metric">
      <div class="head">
        <span class="label"><Cpu size={14} /> CPU</span>
        <span class="big">{cpuNow.toFixed(1)}<small>%</small></span>
      </div>
      <MiniChart values={cpu} max={100} color="var(--accent)" capacity={CAPACITY} />
      <div class="sub">of one core · last {Math.min(cpu.length * 2, CAPACITY * 2)}s</div>
    </div>
    <div class="card metric">
      <div class="head">
        <span class="label"><MemoryStick size={14} /> Memory (RSS)</span>
        <span class="big">{bytes(memNow)}</span>
      </div>
      <MiniChart values={mem} color="var(--info)" capacity={CAPACITY} />
      <div class="sub">peak {bytes(memPeak)}</div>
    </div>
    <div class="stat-row">
      <div class="mini"><span class="k">Threads</span><span class="v">{threads}</span></div>
      <div class="mini"><span class="k">Open files</span><span class="v">{fds}</span></div>
      <div class="mini"><span class="k">Children</span><span class="v">{children}</span></div>
    </div>
    <div class="card ports">
      <span class="label">Listening ports</span>
      {#if ports.length}
        <span class="plist">{#each ports as p (p)}<span class="badge mono">:{p}</span>{/each}</span>
      {:else}
        <span class="muted">none</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .res {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .card {
    padding: var(--space-4) var(--space-5);
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }
  .metric {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
  }
  .label {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-micro);
    font-weight: 600;
    letter-spacing: 0;
    color: var(--text-2);
  }
  .big {
    font-size: var(--fs-2xl);
    font-weight: 600;
    letter-spacing: -0.02em;
    font-variant-numeric: tabular-nums;
  }
  .big small {
    font-size: var(--fs-md);
    color: var(--text-2);
    margin-left: 2px;
  }
  .sub {
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .stat-row {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--space-4);
  }
  .mini {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--space-3) var(--space-4);
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }
  .mini .k {
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .mini .v {
    font-size: var(--fs-lg);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .ports {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
  }
  .plist {
    display: inline-flex;
    gap: 4px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    color: var(--text-2);
    text-align: center;
    font-size: var(--fs-sm);
  }
  .empty h4 {
    color: var(--text-0);
    font-size: var(--fs-md);
    margin-top: var(--space-2);
  }
  .empty p {
    margin: 0;
  }
</style>
