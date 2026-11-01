<script lang="ts">
  import Stethoscope from '@lucide/svelte/icons/stethoscope';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import UserRound from '@lucide/svelte/icons/user-round';
  import Bot from '@lucide/svelte/icons/bot';
  import ShieldQuestion from '@lucide/svelte/icons/shield-question';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import Database from '@lucide/svelte/icons/database';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import { AssistService } from '../../lib/api';
  import type { Diagnosis } from '../../lib/api/types';
  import { getPlatform } from '../../lib/platform';
  import Spinner from '../../lib/ui/Spinner.svelte';
  import { actorInfo, categoryLabel } from './assistView';

  let { processId, instanceKey = '' }: { processId: string; instanceKey?: string } = $props();

  let diag = $state<Diagnosis | null>(null);
  let loading = $state(false);
  let error = $state('');
  let copied = $state('');
  let showTail = $state(false);

  async function load() {
    loading = true;
    error = '';
    try {
      diag = await AssistService.diagnose(processId);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void processId;
    void instanceKey;
    void load();
  });

  async function copy(text: string, key: string) {
    try {
      await getPlatform().copyText(text);
      copied = key;
      setTimeout(() => {
        if (copied === key) copied = '';
      }, 1200);
    } catch {
      copied = '';
    }
  }

  const envSnippet = $derived(
    (diag?.missingEnv ?? [])
      .map((m) => `${m.hint ? `# ${m.hint}\n` : ''}${m.key}=${m.example}`)
      .join('\n')
  );
  const addTargets = $derived([...new Set((diag?.missingEnv ?? []).map((m) => m.addTo).filter(Boolean))]);
  const actor = $derived(actorInfo(diag?.actor ?? ''));

  function groupSteps(steps: string[]): { text: string; code: string[] }[] {
    const out: { text: string; code: string[] }[] = [];
    for (const step of steps) {
      const last = out[out.length - 1];
      if (step.startsWith('  ') && last) last.code.push(step.trim());
      else out.push({ text: step, code: [] });
    }
    return out;
  }
</script>

<div class="diag">
  <div class="bar">
    <Stethoscope size={15} />
    <strong>Diagnosis</strong>
    {#if diag}
      <span class="cat">{categoryLabel(diag.category)}</span>
    {/if}
    <span class="grow"></span>
    <button type="button" class="btn sm" onclick={() => void load()} disabled={loading} aria-label="Re-run diagnosis">
      {#if loading}<Spinner size={12} />{:else}<RefreshCw size={13} />{/if}Re-check
    </button>
  </div>

  <div class="scroll">
    {#if error}
      <div class="state err">Could not diagnose this process: {error}</div>
    {:else if !diag}
      <div class="state muted"><Spinner size={14} />Reading the recent output…</div>
    {:else if diag.category === 'none'}
      <div class="state ok"><CircleCheck size={20} /><span>{diag.summary}</span></div>
    {:else}
      <section class="headline {actor.tone}">
        <div class="who">
          {#if diag.actor === 'user'}<UserRound size={16} />{:else if diag.actor === 'agent_with_approval'}<ShieldQuestion size={16} />{:else}<Bot size={16} />{/if}
          <span>{actor.label}</span>
        </div>
        <p class="summary">{diag.summary}</p>
        <p class="explain">{actor.explain}</p>
      </section>

      {#if diag.missingEnv.length > 0}
        <section class="block">
          <header>
            <KeyRound size={14} />
            <h4>Missing environment variables</h4>
            <span class="grow"></span>
            <button type="button" class="btn sm ghost" onclick={() => void copy(envSnippet, 'env')} title="Copy as .env lines">
              {#if copied === 'env'}<Check size={12} />{:else}<Copy size={12} />{/if}Copy .env lines
            </button>
          </header>
          {#if addTargets.length > 0}
            <p class="hint">
              Add {diag.missingEnv.length === 1 ? 'it' : 'them'} to <span class="mono">{addTargets.join(', ')}</span>{#if diag.envFiles.length > 0}, one of the files this run loads (later files win: <span class="mono">{diag.envFiles.join(' › ')}</span>).{:else}.
                This run loads no env file through jack-ketch, so either the app reads that file itself, or save the run with the file as an env source.{/if}
            </p>
          {/if}
          <table>
            <thead><tr><th>Key</th><th>Where to get it</th><th>Example</th></tr></thead>
            <tbody>
              {#each diag.missingEnv as m (m.key)}
                <tr>
                  <td class="mono key">{m.key}</td>
                  <td>{m.hint || '—'}{#if m.source}<span class="src">from {m.source}</span>{/if}</td>
                  <td class="mono ex" title={m.example}>{m.example || '—'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </section>
      {/if}

      {#if diag.service}
        <section class="block">
          <header><Database size={14} /><h4>Dependency: {diag.service.name}</h4></header>
          <p class="hint">
            Expected at <span class="mono">{diag.service.host || 'localhost'}{diag.service.port ? `:${diag.service.port}` : ''}</span>.
            {#if diag.service.projectApp}This project declares the app <span class="mono">{diag.service.projectApp}</span> for it.{/if}
          </p>
          {#if diag.service.runHint}
            <div class="cmd">
              <code class="mono">{diag.service.runHint}</code>
              <button type="button" class="icon-btn" aria-label="Copy command" onclick={() => void copy(diag!.service!.runHint, 'svc')}>
                {#if copied === 'svc'}<Check size={12} />{:else}<Copy size={12} />{/if}
              </button>
            </div>
          {/if}
        </section>
      {/if}

      {#each diag.issues as iss, i (i)}
        {@const a = actorInfo(iss.actor)}
        <section class="block issue">
          <header>
            <span class="num">{i + 1}</span>
            <h4>{categoryLabel(iss.category)}</h4>
            <span class="pill {a.tone}">{a.short}</span>
          </header>
          {#if i > 0}<p class="summary sm">{iss.summary}</p>{/if}
          {#if iss.steps.length > 0}
            <ol class="steps">
              {#each groupSteps(iss.steps) as step, j (j)}
                <li>
                  {step.text}
                  {#if step.code.length > 0}<pre class="stepcode mono">{step.code.join('\n')}</pre>{/if}
                </li>
              {/each}
            </ol>
          {/if}
          {#if iss.fixes.length > 0}
            <div class="fixes">
              {#each iss.fixes as f, j (j)}
                <div class="fix">
                  <div class="fix-head">
                    <span>{f.description}</span>
                    {#if f.needsApproval}<span class="pill warn">needs approval</span>{:else if f.kind === 'user_action'}<span class="pill user">your action</span>{/if}
                    {#if f.workdir && f.workdir !== '.'}<span class="muted mono">in {f.workdir}</span>{/if}
                  </div>
                  {#if f.command}
                    <div class="cmd">
                      <code class="mono">{f.command}</code>
                      <button type="button" class="icon-btn" aria-label="Copy command" onclick={() => void copy(f.command, `f${i}-${j}`)}>
                        {#if copied === `f${i}-${j}`}<Check size={12} />{:else}<Copy size={12} />{/if}
                      </button>
                    </div>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
          {#if iss.evidence.length > 0}
            <pre class="evidence mono">{iss.evidence.join('\n')}</pre>
          {/if}
        </section>
      {/each}

      {#if diag.tail.length > 0}
        <section class="block">
          <button type="button" class="disclose" aria-expanded={showTail} onclick={() => (showTail = !showTail)}>
            <ChevronRight size={14} class={showTail ? 'rot' : ''} />Last output lines
          </button>
          {#if showTail}<pre class="evidence mono">{diag.tail.join('\n')}</pre>{/if}
        </section>
      {/if}
    {/if}
  </div>
</div>

<style>
  .diag {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-5);
    border-bottom: 1px solid var(--border);
    color: var(--text-1);
  }
  .bar strong {
    color: var(--text-0);
    font-size: var(--fs-sm);
  }
  .cat {
    padding: 1px 8px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-1);
    font-size: var(--fs-xs);
  }
  .grow {
    flex: 1;
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .state {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--fs-sm);
    padding: var(--space-6);
    justify-content: center;
  }
  .state.err {
    color: var(--err);
  }
  .state.ok {
    color: var(--ok);
  }
  .headline {
    border-radius: var(--radius-lg);
    padding: var(--space-4) var(--space-5);
    border: 1px solid var(--border);
    background: var(--bg-2);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .headline.user {
    border-color: color-mix(in srgb, var(--warn) 45%, var(--border));
    background: color-mix(in srgb, var(--warn) 8%, var(--bg-1));
  }
  .headline.approval {
    border-color: color-mix(in srgb, var(--info) 45%, var(--border));
    background: color-mix(in srgb, var(--info) 8%, var(--bg-1));
  }
  .headline.agent {
    border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
    background: color-mix(in srgb, var(--accent) 6%, var(--bg-1));
  }
  .who {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-1);
  }
  .summary {
    margin: 0;
    color: var(--text-0);
    font-size: var(--fs-md, var(--fs-sm));
    font-weight: 500;
    line-height: 1.45;
  }
  .summary.sm {
    font-size: var(--fs-sm);
    font-weight: 400;
  }
  .explain {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-sm);
    line-height: 1.45;
  }
  .block {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .block > header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    color: var(--text-1);
  }
  .block h4 {
    margin: 0;
    font-size: var(--fs-sm);
    color: var(--text-0);
  }
  .num {
    display: inline-grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: var(--bg-3);
    font-size: var(--fs-xs);
    font-weight: 600;
    color: var(--text-1);
    flex: none;
  }
  .pill {
    padding: 0 8px;
    border-radius: 999px;
    font-size: 11px;
    line-height: 18px;
    border: 1px solid var(--border);
    color: var(--text-1);
    white-space: nowrap;
  }
  .pill.user,
  .pill.warn {
    color: var(--warn);
    border-color: color-mix(in srgb, var(--warn) 40%, transparent);
  }
  .pill.approval {
    color: var(--info);
    border-color: color-mix(in srgb, var(--info) 40%, transparent);
  }
  .pill.agent {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 40%, transparent);
  }
  .hint {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-sm);
    line-height: 1.5;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-sm);
  }
  th {
    text-align: left;
    font-weight: 500;
    color: var(--text-2);
    font-size: var(--fs-xs);
    padding: 4px 8px;
    border-bottom: 1px solid var(--border);
  }
  td {
    padding: 6px 8px;
    border-bottom: 1px solid var(--border);
    vertical-align: top;
    color: var(--text-1);
  }
  td.key {
    color: var(--text-0);
    font-weight: 600;
    white-space: nowrap;
  }
  td.ex {
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .src {
    display: block;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .steps {
    margin: 0;
    padding-left: 20px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    color: var(--text-1);
    font-size: var(--fs-sm);
    line-height: 1.5;
  }
  .stepcode {
    margin: 4px 0 2px;
    padding: 6px 10px;
    border-radius: var(--radius-sm);
    background: var(--bg-0);
    border: 1px solid var(--border);
    font-size: var(--fs-xs);
    color: var(--text-0);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .fixes {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .fix {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .fix-head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
    font-size: var(--fs-sm);
    color: var(--text-0);
  }
  .cmd {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 6px 10px;
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    min-width: 0;
  }
  .cmd code {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-xs);
    color: var(--text-0);
    white-space: pre-wrap;
    word-break: break-all;
  }
  .icon-btn {
    flex: none;
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
  .icon-btn:hover {
    color: var(--text-0);
    border-color: var(--border);
  }
  .evidence {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius);
    background: var(--bg-0);
    border: 1px solid var(--border);
    font-size: var(--fs-xs);
    color: var(--text-1);
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 220px;
    overflow: auto;
  }
  .disclose {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-1);
    font-size: var(--fs-sm);
    cursor: pointer;
  }
  .disclose :global(.rot) {
    transform: rotate(90deg);
  }
</style>
