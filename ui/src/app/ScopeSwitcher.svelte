<script lang="ts">
  import FolderOpen from '@lucide/svelte/icons/folder-open';
  import ChevronsUpDown from '@lucide/svelte/icons/chevrons-up-down';
  import Check from '@lucide/svelte/icons/check';
  import Layers from '@lucide/svelte/icons/layers';
  import Plus from '@lucide/svelte/icons/plus';
  import Search from '@lucide/svelte/icons/search';
  import { scopeState } from '../lib/state/scope.svelte';
  import { processes } from '../lib/state/processes.svelte';
  import { isLive } from '../components/processView';

  interface Option {
    id: string;
    name: string;
    path: string;
    missing: boolean;
    running: number;
  }

  let open = $state(false);
  let path = $state('');
  let query = $state('');
  let active = $state(0);
  let root: HTMLDivElement | null = $state(null);
  let filterInput: HTMLInputElement | null = $state(null);
  let listEl: HTMLDivElement | null = $state(null);

  const runningBy = $derived.by(() => {
    const m = new Map<string, number>();
    for (const p of processes.list) {
      if (isLive(p)) m.set(p.workspaceId, (m.get(p.workspaceId) ?? 0) + 1);
    }
    return m;
  });

  const totalRunning = $derived([...runningBy.values()].reduce((n, c) => n + c, 0));

  const options = $derived.by((): Option[] => {
    const q = query.trim().toLowerCase();
    const rows = scopeState.workspaces.map((w) => ({
      id: w.id,
      name: scopeState.workspaceLabel(w.id),
      path: w.path,
      missing: w.missing,
      running: runningBy.get(w.id) ?? 0
    }));
    const hit = q ? rows.filter((r) => r.name.toLowerCase().includes(q) || r.path.toLowerCase().includes(q)) : rows;
    const all: Option[] = q ? [] : [{ id: '', name: 'All workspaces', path: 'Show everything across projects', missing: false, running: totalRunning }];
    return [...all, ...hit];
  });

  $effect(() => {
    void query;
    active = 0;
  });

  function toggle() {
    open = !open;
    if (open) {
      scopeState.error = '';
      query = '';
      const idx = options.findIndex((o) => o.id === scopeState.workspaceId);
      active = idx < 0 ? 0 : idx;
      void scopeState.refresh();
      queueMicrotask(() => {
        filterInput?.focus();
        listEl?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' });
      });
    }
  }

  function choose(id: string) {
    scopeState.select(id);
    open = false;
  }

  function move(delta: number) {
    if (options.length === 0) return;
    active = (active + delta + options.length) % options.length;
    queueMicrotask(() => listEl?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' }));
  }

  function onFilterKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      move(1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      move(-1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const opt = options[active];
      if (opt && !opt.missing) choose(opt.id);
    }
  }

  async function submit(e: Event) {
    e.preventDefault();
    if (!path.trim()) return;
    const ok = await scopeState.open(path);
    if (ok) {
      path = '';
      open = false;
    }
  }

  function onWindowClick(e: MouseEvent) {
    if (open && root && !root.contains(e.target as Node)) open = false;
  }

  function onWindowKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') open = false;
  }

  function shorten(p: string): string {
    if (p.length <= 44) return p;
    return '…' + p.slice(p.length - 43);
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={onWindowKey} />

<div class="switcher" bind:this={root}>
  <button class="trigger" class:open onclick={toggle} aria-haspopup="listbox" aria-expanded={open} aria-label="Workspace scope">
    <span class="ico">
      {#if scopeState.all}<Layers size={14} />{:else}<FolderOpen size={14} />{/if}
    </span>
    <span class="text">
      <span class="name">{scopeState.label}</span>
      {#if !scopeState.all && scopeState.path}<span class="path">{shorten(scopeState.path)}</span>{/if}
    </span>
    {#if (scopeState.all ? totalRunning : (runningBy.get(scopeState.workspaceId) ?? 0)) > 0}
      <span class="live" title="Running processes in this scope">{scopeState.all ? totalRunning : runningBy.get(scopeState.workspaceId)}</span>
    {/if}
    <ChevronsUpDown size={14} />
  </button>

  {#if open}
    <div class="panel">
      <div class="filter">
        <Search size={14} />
        <input
          bind:this={filterInput}
          bind:value={query}
          onkeydown={onFilterKey}
          placeholder="Filter workspaces"
          aria-label="Filter workspaces"
          aria-controls="scope-options"
          aria-activedescendant={options[active] ? `scope-opt-${active}` : undefined}
          spellcheck="false"
          autocomplete="off"
        />
        <span class="count">{scopeState.workspaces.length}</span>
      </div>
      <div class="list" role="listbox" id="scope-options" aria-label="Workspaces" bind:this={listEl}>
        {#each options as o, i (o.id)}
          <button
            type="button"
            class="item"
            class:active={i === active}
            class:all={o.id === ''}
            role="option"
            id={`scope-opt-${i}`}
            data-index={i}
            aria-selected={scopeState.workspaceId === o.id}
            disabled={o.missing}
            onmouseenter={() => (active = i)}
            onclick={() => choose(o.id)}
          >
            {#if o.id === ''}<Layers size={14} />{:else}<FolderOpen size={14} />{/if}
            <span class="col">
              <span class="n">{o.name}</span>
              <span class="p" class:mono={o.id !== ''} title={o.path}>{o.path}</span>
            </span>
            {#if o.missing}<span class="badge warn">missing</span>{/if}
            {#if o.running > 0}<span class="run"><span class="dot ok"></span>{o.running}</span>{/if}
            {#if scopeState.workspaceId === o.id}<Check size={14} class="tick" />{/if}
          </button>
        {/each}
        {#if options.length === 0}
          <div class="none">{query ? `No workspace matches “${query}”.` : 'No workspaces yet. Open a project folder below.'}</div>
        {/if}
      </div>
      <form class="open-form" onsubmit={submit}>
        <div class="section">Open workspace</div>
        <div class="row">
          <input bind:value={path} placeholder="/path/to/project" aria-label="Workspace path" spellcheck="false" />
          <button class="btn primary" type="submit" disabled={scopeState.resolving || !path.trim()}>
            <Plus size={14} />{scopeState.resolving ? 'Opening…' : 'Open'}
          </button>
        </div>
        {#if scopeState.error}<div class="err">{scopeState.error}</div>{/if}
      </form>
    </div>
  {/if}
</div>

<style>
  .switcher {
    position: relative;
  }
  .trigger {
    gap: 10px;
    height: 32px;
    padding: 0 10px 0 4px;
    min-width: 220px;
    max-width: 380px;
    justify-content: flex-start;
    background: var(--bg-2);
    text-align: left;
    color: var(--text-1);
  }
  .trigger.open {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-subtle);
  }
  .ico {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border-radius: 6px;
    background: var(--accent-subtle);
    color: var(--accent);
    flex: none;
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
    line-height: 1.2;
  }
  .name {
    color: var(--text-0);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .path {
    font-size: var(--fs-micro);
    color: var(--text-2);
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .live,
  .run {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex: none;
    padding: 0 6px;
    border-radius: 3px;
    font-family: var(--font-mono);
    font-size: var(--fs-micro);
    line-height: 1.7;
    background: color-mix(in srgb, var(--ok) 14%, transparent);
    color: var(--ok);
  }
  .run .dot {
    width: 6px;
    height: 6px;
    box-shadow: none;
  }
  .panel {
    position: absolute;
    top: calc(100% + 8px);
    left: 0;
    width: 440px;
    max-width: calc(100vw - 24px);
    max-height: min(560px, 75vh);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    z-index: 60;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .filter {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    height: 40px;
    flex: none;
    border-bottom: 1px solid var(--border);
    color: var(--text-2);
  }
  .filter input {
    flex: 1;
    min-width: 0;
    height: 100%;
    padding: 0;
    border: none;
    background: transparent;
    box-shadow: none;
    font-size: var(--fs-md);
  }
  .filter input:focus,
  .filter input:focus-visible {
    border: none;
    box-shadow: none;
    outline: none;
  }
  .filter:focus-within {
    color: var(--accent);
  }
  .count {
    font-family: var(--font-mono);
    font-size: var(--fs-micro);
    color: var(--text-2);
  }
  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--space-2);
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .section {
    font-size: var(--fs-xs);
    font-weight: 600;
    color: var(--text-2);
    padding: 0 0 6px;
  }
  .item {
    width: 100%;
    height: auto;
    min-height: 36px;
    flex: none;
    justify-content: flex-start;
    gap: 10px;
    border-color: transparent;
    background: transparent;
    padding: 6px 8px;
    text-align: left;
    color: var(--text-1);
    font-weight: 400;
  }
  .item.all {
    margin-bottom: 4px;
  }
  .item.active:not(:disabled),
  .item:hover:not(:disabled) {
    background: var(--bg-hover);
    border-color: transparent;
  }
  .item[aria-selected='true'] {
    background: var(--accent-subtle);
    color: var(--text-0);
  }
  .item[aria-selected='true'].active {
    background: color-mix(in srgb, var(--accent) 16%, transparent);
  }
  .col {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    line-height: 1.3;
  }
  .n {
    color: var(--text-0);
    font-weight: 550;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .p {
    font-size: var(--fs-xs);
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item :global(.tick) {
    color: var(--accent);
    flex: none;
  }
  .none {
    padding: 14px 8px;
    color: var(--text-2);
    font-size: var(--fs-sm);
    text-align: center;
  }
  .open-form {
    flex: none;
    padding: 10px 12px 12px;
    border-top: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-0) 45%, var(--bg-1));
  }
  .row {
    display: flex;
    gap: var(--space-3);
  }
  .row input {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
  }
  .row .btn {
    height: var(--input-h);
  }
  .err {
    color: var(--err);
    font-size: var(--fs-xs);
    padding-top: var(--space-2);
  }
</style>
