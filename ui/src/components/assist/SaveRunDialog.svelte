<script lang="ts">
  import { untrack } from 'svelte';
  import BookmarkPlus from '@lucide/svelte/icons/bookmark-plus';
  import FileText from '@lucide/svelte/icons/file-text';
  import Braces from '@lucide/svelte/icons/braces';
  import ArrowUp from '@lucide/svelte/icons/arrow-up';
  import ArrowDown from '@lucide/svelte/icons/arrow-down';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Plus from '@lucide/svelte/icons/plus';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import { AssistService, ConfigService } from '../../lib/api';
  import type { EnvSourceSpec, GitignoreStatus, Process, SaveRunResult, SaveSuggestion } from '../../lib/api/types';
  import { toasts, toastError } from '../../lib/toasts.svelte';
  import { commandLine, toConfigApps, type ConfigApp } from '../../lib/config/apps';
  import GitignorePrompt from './GitignorePrompt.svelte';
  import { NAME_RE, cleanSources } from './assistView';

  let { process, suggestion, onClose, onSaved }: { process: Process; suggestion: SaveSuggestion | null; onClose: () => void; onSaved?: (r: SaveRunResult) => void } = $props();

  interface Row {
    id: number;
    kind: 'file' | 'vars';
    file: string;
    optional: boolean;
    vars: string;
  }

  let seq = 0;
  function toRow(s: EnvSourceSpec): Row {
    return { id: ++seq, kind: s.file !== undefined ? 'file' : 'vars', file: s.file ?? '', optional: s.optional === true, vars: (s.vars ?? []).join('\n') };
  }

  const initial = untrack(() => suggestion);
  let app = $state(untrack(() => initial?.app ?? (process.app || '')));
  let mode = $state(untrack(() => initial?.mode ?? (process.mode || '')));
  let description = $state('');
  let scope = $state<'project' | 'workspace'>('project');
  let setDefault = $state(false);
  let overwrite = $state(false);
  let rows = $state<Row[]>((initial?.envSources ?? []).map(toRow));
  let apps = $state<ConfigApp[]>([]);
  let saving = $state(false);
  let dismissing = $state(false);
  let saved = $state<SaveRunResult | null>(null);
  let gitignore = $state<GitignoreStatus | null>(null);

  $effect(() => {
    const ws = process.workspaceId;
    if (!ws) return;
    void ConfigService.get(ws)
      .then((cfg) => {
        const c = cfg as unknown as { provenance?: Record<string, unknown>; autoApps?: unknown };
        apps = toConfigApps(cfg.apps, c.provenance ?? {}, c.autoApps ?? []);
      })
      .catch(() => (apps = []));
  });

  const target = $derived(apps.find((a) => a.name === app.trim()) ?? null);
  const modeTaken = $derived(!!target && target.modes.some((m) => m.name === mode.trim()));
  const appValid = $derived(NAME_RE.test(app.trim()));
  const modeValid = $derived(NAME_RE.test(mode.trim()));
  const command = $derived(initial?.command?.length ? initial.command : [process.command, ...process.args]);
  const workdir = $derived(initial?.workdir || process.workdir);
  const canSave = $derived(appValid && modeValid && !saving && (!modeTaken || overwrite));

  function move(i: number, dir: -1 | 1) {
    const j = i + dir;
    if (j < 0 || j >= rows.length) return;
    const next = [...rows];
    [next[i], next[j]] = [next[j] as Row, next[i] as Row];
    rows = next;
  }

  function remove(i: number) {
    rows = rows.filter((_, k) => k !== i);
  }

  function addRow(kind: 'file' | 'vars') {
    rows = [...rows, { id: ++seq, kind, file: kind === 'file' ? '.env' : '', optional: kind === 'file', vars: '' }];
  }

  function sources(): EnvSourceSpec[] {
    return cleanSources(rows.map((r) => (r.kind === 'file' ? { file: r.file, optional: r.optional } : { vars: r.vars.split('\n') })));
  }

  async function save(e?: Event) {
    e?.preventDefault();
    if (!canSave) return;
    saving = true;
    try {
      const res = await AssistService.saveRun({
        processId: process.id,
        app: app.trim(),
        mode: mode.trim(),
        scope,
        description: description.trim() || undefined,
        setDefault,
        overwrite,
        envSources: sources()
      });
      saved = res;
      onSaved?.(res);
      toasts.ok(res.unchanged ? 'Already saved with the same settings' : `Saved ${res.app} · ${res.mode}`);
      if (res.gitignore && res.gitignore.missing.length > 0) gitignore = res.gitignore;
      else onClose();
    } catch (err) {
      toastError(err);
    } finally {
      saving = false;
    }
  }

  async function dismiss() {
    dismissing = true;
    try {
      await AssistService.saveRun({ processId: process.id, action: 'dismiss' });
      toasts.ok("Won't suggest saving this run again");
      onClose();
    } catch (err) {
      toastError(err);
    } finally {
      dismissing = false;
    }
  }
</script>

<Dialog title={saved ? 'Saved' : 'Save this run as an app mode'} description={saved ? '' : 'Next time it starts in one click, or by an agent with start_process(app, mode), without rebuilding the command and env.'} icon={BookmarkPlus} width={640} {onClose}>
  {#if saved && gitignore}
    <div class="done">
      <p class="ok"><CircleCheck size={16} />Saved as <span class="mono">{saved.app} · {saved.mode}</span>. Start it with <span class="mono">{saved.startWith}</span>.</p>
      <GitignorePrompt workspaceId={process.workspaceId} status={gitignore} compact onDone={onClose} />
    </div>
  {:else}
    <form class="form" id="save-run-form" onsubmit={save}>
      <div class="cmd">
        <span class="label">Runs</span>
        <code class="mono" title={commandLine(command)}>{commandLine(command)}</code>
        <span class="label">in</span>
        <code class="mono wd" title={workdir}>{workdir || '.'}</code>
      </div>

      <div class="names">
        <label class="field">
          <span>App</span>
          <input class="mono" bind:value={app} list="save-run-apps" aria-label="App" aria-describedby="save-run-app-hint" aria-invalid={!appValid} spellcheck="false" />
          <datalist id="save-run-apps">{#each apps as a (a.name)}<option value={a.name}></option>{/each}</datalist>
          <small id="save-run-app-hint" class:warn={!appValid}>{!appValid ? 'Letters, digits, . _ - only' : target ? `Adds a mode to the existing app (${target.modes.length} mode${target.modes.length === 1 ? '' : 's'})` : 'Creates a new app'}</small>
        </label>
        <label class="field">
          <span>Mode</span>
          <input class="mono" bind:value={mode} aria-label="Mode" aria-describedby="save-run-mode-hint" aria-invalid={!modeValid} spellcheck="false" placeholder="dev, prod, headless, test…" />
          <small id="save-run-mode-hint" class:warn={!modeValid || (modeTaken && !overwrite)}>{!modeValid ? 'Letters, digits, . _ - only' : modeTaken ? 'This mode exists; tick replace to overwrite it' : 'One mode per way of running the app'}</small>
        </label>
      </div>

      <label class="field">
        <span>Description <span class="muted">(optional)</span></span>
        <input bind:value={description} aria-label="Description" placeholder="e.g. API against the staging database" />
      </label>

      <div class="field">
        <span>Env sources <span class="muted">— in priority order, later entries override earlier ones</span></span>
        {#if rows.length === 0}
          <p class="muted small">No env beyond the shell environment. Add a file or variables if this run needs them.</p>
        {/if}
        <ol class="sources">
          {#each rows as r, i (r.id)}
            <li>
              <span class="prio">{i + 1}</span>
              {#if r.kind === 'file'}
                <FileText size={14} />
                <input class="mono grow" bind:value={r.file} placeholder=".env.local" aria-label="Env file path" spellcheck="false" />
                <label class="opt"><input type="checkbox" bind:checked={r.optional} />optional</label>
              {:else}
                <Braces size={14} />
                <textarea class="mono grow" rows={Math.min(6, Math.max(2, r.vars.split('\n').length))} bind:value={r.vars} placeholder={'KEY=value\nPORT=3000'} aria-label="Variables, one KEY=value per line" spellcheck="false"></textarea>
              {/if}
              <span class="rowacts">
                <button type="button" class="icon-btn" aria-label="Move up" disabled={i === 0} onclick={() => move(i, -1)}><ArrowUp size={13} /></button>
                <button type="button" class="icon-btn" aria-label="Move down" disabled={i === rows.length - 1} onclick={() => move(i, 1)}><ArrowDown size={13} /></button>
                <button type="button" class="icon-btn danger" aria-label="Remove source" onclick={() => remove(i)}><Trash2 size={13} /></button>
              </span>
            </li>
          {/each}
        </ol>
        <div class="addrow">
          <button type="button" class="btn sm ghost" onclick={() => addRow('file')}><Plus size={13} />Env file</button>
          <button type="button" class="btn sm ghost" onclick={() => addRow('vars')}><Plus size={13} />Variables</button>
        </div>
      </div>

      <div class="opts">
        <div class="seg" role="radiogroup" aria-label="Where to save">
          <button type="button" role="radio" aria-checked={scope === 'project'} class:on={scope === 'project'} onclick={() => (scope = 'project')}>Whole project</button>
          <button type="button" role="radio" aria-checked={scope === 'workspace'} class:on={scope === 'workspace'} onclick={() => (scope = 'workspace')}>This checkout only</button>
        </div>
        <label class="chk"><input type="checkbox" bind:checked={setDefault} />Make it the default mode</label>
        {#if modeTaken}<label class="chk warn"><input type="checkbox" bind:checked={overwrite} />Replace the existing {mode} mode</label>{/if}
      </div>
    </form>
  {/if}

  {#snippet footer()}
    {#if !saved}
      <button type="button" class="btn ghost" disabled={dismissing || saving} onclick={dismiss} title="Never suggest saving this run again">{#if dismissing}<Spinner size={12} />{/if}Don't save</button>
      <span class="grow"></span>
      <button type="button" class="btn" onclick={onClose}>Cancel</button>
      <button type="submit" form="save-run-form" class="btn primary" disabled={!canSave}>{#if saving}<Spinner size={12} />{/if}Save mode</button>
    {:else}
      <button type="button" class="btn" onclick={onClose}>Close</button>
    {/if}
  {/snippet}
</Dialog>

<style>
  .form,
  .done {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .ok {
    margin: 0;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
    color: var(--ok);
    font-size: var(--fs-sm);
  }
  .ok .mono {
    color: var(--text-0);
  }
  .cmd {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 4px var(--space-3);
    align-items: center;
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    font-size: var(--fs-xs);
  }
  .cmd code {
    color: var(--text-0);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .cmd .wd {
    color: var(--text-1);
  }
  .label {
    color: var(--text-2);
  }
  .names {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-4);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: var(--fs-sm);
    color: var(--text-1);
    min-width: 0;
  }
  .field input,
  .field textarea {
    height: 32px;
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    color: var(--text-0);
    padding: 0 10px;
    font-size: var(--fs-sm);
  }
  .field textarea {
    height: auto;
    padding: 6px 10px;
    resize: vertical;
    font-size: var(--fs-xs);
  }
  .field input:focus,
  .field textarea:focus {
    outline: none;
    border-color: var(--accent);
  }
  .field input[aria-invalid='true'] {
    border-color: var(--err);
  }
  small {
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  small.warn {
    color: var(--warn);
  }
  .small {
    margin: 0;
    font-size: var(--fs-xs);
  }
  .sources {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .sources li {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    color: var(--text-2);
  }
  .sources li > :global(svg) {
    margin-top: 9px;
    flex: none;
  }
  .prio {
    flex: none;
    margin-top: 6px;
    width: 20px;
    height: 20px;
    display: inline-grid;
    place-items: center;
    border-radius: 50%;
    background: var(--bg-3);
    color: var(--text-1);
    font-size: 11px;
    font-weight: 600;
  }
  .grow {
    flex: 1;
    min-width: 0;
  }
  .opt {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-top: 7px;
    font-size: var(--fs-xs);
    color: var(--text-1);
    white-space: nowrap;
  }
  .rowacts {
    display: inline-flex;
    gap: 2px;
    margin-top: 4px;
  }
  .icon-btn {
    display: inline-grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border-radius: var(--radius-sm);
    border: 1px solid transparent;
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
  }
  .icon-btn:hover:not(:disabled) {
    color: var(--text-0);
    border-color: var(--border);
  }
  .icon-btn.danger:hover:not(:disabled) {
    color: var(--err);
  }
  .icon-btn:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .addrow {
    display: flex;
    gap: var(--space-2);
  }
  .opts {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-4);
  }
  .seg {
    display: inline-flex;
    padding: 2px;
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-2);
  }
  .seg button {
    padding: 4px 10px;
    border: none;
    border-radius: calc(var(--radius) - 2px);
    background: transparent;
    color: var(--text-1);
    font-size: var(--fs-xs);
    cursor: pointer;
  }
  .seg button.on {
    background: var(--bg-1);
    color: var(--text-0);
    box-shadow: 0 0 0 1px var(--border-strong);
  }
  .chk {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-sm);
    color: var(--text-1);
    cursor: pointer;
  }
  .chk.warn {
    color: var(--warn);
  }
  @media (max-width: 640px) {
    .names {
      grid-template-columns: 1fr;
    }
  }
</style>
