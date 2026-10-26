<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import Login from './Login.svelte';
  import { router } from '../lib/router.svelte';
  import { scopeState } from '../lib/state/scope.svelte';
  import { connection } from '../lib/state/connection.svelte';
  import { events } from '../lib/state/events.svelte';
  import { sessions } from '../lib/state/sessions.svelte';
  import { processes } from '../lib/state/processes.svelte';
  import { realtime } from '../lib/realtime/index.svelte';
  import { handleAlertEvent } from '../lib/state/alerts.svelte';
  import { installGlobalKeys } from '../lib/keys';
  import { SystemService, hasAuthToken, setUnauthorizedHandler } from '../lib/api';
  import ToastRegion from '../lib/ui/ToastRegion.svelte';
  import CommandPalette from '../lib/ui/CommandPalette.svelte';
  import ConfirmHost from '../lib/ui/ConfirmHost.svelte';
  import Shell from '../app/Shell.svelte';
  import Outlet from '../app/Outlet.svelte';
  import Splash from '../app/Splash.svelte';
  import DaemonUnreachable from '../app/DaemonUnreachable.svelte';
  import ShortcutsDialog from '../app/ShortcutsDialog.svelte';

  let authed = $state(hasAuthToken());
  let ready = $state(false);
  let bootFailed = $state(false);
  let showSplash = $state(false);
  let shortcutsOpen = $state(false);
  let started = false;
  let stopMonitor: (() => void) | null = null;

  $effect(() => connection.setLive(realtime.state));

  $effect(() => {
    void router.path;
    untrack(() => scopeState.syncFromUrl());
  });

  onMount(() => {
    setUnauthorizedHandler(() => {
      authed = false;
      stopAll();
    });
    const offKeys = installGlobalKeys();
    const onShortcuts = () => (shortcutsOpen = true);
    window.addEventListener('ar:shortcuts', onShortcuts);
    const splashTimer = setTimeout(() => (showSplash = true), 300);
    void boot().finally(() => clearTimeout(splashTimer));
    return () => {
      offKeys();
      window.removeEventListener('ar:shortcuts', onShortcuts);
    };
  });

  function stopAll() {
    realtime.stop();
    processes.disconnect();
    events.disconnect();
    sessions.disconnect();
    stopMonitor?.();
    stopMonitor = null;
    started = false;
  }

  async function boot() {
    if (!authed) {
      ready = true;
      return;
    }
    if (router.path === '/') router.navigate('/processes', { replace: true });
    const ok = await verify();
    bootFailed = !ok;
    startOnce();
    ready = true;
  }

  async function verify(): Promise<boolean> {
    try {
      const v = await SystemService.version();
      connection.version = v.daemonVersion;
      connection.apiVersion = v.apiVersion;
      return true;
    } catch {
      return false;
    }
  }

  function startOnce() {
    if (started) return;
    started = true;
    void scopeState.refresh();
    realtime.start();
    processes.connect('', true);
    events.connect('');
    sessions.connect();
    const alertsOnLeader = (e: Parameters<typeof handleAlertEvent>[0]) => {
      if (realtime.leader) handleAlertEvent(e);
    };
    events.onEvent('logs.alert', alertsOnLeader);
    events.onEvent('process.crashed', alertsOnLeader);
    events.onEvent('process.failed', alertsOnLeader);
    stopMonitor = connection.start();
  }
</script>

{#if !authed}
  <Login onAuthed={() => { authed = true; started = false; void boot(); }} />
{:else if !ready}
  {#if showSplash}<Splash />{/if}
{:else if bootFailed && processes.map.size === 0 && connection.state === 'offline'}
  <DaemonUnreachable onRetry={() => void boot()} />
  <ToastRegion />
{:else}
  <Shell>
    <Outlet />
  </Shell>
  <ToastRegion />
  <CommandPalette />
  <ShortcutsDialog bind:open={shortcutsOpen} />
  <ConfirmHost />
{/if}
