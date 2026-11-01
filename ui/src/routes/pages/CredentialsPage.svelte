<script lang="ts">
  import { getContext, onMount, untrack } from 'svelte';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import Plus from '@lucide/svelte/icons/plus';
  import Terminal from '@lucide/svelte/icons/terminal';
  import Network from '@lucide/svelte/icons/network';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Pencil from '@lucide/svelte/icons/pencil';
  import Eye from '@lucide/svelte/icons/eye';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import Copy from '@lucide/svelte/icons/copy';
  import Star from '@lucide/svelte/icons/star';
  import Cookie from '@lucide/svelte/icons/cookie';
  import Lock from '@lucide/svelte/icons/lock';
  import FlaskConical from '@lucide/svelte/icons/flask-conical';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import ShieldCheck from '@lucide/svelte/icons/shield-check';
  import Square from '@lucide/svelte/icons/square';
  import { CredentialService, ConfigService } from '../../lib/api';
  import { profileToDraft } from '../../lib/api/credentials';
  import type { CredentialDraft, CredentialField, CredentialProfile, CredentialProxy, KeystoreStatus } from '../../lib/api/types';
  import { appliedAs, emptyDraft, expiryText, isProduction, maskedValue, newField, statusLabel, statusTone } from '../../lib/credentialsView';
  import { toConfigApps } from '../../lib/config/apps';
  import { ROUTE_CONTEXT, type RouteView } from '../../lib/routes';
  import { clock } from '../../lib/state/clock.svelte';
  import { dialogs } from '../../lib/state/dialogs.svelte';
  import { processes } from '../../lib/state/processes.svelte';
  import { scopeState } from '../../lib/state/scope.svelte';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import { getPlatform } from '../../lib/platform';
  import CredentialEditor from '../../components/credentials/CredentialEditor.svelte';
  import CredentialTest from '../../components/credentials/CredentialTest.svelte';
  import ImportCurlDialog from '../../components/credentials/ImportCurlDialog.svelte';
  import ProxyDialog from '../../components/credentials/ProxyDialog.svelte';
  import RelativeTime from '../../lib/ui/RelativeTime.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';

  const route = getContext<RouteView | undefined>(ROUTE_CONTEXT);

  let profiles = $state<CredentialProfile[]>([]);
  let proxies = $state<CredentialProxy[]>([]);
  let keystore = $state<KeystoreStatus | null>(null);
  let hint = $state('');
  let loaded = $state(false);
  let error = $state('');
  let apps = $state<string[]>([]);
  let editing = $state<CredentialDraft | null>(null);
  let importing = $state(false);
  let proxying = $state<string | null>(null);
  let testOpen = $state<Record<string, boolean>>({});
  let revealed = $state<Record<string, string>>({});
  let busy = $state<Record<string, string>>({});
  let keyBusy = $state(false);
  let seq = 0;
  const timers = new Map<string, ReturnType<typeof setTimeout>>();

  const ws = $derived(scopeState.workspaceId);
  const live = $derived(
    processes
      .byWorkspace(ws)
      .filter((p) => p.status === 'running' || p.status === 'ready' || p.status === 'starting')
  );
  const nowMs = $derived(clock.now);
  const unreadable = $derived(profiles.some((p) => p.status === 'unreadable'));

  async function load(workspaceId: string, quiet = false) {
    if (!workspaceId) {
      profiles = [];
      proxies = [];
      loaded = true;
      return;
    }
    const my = ++seq;
    try {
      const [list, px, ks] = await Promise.all([CredentialService.list(workspaceId), CredentialService.proxies(workspaceId), CredentialService.keystore()]);
      if (my !== seq) return;
      profiles = list.profiles;
      hint = list.hint;
      proxies = px;
      keystore = ks;
      error = '';
    } catch (err) {
      if (my !== seq) return;
      if (!quiet) error = err instanceof Error ? err.message : String(err);
    } finally {
      if (my === seq) loaded = true;
    }
  }

  async function loadApps(workspaceId: string) {
    if (!workspaceId) {
      apps = [];
      return;
    }
    try {
      const cfg = await ConfigService.get(workspaceId);
      apps = toConfigApps(cfg.apps).map((a) => a.name);
    } catch {
      apps = [];
    }
  }

  $effect(() => {
    const id = ws;
    if (route && !route.active) return;
    untrack(() => {
      loaded = false;
      revealed = {};
      void load(id);
      void loadApps(id);
    });
  });

  onMount(() => {
    const t = setInterval(() => {
      if (route && !route.active) return;
      if (document.hidden) return;
      void load(scopeState.workspaceId, true);
    }, 6000);
    return () => {
      clearInterval(t);
      for (const h of timers.values()) clearTimeout(h);
    };
  });

  function startNew() {
    editing = emptyDraft();
  }

  function startEdit(p: CredentialProfile) {
    editing = profileToDraft(p);
  }

  function onImported(fields: CredentialField[], host: string) {
    importing = false;
    const d = emptyDraft();
    d.name = (host || 'imported').replace(/[^A-Za-z0-9_.-]+/g, '-').replace(/^-+/, '') || 'imported';
    d.fields = fields.length > 0 ? fields : [newField('bearer')];
    d.default = profiles.length === 0;
    editing = d;
  }

  async function saved() {
    editing = null;
    await load(ws);
  }

  function mark(id: string, label: string | null) {
    const next = { ...busy };
    if (label === null) delete next[id];
    else next[id] = label;
    busy = next;
  }

  async function remove(p: CredentialProfile) {
    const ok = await dialogs.confirm(`Delete ${p.name}? Saved checks that name it will run without credentials until you pick another profile.`, { title: 'Delete credential profile', confirmLabel: 'Delete', danger: true });
    if (!ok) return;
    mark(p.id, 'delete');
    try {
      await CredentialService.remove(ws, p.id);
      toasts.ok(`Deleted ${p.name}`);
      await load(ws);
    } catch (err) {
      toastError(err);
    } finally {
      mark(p.id, null);
    }
  }

  async function makeDefault(p: CredentialProfile) {
    mark(p.id, 'default');
    try {
      await CredentialService.upsert(ws, { ...profileToDraft(p), default: true });
      toasts.ok(`${p.name} is now the default for this project`);
      await load(ws);
    } catch (err) {
      toastError(err);
    } finally {
      mark(p.id, null);
    }
  }

  async function refresh(p: CredentialProfile) {
    const target = live.find((x) => x.ports.length > 0) ?? live[0];
    if (!target) {
      toasts.err('Start the app first: the login request is sent to it');
      return;
    }
    mark(p.id, 'refresh');
    try {
      const got = await CredentialService.refresh({ workspaceId: ws, id: p.id, processId: target.id });
      toasts.ok(got.status === 'ok' ? `${p.name} logged in` : `${p.name} refreshed`);
      await load(ws);
    } catch (err) {
      toastError(err);
      await load(ws, true);
    } finally {
      mark(p.id, null);
    }
  }

  async function clearJar(p: CredentialProfile) {
    mark(p.id, 'jar');
    try {
      await CredentialService.clearJar(ws, p.id);
      toasts.ok('Cookie jar cleared');
      await load(ws);
    } catch (err) {
      toastError(err);
    } finally {
      mark(p.id, null);
    }
  }

  function revealKey(p: CredentialProfile, f: CredentialField): string {
    return `${p.id}:${f.name}`;
  }

  async function toggleReveal(p: CredentialProfile, f: CredentialField) {
    const key = revealKey(p, f);
    if (key in revealed) {
      hide(key);
      return;
    }
    try {
      const v = await CredentialService.reveal(ws, p.id, f.name);
      revealed = { ...revealed, [key]: v };
      timers.set(key, setTimeout(() => hide(key), 20000));
    } catch (err) {
      toastError(err);
    }
  }

  function hide(key: string) {
    const next = { ...revealed };
    delete next[key];
    revealed = next;
    const t = timers.get(key);
    if (t) clearTimeout(t);
    timers.delete(key);
  }

  async function copyField(p: CredentialProfile, f: CredentialField) {
    try {
      const v = f.secret ? await CredentialService.reveal(ws, p.id, f.name) : f.value;
      await getPlatform().copyText(v);
      toasts.ok(`Copied ${f.name}`);
    } catch (err) {
      toastError(err);
    }
  }

  async function copyUrl(url: string) {
    try {
      await getPlatform().copyText(url);
      toasts.ok('Proxy address copied');
    } catch (err) {
      toastError(err);
    }
  }

  async function stopProxy(px: CredentialProxy) {
    try {
      await CredentialService.stopProxy(ws, px.id);
      toasts.ok('Proxy stopped');
      await load(ws, true);
    } catch (err) {
      toastError(err);
      await load(ws, true);
    }
  }

  function proxyStarted(px: CredentialProxy) {
    proxying = null;
    toasts.ok(`Proxy listening at ${px.url}`);
    void load(ws, true);
  }

  async function setBackend(backend: string) {
    keyBusy = true;
    try {
      keystore = await CredentialService.setKeystore(backend);
      toasts.ok(`Encryption key now stored in the ${keystore.active === 'keychain' ? 'OS keychain' : 'key file'}`);
    } catch (err) {
      toastError(err);
    } finally {
      keyBusy = false;
    }
  }

  async function resetKey() {
    const ok = await dialogs.confirm('Create a new encryption key and clear every stored secret in every project? Names, kinds and non-secret values stay; you will have to enter the secrets again.', { title: 'Reset the credentials key', confirmLabel: 'Reset key', danger: true });
    if (!ok) return;
    keyBusy = true;
    try {
      const n = await CredentialService.resetKey();
      toasts.ok(`Key reset; ${n} profile${n === 1 ? '' : 's'} need their secrets again`);
      await load(ws);
    } catch (err) {
      toastError(err);
    } finally {
      keyBusy = false;
    }
  }

  function valueText(p: CredentialProfile, f: CredentialField): string {
    const k = revealKey(p, f);
    if (k in revealed) return revealed[k] ?? '';
    return maskedValue(f);
  }

  const BACKENDS = [
    { value: 'auto', label: 'Automatic' },
    { value: 'keychain', label: 'OS keychain' },
    { value: 'file', label: 'Key file' }
  ];

  function activeLabel(k: KeystoreStatus): string {
    if (k.active === 'keychain') return 'OS keychain';
    if (k.active === 'file') return 'key file';
    return 'not created yet';
  }
</script>

<div class="page">
  <header class="page-header">
    <div class="titles">
      <h1>Credentials</h1>
      <p class="subtitle">Test credentials the agent can use without ever seeing them.</p>
    </div>
    <div class="actions">
      <button class="btn" onclick={() => (importing = true)} disabled={!ws}><Terminal size={14} />Import cURL</button>
      <button class="btn" onclick={() => (proxying = '')} disabled={!ws || profiles.length === 0}><Network size={14} />Start proxy</button>
      <button class="btn primary" onclick={startNew} disabled={!ws}><Plus size={14} />New profile</button>
    </div>
  </header>

  {#if !ws}
    <div class="card">
      <div class="empty-state">
        <span class="icon-wrap"><KeyRound size={20} /></span>
        <h3>Choose a project</h3>
        <p>Credential profiles belong to a project. Pick a workspace in the top bar to manage its credentials.</p>
      </div>
    </div>
  {:else}
    <div class="banner info" role="note">
      <ShieldCheck size={16} />
      <span>The agent sees profile names, field kinds and token status only. Secret values are encrypted at rest and replaced with <span class="mono">[credential:name]</span> in every probe result and log line. <code>probe</code> attaches the default profile automatically.</span>
    </div>

    {#if error}
      <div class="banner err" role="alert"><TriangleAlert size={16} /><span>Could not load credentials: {error}</span><button class="btn sm" onclick={() => void load(ws)}>Retry</button></div>
    {/if}

    {#if unreadable}
      <div class="banner err" role="alert">
        <TriangleAlert size={16} />
        <span>Some profiles cannot be decrypted with the current key (the key file or keychain entry was replaced or lost). Reset the key below and enter their secrets again, or restore the original key.</span>
      </div>
    {/if}

    {#if !loaded}
      <div class="card" aria-busy="true"><div class="card-body"><Skeleton width="40%" height="16px" /><Skeleton width="80%" height="12px" /><Skeleton width="65%" height="12px" /></div></div>
    {:else if profiles.length === 0 && !error}
      <div class="card">
        <div class="empty-state">
          <span class="icon-wrap"><KeyRound size={20} /></span>
          <h3>No credential profiles yet</h3>
          <p>{hint || 'Add a JWT, session cookie or API key, plus a tenant id if the API needs one. The agent can then call your authenticated endpoints and see the logs each call produced.'}</p>
          <div class="empty-actions">
            <button class="btn primary" onclick={startNew}><Plus size={14} />New profile</button>
            <button class="btn" onclick={() => (importing = true)}><Terminal size={14} />Import from cURL</button>
          </div>
        </div>
      </div>
    {:else}
      <div class="profiles">
        {#each profiles as p (p.id)}
          {@const tone = statusTone(p.status)}
          {@const working = busy[p.id]}
          <article class="profile card" class:def={p.default} aria-label="Credential profile {p.name}">
            <div class="head">
              <div class="title">
                <h2 class="mono">{p.name}</h2>
                {#if p.default}<span class="badge accent">default</span>{/if}
                {#if p.environment}<span class="badge" class:err={isProduction(p.environment)}>{p.environment}</span>{/if}
                <span class="badge {tone === 'neutral' ? '' : tone}" title={p.lastError || undefined}>{statusLabel(p.status, !!p.recipe)}</span>
                {#if p.expiresAt}<span class="exp" class:late={p.status === 'expired'}>{expiryText(p.expiresAt, nowMs)}</span>{/if}
                {#if p.apps.length > 0}<span class="badge" title="Only used for these apps">apps: {p.apps.join(', ')}</span>{/if}
              </div>
              <div class="tools">
                <button class="btn ghost sm" aria-pressed={!!testOpen[p.id]} onclick={() => (testOpen = { ...testOpen, [p.id]: !testOpen[p.id] })}><FlaskConical size={13} />Test</button>
                {#if p.recipe}
                  <button class="btn ghost sm" disabled={!!working} onclick={() => void refresh(p)} title="Run the login recipe now">{#if working === 'refresh'}<Spinner size={12} />{:else}<RefreshCw size={13} />{/if}Log in</button>
                {/if}
                {#if !p.default}
                  <button class="btn ghost sm" disabled={!!working || p.status === 'unreadable'} onclick={() => void makeDefault(p)} title="Attach this profile automatically"><Star size={13} />Make default</button>
                {/if}
                <button class="btn ghost icon sm" aria-label="Edit {p.name}" title="Edit" disabled={p.status === 'unreadable'} onclick={() => startEdit(p)}><Pencil size={14} /></button>
                <button class="btn ghost icon sm danger" aria-label="Delete {p.name}" title="Delete" disabled={!!working} onclick={() => void remove(p)}><Trash2 size={14} /></button>
              </div>
            </div>
            {#if p.description}<p class="desc">{p.description}</p>{/if}
            {#if p.lastError && p.status !== 'unreadable'}
              <div class="banner warn compact"><TriangleAlert size={14} /><span>{p.lastError}</span></div>
            {/if}
            {#if p.status === 'unreadable'}
              <div class="banner err compact"><TriangleAlert size={14} /><span>{p.lastError}</span></div>
            {:else}
              <table class="fields">
                <thead><tr><th>Field</th><th>Applied as</th><th>Value</th><th class="act"></th></tr></thead>
                <tbody>
                  {#each p.fields as f (f.name)}
                    {@const key = revealKey(p, f)}
                    <tr>
                      <td class="mono">{f.name}{#if f.secret}<Lock size={11} class="lock" />{/if}</td>
                      <td class="mono dim">{appliedAs(f)}</td>
                      <td class="mono val" class:shown={key in revealed} class:unset={f.secret && !f.hasValue}>{valueText(p, f)}{#if f.expiresAt}<span class="fexp">{expiryText(f.expiresAt, nowMs)}</span>{/if}</td>
                      <td class="act">
                        {#if f.secret && f.hasValue}
                          <button class="btn ghost icon sm" aria-label={key in revealed ? `Hide ${f.name}` : `Reveal ${f.name}`} title={key in revealed ? 'Hide' : 'Reveal for 20 seconds'} onclick={() => void toggleReveal(p, f)}>
                            {#if key in revealed}<EyeOff size={13} />{:else}<Eye size={13} />{/if}
                          </button>
                        {/if}
                        {#if f.hasValue}
                          <button class="btn ghost icon sm" aria-label="Copy {f.name}" title="Copy" onclick={() => void copyField(p, f)}><Copy size={13} /></button>
                        {/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
              {#if p.recipe}
                <p class="meta">Login recipe: <span class="mono">{p.recipe.method} {p.recipe.url}</span>, fills {p.recipe.extract.map((e) => e.field).join(', ') || 'cookies'}.
                  {#if p.lastRefreshAt}Last login <RelativeTime ts={p.lastRefreshAt * 1000} />.{/if}</p>
              {/if}
              {#if p.captureCookies}
                <p class="meta jar">
                  <Cookie size={12} />
                  {#if p.cookies.length > 0}
                    Cookie jar: <span class="mono">{p.cookies.map((c) => c.name).join(', ')}</span>
                    <button class="btn ghost sm" disabled={working === 'jar'} onclick={() => void clearJar(p)}>Clear</button>
                  {:else}
                    Cookie jar is empty. Cookies the app sets are kept here automatically.
                  {/if}
                </p>
              {/if}
              <p class="meta">{#if p.lastUsedAt}Used <RelativeTime ts={p.lastUsedAt * 1000} />.{:else}Not used yet.{/if}</p>
              {#if testOpen[p.id]}
                <div class="testwrap"><CredentialTest profile={p} processes={live} onResult={() => void load(ws, true)} /></div>
              {/if}
            {/if}
          </article>
        {/each}
      </div>
    {/if}

    <section class="card" aria-label="Credential proxies">
      <div class="card-header"><h3>Proxies</h3><span class="muted">Local addresses that add a profile to every request. They stop on their own.</span></div>
      <div class="card-body">
        {#if proxies.length === 0}
          <p class="muted small">No proxies running. Start one to drive curl, Playwright or a browser as an authenticated user.</p>
        {:else}
          <ul class="proxies">
            {#each proxies as px (px.id)}
              <li>
                <button class="url mono" title="Copy address" onclick={() => void copyUrl(px.url)}>{px.url}<Copy size={12} /></button>
                <span class="mono dim">→ {px.upstream}</span>
                <span class="badge accent">{px.profile}</span>
                {#if px.label}<span class="badge">{px.label}</span>{/if}
                <span class="muted">{px.requests} request{px.requests === 1 ? '' : 's'}{px.refreshes ? `, ${px.refreshes} login${px.refreshes === 1 ? '' : 's'}` : ''}{px.lastStatus ? `, last ${px.lastStatus}` : ''}</span>
                <span class="muted">{expiryText(px.expiresAt, nowMs).replace('expires', 'stops')}</span>
                <button class="btn ghost sm" onclick={() => void stopProxy(px)}><Square size={12} />Stop</button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    </section>

    {#if keystore}
      <section class="card" aria-label="Encryption key storage">
        <div class="card-header"><h3>Encryption key</h3><span class="muted">Secrets are sealed with AES-256-GCM. Stored in the {activeLabel(keystore)}.</span></div>
        <div class="card-body key">
          <div class="seg" role="group" aria-label="Key storage">
            {#each BACKENDS as b (b.value)}
              <button type="button" class:active={keystore.backend === b.value} aria-pressed={keystore.backend === b.value} disabled={keyBusy || (b.value === 'keychain' && !keystore.keychainAvailable)} onclick={() => void setBackend(b.value)}>{b.label}</button>
            {/each}
          </div>
          {#if keyBusy}<Spinner size={14} />{/if}
          <span class="muted small">
            {#if keystore.keychainAvailable}
              OS keychain available ({keystore.keychainTool}). Automatic uses it and falls back to the file.
            {:else}
              OS keychain unavailable{keystore.keychainError ? `: ${keystore.keychainError}` : ''}. Using <span class="mono">{keystore.keyPath}</span> (mode 0600).
            {/if}
          </span>
          <span class="grow"></span>
          <button class="btn ghost sm danger" disabled={keyBusy} onclick={() => void resetKey()}>Reset key…</button>
        </div>
      </section>
    {/if}
  {/if}
</div>

{#if editing}
  <CredentialEditor workspaceId={ws} initial={editing} knownApps={apps} onClose={() => (editing = null)} onSaved={() => void saved()} />
{/if}
{#if importing}
  <ImportCurlDialog onClose={() => (importing = false)} onImport={onImported} />
{/if}
{#if proxying !== null}
  <ProxyDialog workspaceId={ws} {profiles} processes={live} initialProfile={proxying} onClose={() => (proxying = null)} onStarted={proxyStarted} />
{/if}

<style>
  .page {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .banner {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
  }
  .banner.compact {
    padding: 6px 10px;
    font-size: var(--fs-xs);
  }
  .empty-actions {
    display: flex;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }
  .profiles {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .profile {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4) var(--space-5);
  }
  .profile.def {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    flex-wrap: wrap;
  }
  .title {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
    min-width: 0;
  }
  .title h2 {
    margin: 0;
    font-size: var(--fs-md);
    font-weight: 600;
    color: var(--text-0);
  }
  .exp {
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .exp.late {
    color: var(--err);
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 2px;
    flex-wrap: wrap;
  }
  .tools :global(.danger:hover) {
    color: var(--err);
  }
  .desc {
    margin: 0;
    font-size: var(--fs-sm);
    color: var(--text-1);
  }
  .fields {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-xs);
  }
  .fields th {
    text-align: left;
    font-weight: 500;
    color: var(--text-2);
    padding: 4px 8px;
    border-bottom: 1px solid var(--border);
  }
  .fields td {
    padding: 5px 8px;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
    vertical-align: middle;
  }
  .fields :global(.lock) {
    margin-left: 5px;
    color: var(--text-2);
    vertical-align: -1px;
  }
  .dim {
    color: var(--text-2);
  }
  .val {
    word-break: break-all;
    color: var(--text-1);
  }
  .val.shown {
    color: var(--warn);
  }
  .val.unset {
    color: var(--err);
  }
  .fexp {
    margin-left: 10px;
    color: var(--text-2);
    font-size: var(--fs-micro);
  }
  .act {
    width: 1%;
    white-space: nowrap;
    text-align: right;
  }
  .meta {
    margin: 0;
    font-size: var(--fs-xs);
    color: var(--text-2);
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .testwrap {
    padding-top: var(--space-3);
    border-top: 1px dashed var(--border);
  }
  .proxies {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .proxies li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
    font-size: var(--fs-xs);
  }
  .url {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 2px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-2);
    color: var(--text-0);
    cursor: pointer;
  }
  .url:hover {
    border-color: var(--border-strong);
  }
  .key {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    flex-wrap: wrap;
  }
  .grow {
    flex: 1;
  }
  .muted {
    color: var(--text-2);
  }
  .small {
    font-size: var(--fs-xs);
    margin: 0;
  }
  .card-header h3 {
    margin: 0;
    font-size: var(--fs-sm);
  }
  .card-header .muted {
    margin-left: var(--space-3);
    font-size: var(--fs-xs);
  }
</style>
