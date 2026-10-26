<script lang="ts">
  import Send from '@lucide/svelte/icons/send';
  import ListPlus from '@lucide/svelte/icons/list-plus';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CircleX from '@lucide/svelte/icons/circle-x';
  import History from '@lucide/svelte/icons/history';
  import { AssistService } from '../../lib/api';
  import type { Process, ProbeResult } from '../../lib/api/types';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import { parseHeaders, prettyBody } from './assistView';

  let { process }: { process: Process } = $props();

  const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];
  const HISTORY_KEY = 'jk.probe.history';

  let method = $state('GET');
  let url = $state('/');
  let port = $state('');
  let headers = $state('');
  let body = $state('');
  let expectStatus = $state('');
  let expectContains = $state('');
  let showAdvanced = $state(false);
  let sending = $state(false);
  let saving = $state(false);
  let naming = $state(false);
  let checkName = $state('');
  let result = $state<ProbeResult | null>(null);
  let history = $state<{ method: string; url: string; body: string }[]>(readHistory());

  function readHistory() {
    try {
      const raw = localStorage.getItem(HISTORY_KEY);
      const parsed = raw ? (JSON.parse(raw) as { method: string; url: string; body: string }[]) : [];
      return Array.isArray(parsed) ? parsed.slice(0, 8) : [];
    } catch {
      return [];
    }
  }

  function remember() {
    const entry = { method, url, body };
    const next = [entry, ...history.filter((h) => !(h.method === entry.method && h.url === entry.url && h.body === entry.body))].slice(0, 8);
    history = next;
    try {
      localStorage.setItem(HISTORY_KEY, JSON.stringify(next));
    } catch {
      return;
    }
  }

  const live = $derived(process.status === 'running' || process.status === 'ready' || process.status === 'starting');
  const unsafe = $derived(!['GET', 'HEAD', 'OPTIONS'].includes(method) || /^https?:\/\/(?!localhost|127\.|\[::1\])/i.test(url.trim()));
  const pretty = $derived(result ? prettyBody(result.body) : { text: '', json: false });
  const canSaveCheck = $derived(!!process.app && !!result?.check);

  async function send(e?: Event) {
    e?.preventDefault();
    if (!url.trim() && method !== 'GET') return;
    sending = true;
    try {
      result = await AssistService.probe({
        processId: process.id,
        method,
        url: url.trim() || '/',
        port: port ? Number(port) : undefined,
        headers: parseHeaders(headers),
        body: body || undefined,
        expectStatus: expectStatus ? Number(expectStatus) : undefined,
        expectContains: expectContains || undefined,
        confirmed: true
      });
      remember();
    } catch (err) {
      toastError(err);
    } finally {
      sending = false;
    }
  }

  function focusOnMount(node: HTMLInputElement) {
    queueMicrotask(() => {
      node.focus();
      node.select();
    });
  }

  function startNaming() {
    if (!result?.check) return;
    checkName = result.check.name;
    naming = true;
  }

  async function saveCheck(e?: Event) {
    e?.preventDefault();
    if (!result?.check) return;
    const name = checkName.trim();
    if (!name) return;
    saving = true;
    try {
      const res = await AssistService.saveRun({
        processId: process.id,
        app: process.app,
        mode: process.mode || undefined,
        checks: [{ ...result.check, name }]
      });
      toasts.ok(res.unchanged ? `Check ${name} was already saved` : `Saved check ${name} on ${process.app}${process.mode ? ` · ${process.mode}` : ''}`);
      naming = false;
    } catch (err) {
      toastError(err);
    } finally {
      saving = false;
    }
  }

  function load(h: { method: string; url: string; body: string }) {
    method = h.method;
    url = h.url;
    body = h.body;
  }

  function statusTone(r: ProbeResult): string {
    if (r.error) return 'err';
    if (r.status >= 500) return 'err';
    if (r.status >= 400) return 'warn';
    return 'ok';
  }
</script>

<div class="probe">
  <form class="req" onsubmit={send}>
    <div class="line">
      <select bind:value={method} aria-label="HTTP method" class="method">
        {#each METHODS as m (m)}<option value={m}>{m}</option>{/each}
      </select>
      <input class="url mono" bind:value={url} placeholder="/api/health or http://localhost:3000/x" aria-label="Path or URL" spellcheck="false" />
      <button type="submit" class="btn primary sm" disabled={sending || !live}>
        {#if sending}<Spinner size={12} />{:else}<Send size={13} />{/if}Send
      </button>
    </div>
    {#if !live}
      <p class="note">The process is not running. Start it to send requests.</p>
    {:else if unsafe}
      <p class="note warn"><TriangleAlert size={13} />This request can change data or leaves this machine. It is sent as you, without an agent confirmation step.</p>
    {/if}
    <div class="row2">
      <button type="button" class="linkish" aria-expanded={showAdvanced} onclick={() => (showAdvanced = !showAdvanced)}>{showAdvanced ? 'Hide' : 'Headers, body, expectations'}</button>
      {#if process.ports.length > 1}
        <label class="port">port
          <select bind:value={port} aria-label="Port">
            <option value="">auto</option>
            {#each process.ports as p (p)}<option value={String(p)}>{p}</option>{/each}
          </select>
        </label>
      {/if}
      {#if history.length > 0}
        <span class="grow"></span>
        <History size={13} />
        {#each history.slice(0, 4) as h, i (i)}
          <button type="button" class="hist mono" title={h.body ? `${h.method} ${h.url}\n${h.body}` : `${h.method} ${h.url}`} onclick={() => load(h)}>{h.method} {h.url}</button>
        {/each}
      {/if}
    </div>
    {#if showAdvanced}
      <div class="adv">
        <label>Headers <span class="muted">(Name: value per line; ${'{'}VAR{'}'} expands from the process env)</span>
          <textarea class="mono" rows="3" bind:value={headers} placeholder="Authorization: Bearer ${'{'}API_TOKEN{'}'}"></textarea>
        </label>
        <label>Body
          <textarea class="mono" rows="5" bind:value={body} placeholder={'{"name": "test"}'}></textarea>
        </label>
        <div class="expect">
          <label>Expect status <input class="mono" inputmode="numeric" bind:value={expectStatus} placeholder="2xx/3xx" /></label>
          <label class="grow">Body contains <input class="mono" bind:value={expectContains} placeholder="optional" /></label>
        </div>
      </div>
    {/if}
  </form>

  <div class="res">
    {#if !result}
      <div class="empty muted">
        <p>Send a request to see the response next to the exact log lines the process printed while handling it.</p>
      </div>
    {:else}
      <div class="summary">
        {#if result.ok}<CircleCheck size={16} class="okc" />{:else}<CircleX size={16} class="errc" />{/if}
        {#if result.status}<span class="status {statusTone(result)}">{result.status}</span>{/if}
        <span class="mono target truncate" title={result.url || result.tcp}>{result.method} {result.url || result.tcp}</span>
        <span class="muted">{result.durationMs} ms</span>
        {#if result.bodyBytes}<span class="muted">{result.bodyBytes} B{result.bodyTruncated ? '+' : ''}</span>{/if}
        <span class="grow"></span>
        {#if naming}
          <form class="namer" onsubmit={saveCheck}>
            <input class="mono" bind:value={checkName} aria-label="Check name" use:focusOnMount onkeydown={(e) => { if (e.key === 'Escape') naming = false; }} />
            <button type="submit" class="btn sm primary" disabled={saving || !checkName.trim()}>{#if saving}<Spinner size={12} />{/if}Save</button>
            <button type="button" class="btn sm ghost" onclick={() => (naming = false)}>Cancel</button>
          </form>
        {:else}
          <button type="button" class="btn sm" disabled={!canSaveCheck || saving} title={process.app ? `Save this request as a check on ${process.app}${process.mode ? ` · ${process.mode}` : ''} so it can be re-run` : 'Save this run as an app mode first'} onclick={startNaming}>
            <ListPlus size={13} />Save as check
          </button>
        {/if}
      </div>
      {#if result.failures.length > 0 || result.error}
        <ul class="fails">
          {#if result.error}<li>{result.error}</li>{/if}
          {#each result.failures.filter((f) => !result?.error || !f.includes(result.error)) as f (f)}<li>{f}</li>{/each}
          {#if result.listeningPorts.length > 0}<li class="muted">listening on {result.listeningPorts.join(', ')}</li>{/if}
        </ul>
      {/if}
      <div class="split">
        <section>
          <h5>Response</h5>
          {#if Object.keys(result.headers).length > 0}
            <dl class="hdrs">
              {#each Object.entries(result.headers) as [k, v] (k)}<dt>{k}</dt><dd class="mono">{v}</dd>{/each}
            </dl>
          {/if}
          <pre class="body mono" class:json={pretty.json}>{pretty.text || '(empty body)'}</pre>
        </section>
        <section>
          <h5>Logs during the request <span class="muted">{result.logs.length}{result.logsTruncated ? '+' : ''} line{result.logs.length === 1 ? '' : 's'}{result.errorLogs ? ` · ${result.errorLogs} error` : ''}</span></h5>
          {#if result.logs.length === 0}
            <p class="muted small">The process printed nothing while handling this request. Add a request log line (method, path, status, request_id) to see it here.</p>
          {:else}
            <pre class="logs mono">{#each result.logs as l (l.id)}<span class:stderr={l.stream === 'stderr'}>{l.line}
</span>{/each}</pre>
          {/if}
        </section>
      </div>
    {/if}
  </div>
</div>

<style>
  .probe {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .req {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4) var(--space-5);
    border-bottom: 1px solid var(--border);
  }
  .line {
    display: flex;
    gap: var(--space-3);
    align-items: center;
  }
  .method,
  .port select {
    height: 30px;
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-0);
    font-size: var(--fs-sm);
    padding: 0 6px;
  }
  .url {
    flex: 1;
    min-width: 0;
    height: 30px;
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    color: var(--text-0);
    padding: 0 10px;
    font-size: var(--fs-sm);
  }
  .url:focus,
  textarea:focus,
  .expect input:focus {
    outline: none;
    border-color: var(--accent);
  }
  .note {
    margin: 0;
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-xs);
    color: var(--text-2);
  }
  .note.warn {
    color: var(--warn);
  }
  .row2 {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .linkish {
    padding: 0;
    border: none;
    background: transparent;
    color: var(--accent);
    font-size: var(--fs-xs);
    cursor: pointer;
  }
  .port {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .hist {
    padding: 1px 8px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-1);
    font-size: 11px;
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
  }
  .hist:hover {
    color: var(--text-0);
    border-color: var(--border-strong);
  }
  .adv {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .adv label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: var(--fs-xs);
    color: var(--text-1);
  }
  textarea,
  .expect input {
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    color: var(--text-0);
    padding: 6px 10px;
    font-size: var(--fs-xs);
    resize: vertical;
  }
  .expect {
    display: flex;
    gap: var(--space-3);
  }
  .expect input {
    height: 28px;
  }
  .grow {
    flex: 1;
  }
  .res {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--space-4) var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .empty {
    margin: auto;
    max-width: 360px;
    text-align: center;
    font-size: var(--fs-sm);
  }
  .summary {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    font-size: var(--fs-sm);
  }
  .summary :global(.okc) {
    color: var(--ok);
    flex: none;
  }
  .summary :global(.errc) {
    color: var(--err);
    flex: none;
  }
  .status {
    padding: 1px 8px;
    border-radius: 999px;
    font-weight: 600;
    font-size: var(--fs-xs);
    font-variant-numeric: tabular-nums;
  }
  .status.ok {
    color: var(--ok);
    background: color-mix(in srgb, var(--ok) 14%, transparent);
  }
  .status.warn {
    color: var(--warn);
    background: color-mix(in srgb, var(--warn) 14%, transparent);
  }
  .status.err {
    color: var(--err);
    background: color-mix(in srgb, var(--err) 14%, transparent);
  }
  .target {
    min-width: 0;
    color: var(--text-0);
  }
  .namer {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .namer input {
    height: 26px;
    width: 160px;
    border-radius: var(--radius);
    border: 1px solid var(--accent);
    background: var(--bg-0);
    color: var(--text-0);
    padding: 0 8px;
    font-size: var(--fs-xs);
  }
  .fails {
    margin: 0;
    padding: var(--space-3) var(--space-4) var(--space-3) var(--space-6);
    border-radius: var(--radius);
    border: 1px solid color-mix(in srgb, var(--err) 35%, var(--border));
    background: color-mix(in srgb, var(--err) 7%, transparent);
    color: var(--err);
    font-size: var(--fs-xs);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .split {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--space-4);
    min-height: 0;
  }
  .split section {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
  }
  h5 {
    margin: 0;
    font-size: var(--fs-xs);
    font-weight: 600;
    color: var(--text-1);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  h5 .muted {
    text-transform: none;
    letter-spacing: 0;
    font-weight: 400;
  }
  .hdrs {
    margin: 0;
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 2px var(--space-3);
    font-size: var(--fs-xs);
  }
  .hdrs dt {
    color: var(--text-2);
  }
  .hdrs dd {
    margin: 0;
    color: var(--text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .body,
  .logs {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    font-size: var(--fs-xs);
    color: var(--text-1);
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 360px;
    overflow: auto;
  }
  .body.json {
    color: var(--text-0);
  }
  .logs .stderr {
    color: var(--err);
  }
  .small {
    font-size: var(--fs-xs);
    margin: 0;
  }
  @media (max-width: 900px) {
    .split {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
