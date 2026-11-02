<script lang="ts">
  import { onMount } from 'svelte';
  import File from '@lucide/svelte/icons/file';
  import Folder from '@lucide/svelte/icons/folder';
  import Spinner from './Spinner.svelte';
  import { SystemService } from '../api';
  import { joinPathEntry, pathSuggestionBase, splitPathInput, type DirectoryEntry } from '../pathSuggest';

  let {
    input = null,
    baseDir = '',
    dirsOnly = false,
    limit = 100,
    absolute = false,
    onpick
  }: {
    input?: HTMLInputElement | null;
    baseDir?: string;
    dirsOnly?: boolean;
    limit?: number;
    absolute?: boolean;
    onpick?: (next: string) => void;
  } = $props();

  const uid = `pth-${Math.random().toString(36).slice(2, 9)}`;
  let open = $state(false);
  let entries = $state<DirectoryEntry[]>([]);
  let truncated = $state(false);
  let loading = $state(false);
  let active = $state(-1);
  let resolvedDir = $state('');
  let popover: HTMLDivElement | null = $state(null);
  let pos = $state({ left: 0, top: 0, bottom: 0, width: 0, maxHeight: 300, above: false });
  let timer: ReturnType<typeof setTimeout> | null = null;
  let fetchSeq = 0;
  let suppressInput = false;

  const visible = $derived(open && (loading || entries.length > 0));

  $effect(() => {
    const el = input;
    if (!el) return;
    el.setAttribute('role', 'combobox');
    el.setAttribute('aria-autocomplete', 'list');
    el.setAttribute('aria-haspopup', 'listbox');
    el.setAttribute('aria-controls', `${uid}-list`);
    const onFocus = () => show();
    const onInput = () => {
      if (suppressInput) return;
      show(90);
    };
    const onKey = (e: KeyboardEvent) => keys(e);
    el.addEventListener('focus', onFocus);
    el.addEventListener('input', onInput);
    el.addEventListener('keydown', onKey);
    if (document.activeElement === el) show();
    return () => {
      el.removeEventListener('focus', onFocus);
      el.removeEventListener('input', onInput);
      el.removeEventListener('keydown', onKey);
      el.removeAttribute('role');
      el.removeAttribute('aria-autocomplete');
      el.removeAttribute('aria-haspopup');
      el.removeAttribute('aria-controls');
      el.removeAttribute('aria-expanded');
      el.removeAttribute('aria-activedescendant');
    };
  });

  $effect(() => {
    if (!input) return;
    input.setAttribute('aria-expanded', visible ? 'true' : 'false');
    if (visible && active >= 0) input.setAttribute('aria-activedescendant', `${uid}-${active}`);
    else input.removeAttribute('aria-activedescendant');
  });

  $effect(() => {
    void baseDir;
    void dirsOnly;
    void absolute;
    if (open && input === document.activeElement) schedule(0);
  });

  onMount(() => {
    const outside = (e: PointerEvent) => {
      if (!visible) return;
      const target = e.target as Node;
      if (input?.contains(target) || popover?.contains(target)) return;
      close();
    };
    const onScroll = () => {
      if (visible) updatePos();
    };
    document.addEventListener('pointerdown', outside, true);
    window.addEventListener('resize', onScroll);
    window.addEventListener('scroll', onScroll, true);
    return () => {
      document.removeEventListener('pointerdown', outside, true);
      window.removeEventListener('resize', onScroll);
      window.removeEventListener('scroll', onScroll, true);
      if (timer) clearTimeout(timer);
    };
  });

  function show(delay = 0) {
    const el = input;
    if (!el || el.disabled || el.readOnly) return;
    open = true;
    updatePos();
    schedule(delay);
  }

  function close() {
    open = false;
    active = -1;
    if (timer) clearTimeout(timer);
    timer = null;
  }

  function schedule(delay: number) {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      void load();
    }, delay);
  }

  async function load() {
    const el = input;
    if (!el || el.disabled || el.readOnly) return;
    const typed = el.value;
    const { dir, prefix } = splitPathInput(typed);
    const seq = ++fetchSeq;
    loading = true;
    try {
      const res = await SystemService.listDirectory({ dir, prefix, baseDir, dirsOnly, limit });
      if (seq !== fetchSeq) return;
      resolvedDir = res.dir || '';
      entries = res.entries ?? [];
      truncated = res.truncated === true;
      active = -1;
      updatePos();
    } catch {
      if (seq === fetchSeq) {
        entries = [];
        truncated = false;
      }
    } finally {
      if (seq === fetchSeq) loading = false;
    }
  }

  function updatePos() {
    const el = input;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const below = window.innerHeight - r.bottom - 8;
    const above = r.top - 8;
    const wantAbove = below < 180 && above > below;
    const width = Math.max(260, Math.min(r.width, window.innerWidth - 16));
    const left = Math.max(8, Math.min(r.left, window.innerWidth - width - 8));
    pos = {
      left,
      width,
      top: r.bottom + 4,
      bottom: window.innerHeight - r.top + 4,
      maxHeight: Math.max(120, Math.min(320, wantAbove ? above : below)),
      above: wantAbove
    };
  }

  function keys(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      if (visible) {
        e.preventDefault();
        e.stopPropagation();
        close();
      }
      return;
    }
    if (!visible || entries.length === 0) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      active = active < entries.length - 1 ? active + 1 : 0;
      scrollActive();
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      active = active > 0 ? active - 1 : entries.length - 1;
      scrollActive();
      return;
    }
    if (e.key === 'Tab') {
      if (active < 0) {
        close();
        return;
      }
      e.preventDefault();
      choose(active);
      return;
    }
    if (e.key === 'Enter') {
      if (active < 0) return;
      e.preventDefault();
      e.stopPropagation();
      choose(active);
    }
  }

  function scrollActive() {
    if (active < 0) return;
    queueMicrotask(() => {
      popover?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' });
    });
  }

  function choose(index: number) {
    const entry = entries[index];
    const el = input;
    if (!entry || !el) return;
    const base = pathSuggestionBase(el.value, resolvedDir, absolute);
    const next = joinPathEntry(base, entry.name, entry.isDir);
    setValue(next);
    onpick?.(next);
    active = -1;
    if (entry.isDir) {
      open = true;
      updatePos();
      schedule(0);
    } else {
      close();
      el.focus();
      el.setSelectionRange(next.length, next.length);
    }
  }

  function setValue(next: string) {
    const el = input;
    if (!el) return;
    suppressInput = true;
    el.value = next;
    el.dispatchEvent(new Event('input', { bubbles: true }));
    suppressInput = false;
  }
</script>

{#if visible}
  <div
    class="suggest"
    id={`${uid}-list`}
    role="listbox"
    tabindex="-1"
    bind:this={popover}
    style:left={`${pos.left}px`}
    style:width={`${pos.width}px`}
    style:max-height={`${pos.maxHeight}px`}
    style:top={pos.above ? 'auto' : `${pos.top}px`}
    style:bottom={pos.above ? `${pos.bottom}px` : 'auto'}
    onmousedown={(e) => e.preventDefault()}
  >
    {#if loading && entries.length === 0}
      <div class="empty"><Spinner size={12} />Looking…</div>
    {:else}
      {#each entries as entry, i (entry.name)}
        <div
          class="row"
          class:active={i === active}
          role="option"
          aria-selected={i === active}
          id={`${uid}-${i}`}
          data-index={i}
          tabindex="-1"
          onmouseenter={() => (active = i)}
          onmousedown={(e) => {
            e.preventDefault();
            choose(i);
          }}
        >
          <span class="ico" class:dir={entry.isDir}>{#if entry.isDir}<Folder size={14} />{:else}<File size={14} />{/if}</span>
          <span class="name mono">{entry.name}</span>
          {#if entry.isDir}<span class="kind">folder</span>{/if}
        </div>
      {/each}
      {#if loading}
        <div class="foot"><Spinner size={11} />Refreshing…</div>
      {:else if truncated}
        <div class="foot">More matches not shown. Keep typing to narrow.</div>
      {/if}
    {/if}
  </div>
{/if}

<style>
  .suggest {
    position: fixed;
    z-index: var(--z-popover, 900);
    overflow: auto;
    padding: 4px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    background: var(--bg-2);
    box-shadow: var(--shadow-pop);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 28px;
    padding: 3px 8px;
    border-radius: calc(var(--radius) - 2px);
    color: var(--text-1);
    cursor: pointer;
    user-select: none;
  }
  .row.active {
    background: var(--accent-subtle);
    color: var(--text-0);
  }
  .ico {
    display: inline-flex;
    flex: none;
    color: var(--text-2);
  }
  .ico.dir {
    color: var(--accent);
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-xs);
  }
  .kind {
    flex: none;
    font-size: 10px;
    color: var(--text-2);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .empty,
  .foot {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 9px;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .foot {
    border-top: 1px solid var(--border);
    margin-top: 4px;
  }
</style>
