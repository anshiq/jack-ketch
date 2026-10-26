<script lang="ts" module>
  export type DetailTab = 'logs' | 'info' | 'env' | 'diagnose' | 'probe' | 'resources' | 'terminal';
</script>

<script lang="ts">
  import { onMount } from 'svelte';
  import X from '@lucide/svelte/icons/x';
  import Copy from '@lucide/svelte/icons/copy';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import Square from '@lucide/svelte/icons/square';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import Info from '@lucide/svelte/icons/info';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import Activity from '@lucide/svelte/icons/activity';
  import SquareTerminal from '@lucide/svelte/icons/square-terminal';
  import SearchX from '@lucide/svelte/icons/search-x';
  import Stethoscope from '@lucide/svelte/icons/stethoscope';
  import Send from '@lucide/svelte/icons/send';
  import BookmarkPlus from '@lucide/svelte/icons/bookmark-plus';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import { AssistService } from '../lib/api';
  import type { Diagnosis, SaveSuggestion } from '../lib/api/types';
  import DiagnosePanel from './assist/DiagnosePanel.svelte';
  import ProbePanel from './assist/ProbePanel.svelte';
  import SaveRunDialog from './assist/SaveRunDialog.svelte';
  import { actorInfo, isFailing, isHealthyRun } from './assist/assistView';
  import { toasts, toastError } from '../lib/toasts.svelte';
  import { processes } from '../lib/state/processes.svelte';
  import type { Component } from 'svelte';
  import type { Process } from '../lib/api/types';
  import { scopeState } from '../lib/state/scope.svelte';
  import ProcessStatus from './ProcessStatus.svelte';
  import ProcessLogs from './ProcessLogs.svelte';
  import ProcessInfo from './ProcessInfo.svelte';
  import ProcessEnv from './ProcessEnv.svelte';
  import ProcessResources from './ProcessResources.svelte';
  import ActionMenu from './ActionMenu.svelte';
  import { processMenu } from './processMenu';
  import { procActions } from './processActions.svelte';
  import { commandLine, displayName, isLive } from './processView';

  let {
    processId,
    process,
    tab = $bindable('logs'),
    onClose,
    onCopyId,
    onRemoved,
    onTabChange,
    fullPage = false
  }: {
    processId: string;
    process?: Process;
    tab?: DetailTab;
    onClose: () => void;
    onCopyId: (id: string) => void;
    onRemoved: (ids: string[]) => void;
    onTabChange?: (t: DetailTab) => void;
    fullPage?: boolean;
  } = $props();

  let waited = $state(false);
  let TerminalTab = $state<Component<{ processId: string; interactive: boolean }> | null>(null);
  let terminalError = $state('');
  let loadingTerminal = false;

  async function loadTerminal() {
    if (TerminalTab || loadingTerminal) return;
    loadingTerminal = true;
    terminalError = '';
    try {
      TerminalTab = (await import('./Terminal.svelte')).default;
    } catch (err) {
      terminalError = err instanceof Error ? err.message : String(err);
    } finally {
      loadingTerminal = false;
    }
  }

  $effect(() => {
    if (tab === 'terminal') void loadTerminal();
  });
  const tabs: { id: DetailTab; label: string; icon: Component<{ size?: number | string }> }[] = [
    { id: 'logs', label: 'Logs', icon: ScrollText },
    { id: 'info', label: 'Overview', icon: Info },
    { id: 'env', label: 'Env', icon: KeyRound },
    { id: 'diagnose', label: 'Diagnose', icon: Stethoscope },
    { id: 'probe', label: 'Probe', icon: Send },
    { id: 'resources', label: 'Resources', icon: Activity },
    { id: 'terminal', label: 'Terminal', icon: SquareTerminal }
  ];

  onMount(() => {
    const t = setTimeout(() => (waited = true), 2500);
    return () => clearTimeout(t);
  });

  const live = $derived(process ? isLive(process) : false);
  const pending = $derived(procActions.busy.get(processId) ?? '');
  const line = $derived(process ? commandLine(process) : '');
  const items = $derived(
    process ? processMenu(process, { onCopyId, afterRemove: onRemoved }) : []
  );

  let suggestion = $state<SaveSuggestion | null>(null);
  let brief = $state<Diagnosis | null>(null);
  let saveOpen = $state(false);
  let hideSuggestion = $state(false);
  let dismissing = $state(false);

  const failing = $derived(process ? isFailing(process) : false);
  const assistKey = $derived(process ? `${process.id}|${process.instanceId}|${process.status}|${process.exitCode}|${process.health}|${process.saveable}` : '');

  $effect(() => {
    const key = assistKey;
    const p = process;
    if (!key || !p) return;
    let cancelled = false;
    const timer = setTimeout(async () => {
      if (isFailing(p)) {
        suggestion = null;
        try {
          const d = await AssistService.diagnose(p.id);
          if (!cancelled) brief = d.category === 'none' ? null : d;
        } catch {
          if (!cancelled) brief = null;
        }
        return;
      }
      brief = null;
      if (!p.saveable || !isHealthyRun(p)) {
        suggestion = null;
        return;
      }
      try {
        const s = await AssistService.suggestion(p.id);
        if (!cancelled) suggestion = s;
      } catch {
        if (!cancelled) suggestion = null;
      }
    }, 400);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  });

  async function notNow() {
    if (!process) return;
    dismissing = true;
    try {
      await AssistService.saveRun({ processId: process.id, action: 'dismiss' });
      hideSuggestion = true;
      toasts.ok("Won't suggest saving this run again");
    } catch (err) {
      toastError(err);
    } finally {
      dismissing = false;
    }
  }

  function selectTab(next: DetailTab) {
    if (next === tab) return;
    tab = next;
    if (next === 'terminal') void loadTerminal();
    onTabChange?.(next);
  }

  function tabKey(e: KeyboardEvent) {
    const idx = tabs.findIndex((t) => t.id === tab);
    let next = idx;
    if (e.key === 'ArrowRight') next = (idx + 1) % tabs.length;
    else if (e.key === 'ArrowLeft') next = (idx - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = tabs.length - 1;
    else return;
    e.preventDefault();
    selectTab(tabs[next]!.id);
    (e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('[role="tab"]')[next]?.focus();
  }
</script>

<div class="detail">
  <header class="head">
    <div class="title">
      <div class="row1">
        <h2 class="name" title={process ? displayName(process) : processId}>{process ? displayName(process) : 'Process'}</h2>
        {#if process}<ProcessStatus {process} {pending} />{/if}
      </div>
      <div class="row2">
        <button type="button" class="idchip mono" title="Copy process id" aria-label="Copy process id" onclick={() => onCopyId(processId)}>
          <Copy size={12} />{processId}
        </button>
        {#if process}
          <span class="dotsep"></span>
          <span class="ws truncate">{scopeState.workspaceLabel(process.workspaceId)}</span>
        {/if}
      </div>
    </div>
    <div class="head-actions">
      {#if process}
        <button
          type="button"
          class="btn sm"
          title="Restart"
          disabled={!!pending}
          onclick={() => void procActions.restart([processId])}
        >
          <RotateCw size={14} /> Restart
        </button>
        <button
          type="button"
          class="btn sm stop"
          title="Stop"
          disabled={!!pending || !live}
          onclick={() => void procActions.stop([processId])}
        >
          <Square size={12} /> Stop
        </button>
        <ActionMenu {items} label="More process actions" triggerClass="btn icon sm">
          <Ellipsis size={14} />
        </ActionMenu>
      {/if}
      <button type="button" class="btn ghost icon sm" aria-label={fullPage ? 'Back to processes' : 'Close details'} title={fullPage ? 'Back (Esc)' : 'Close (Esc)'} onclick={onClose}>
        <X size={16} />
      </button>
    </div>
  </header>

  {#if process && line}
    <div class="cmdrow mono truncate" title={line}>{line}{#if process.mode}<span class="modechip">{process.mode}</span>{/if}</div>
  {/if}

  {#if process && failing && brief}
    {@const a = actorInfo(brief.actor)}
    <div class="assist bad" role="alert">
      <CircleAlert size={15} />
      <div class="assist-text">
        <strong>{brief.summary}</strong>
        <span class="muted">{a.label}</span>
      </div>
      {#if tab !== 'diagnose'}
        <button type="button" class="btn sm" onclick={() => selectTab('diagnose')}>Diagnose<ArrowRight size={13} /></button>
      {/if}
    </div>
  {:else if process && suggestion && !hideSuggestion}
    <div class="assist good" role="status">
      <BookmarkPlus size={15} />
      <div class="assist-text">
        <strong>{suggestion.kind === 'task' ? 'This command finished successfully.' : 'This run is working.'}</strong>
        <span class="muted">Save it as <span class="mono">{suggestion.app} · {suggestion.mode}</span> so it starts in one click next time, with the same command and env.</span>
      </div>
      <button type="button" class="btn sm ghost" disabled={dismissing} onclick={notNow}>Don't save</button>
      <button type="button" class="btn sm primary" onclick={() => (saveOpen = true)}>Save as mode…</button>
    </div>
  {/if}

  <div class="tablist" role="tablist" aria-label="Process details" tabindex="-1" onkeydown={tabKey}>
    {#each tabs as t (t.id)}
      {@const Icon = t.icon}
      <button
        type="button"
        role="tab"
        id="tab-{t.id}"
        class="tab"
        class:active={tab === t.id}
        aria-selected={tab === t.id}
        aria-controls="panel"
        tabindex={tab === t.id ? 0 : -1}
        onclick={() => selectTab(t.id)}
        onpointerenter={() => { if (t.id === 'terminal') void loadTerminal(); }}
        onfocus={() => { if (t.id === 'terminal') void loadTerminal(); }}
      >
        <Icon size={14} />{t.label}
      </button>
    {/each}
  </div>

  <div class="panel" id="panel" role="tabpanel" aria-labelledby="tab-{tab}">
    {#if !process}
      {#if waited}
        <div class="missing">
          <div class="icon-wrap"><SearchX size={20} /></div>
          <h3>Process not found</h3>
          <p>It may have been removed, or it belongs to a workspace that is not loaded.</p>
          <button type="button" class="btn" onclick={onClose}>Close</button>
        </div>
      {:else}
        <div class="missing"><span class="muted">Loading process…</span></div>
      {/if}
    {:else if tab === 'logs'}
      <ProcessLogs processId={processId} {live} workspaceId={process?.workspaceId ?? ''} />
    {:else if tab === 'info'}
      <ProcessInfo {process} />
    {:else if tab === 'env'}
      <ProcessEnv {processId} />
    {:else if tab === 'diagnose'}
      <DiagnosePanel {processId} instanceKey={assistKey} />
    {:else if tab === 'probe'}
      <ProbePanel {process} />
    {:else if tab === 'resources'}
      <ProcessResources {processId} {live} />
    {:else}
      {#if TerminalTab}
        <TerminalTab {processId} interactive={live} />
      {:else}
        <div class="loading-terminal">
          {#if terminalError}
            <span class="err-text">Could not load the terminal: {terminalError}</span>
            <button type="button" class="btn sm" onclick={() => void loadTerminal()}>Retry</button>
          {:else}
            <span class="muted">Loading terminal…</span>
          {/if}
        </div>
      {/if}
    {/if}
  </div>
</div>

{#if saveOpen && process}
  <SaveRunDialog
    {process}
    {suggestion}
    onClose={() => (saveOpen = false)}
    onSaved={(r) => {
      if (!r.saved) return;
      hideSuggestion = true;
      processes.patch(processId, { app: r.app, mode: r.mode, saveable: false });
    }}
  />
{/if}

<style>
  .modechip {
    margin-left: 8px;
    padding: 0 7px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--accent) 16%, var(--bg-3));
    color: var(--text-0);
    font-size: 11px;
  }
  .assist {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin: 0 var(--space-5) var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius);
    border: 1px solid var(--border);
    font-size: var(--fs-sm);
  }
  .assist.bad {
    border-color: color-mix(in srgb, var(--err) 40%, var(--border));
    background: color-mix(in srgb, var(--err) 7%, var(--bg-1));
    color: var(--err);
  }
  .assist.good {
    border-color: color-mix(in srgb, var(--ok) 40%, var(--border));
    background: color-mix(in srgb, var(--ok) 7%, var(--bg-1));
    color: var(--ok);
  }
  .assist-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .assist-text strong {
    color: var(--text-0);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .assist-text .muted {
    font-size: var(--fs-xs);
  }
  .loading-terminal {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-6);
    font-size: var(--fs-sm);
  }
  .loading-terminal .err-text {
    color: var(--err);
  }
  .detail {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-1);
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-4) var(--space-3) var(--space-5);
  }
  .title {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .row1 {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }
  .name {
    font-size: var(--fs-lg);
    font-weight: 600;
    letter-spacing: -0.01em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .row2 {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .idchip {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 1px 7px;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-1);
    font-size: var(--fs-micro);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .idchip:hover {
    color: var(--text-0);
    border-color: var(--border-strong);
  }
  .dotsep {
    width: 3px;
    height: 3px;
    border-radius: 50%;
    background: var(--text-2);
    flex: none;
  }
  .head-actions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
  }
  .stop:hover:not(:disabled) {
    color: var(--err);
  }
  .cmdrow {
    margin: 0 var(--space-5) var(--space-3);
    padding: 5px 10px;
    border-radius: var(--radius-sm);
    background: var(--bg-0);
    border: 1px solid var(--border);
    color: var(--text-1);
    font-size: var(--fs-xs);
  }
  .tablist {
    display: flex;
    gap: 2px;
    padding: 0 var(--space-4);
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tab {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 9px 10px;
    margin-bottom: -1px;
    border: none;
    border-bottom: 2px solid transparent;
    border-radius: 0;
    background: transparent;
    color: var(--text-1);
    font-size: var(--fs-sm);
    font-weight: 500;
    white-space: nowrap;
  }
  .tab:hover {
    color: var(--text-0);
    background: transparent;
  }
  .tab.active {
    color: var(--text-0);
    border-bottom-color: var(--accent);
  }
  .tab :global(svg) {
    color: var(--text-2);
  }
  .tab.active :global(svg) {
    color: var(--accent);
  }
  .panel {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .missing {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-8);
    text-align: center;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .missing .icon-wrap {
    width: 44px;
    height: 44px;
    border-radius: 12px;
    display: grid;
    place-items: center;
    background: var(--bg-3);
    color: var(--text-1);
  }
  .missing h3 {
    color: var(--text-0);
    font-size: var(--fs-lg);
  }
  .missing p {
    margin: 0;
    max-width: 300px;
  }
</style>
