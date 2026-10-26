<script lang="ts">
  import Check from '@lucide/svelte/icons/check';
  import Download from '@lucide/svelte/icons/download';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Sparkles from '@lucide/svelte/icons/sparkles';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import type { HarnessInfo, ScopeName, SkillState } from '../../lib/api/types';
  import { IntegrationService } from '../../lib/api';
  import Badge from '../../lib/ui/Badge.svelte';
  import Banner from '../../lib/ui/Banner.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import { dialogs } from '../../lib/state/dialogs.svelte';
  import { toastError, toasts } from '../../lib/toasts.svelte';
  import { scopeOf } from './harness';
  import { countLabel, primaryActionLabel, removeActionLabel, skillBadge, skillCounts, versionLabel } from './skills';

  let { harnesses, scope, projectPath, focus }: { harnesses: HarnessInfo[]; scope: ScopeName; projectPath: string; focus: string } = $props();

  let selected = $state('');
  let states = $state<SkillState[]>([]);
  let loading = $state(false);
  let busy = $state('');
  let error = $state('');

  const agents = $derived(harnesses.filter((h) => h.skillsSupported));
  const options = $derived(agents.map((h) => ({ value: h.id, label: h.displayName })));
  const current = $derived(agents.find((h) => h.id === selected) ?? null);
  const counts = $derived(skillCounts(states));
  const allCurrent = $derived(states.length > 0 && counts.missing === 0 && counts.outdated === 0);
  const canManage = $derived(!!selected && scope !== 'local' && (scope === 'user' || !!projectPath));
  const needsProject = $derived(scope !== 'user' && scope !== 'local' && !projectPath);
  const action = $derived(primaryActionLabel(states));
  const removeLabel = $derived(removeActionLabel(counts.owned));

  let run = 0;

  $effect(() => {
    if (focus) selected = focus;
  });

  $effect(() => {
    if (!selected && agents.length > 0) {
      selected = agents.find((h) => h.connected > 0 && scopeOf(h, scope)?.status === 'installed')?.id ?? agents[0]?.id ?? '';
    }
  });

  $effect(() => {
    void selected;
    void scope;
    void projectPath;
    void load();
  });

  async function load() {
    const id = selected;
    const token = ++run;
    states = [];
    error = '';
    if (!id || scope === 'local' || (scope !== 'user' && !projectPath)) {
      loading = false;
      return;
    }
    loading = true;
    try {
      const next = await IntegrationService.skillStatus(id, scope, projectPath);
      if (token !== run) return;
      states = next;
    } catch (err) {
      if (token !== run) return;
      error = err instanceof Error ? err.message : String(err);
    } finally {
      if (token === run) loading = false;
    }
  }

  async function install() {
    if (!selected || !canManage) return;
    const id = selected;
    const sc = scope;
    const proj = projectPath;
    const before = skillCounts(states);
    busy = 'install';
    try {
      const next = await IntegrationService.installSkills(id, sc, proj);
      if (selected !== id || scope !== sc || projectPath !== proj) return;
      states = next;
      const after = skillCounts(next);
      const parts: string[] = [];
      const added = before.missing - after.missing;
      const updated = before.outdated - after.outdated;
      if (added > 0) parts.push(`installed ${countLabel(added, 'skill')}`);
      if (updated > 0) parts.push(`updated ${countLabel(updated, 'skill')}`);
      toasts.ok(`${current?.displayName ?? id}: ${parts.join(', ') || 'skills are up to date'}`);
      error = '';
    } catch (err) {
      toastError(err);
    } finally {
      busy = '';
    }
  }

  async function remove() {
    if (!selected || !canManage || counts.owned === 0) return;
    const id = selected;
    const sc = scope;
    const proj = projectPath;
    const name = current?.displayName ?? id;
    const ok = await dialogs.confirm(`Remove ${countLabel(counts.owned, 'skill folder')} for ${name}? Other agents that read this folder will stop seeing those skills.`, {
      title: 'Remove skills',
      danger: true,
      confirmLabel: 'Remove'
    });
    if (!ok) return;
    busy = 'remove';
    try {
      const result = await IntegrationService.removeSkills(id, sc, proj);
      if (selected === id && scope === sc && projectPath === proj) {
        const n = result.removed.length;
        if (n === 0) toasts.info('No owned skill folders were removed');
        else toasts.ok(`Removed ${countLabel(n, 'skill folder')} for ${name}`);
        await load();
      }
    } catch (err) {
      toastError(err);
    } finally {
      busy = '';
    }
  }
</script>

<section class="panel">
  <header>
    <div class="titles">
      <h2><Sparkles size={16} />Skills</h2>
      <p>Small instruction packs that teach agents how to drive jack-ketch.</p>
    </div>
    <div class="picker">
      <span class="field-label">Agent</span>
      <Select bind:value={selected} {options} label="Agent for skill installs" size="sm" disabled={agents.length === 0 || busy !== ''} />
    </div>
  </header>

  <p class="hint">
    {#if scope === 'local'}
      Skills live in shared user or project folders, so local scope is not available.
    {:else}
      Skills are shared when agents read the same folder. Update changes the copy this agent reads. Remove deletes only folders this agent owns, so agents reading them lose access.
    {/if}
  </p>

  <div class="controls">
    <div class="summary">
      {#if counts.owned > 0}<Badge tone="ok" size="sm">{countLabel(counts.owned, 'installed skill')}</Badge>{/if}
      {#if counts.outdated > 0}<Badge tone="warn" size="sm">{countLabel(counts.outdated, 'update')}</Badge>{/if}
      {#if counts.missing > 0}<Badge tone="neutral" size="sm">{countLabel(counts.missing, 'missing skill')}</Badge>{/if}
      {#if counts.shared > 0}<Badge tone="info" size="sm">{countLabel(counts.shared, 'shared skill')}</Badge>{/if}
    </div>
    <div class="actions">
      <Button
        variant={allCurrent ? 'secondary' : 'primary'}
        size="sm"
        disabled={!canManage || allCurrent || busy !== '' || loading}
        loading={busy === 'install'}
        onclick={() => void install()}
        title={allCurrent ? 'Every bundled skill is installed and current' : action}
      >
        {#if allCurrent}<Check size={14} />{:else if counts.outdated > 0 && counts.missing === 0}<RefreshCw size={14} />{:else}<Download size={14} />{/if}
        {action}
      </Button>
      <Button
        variant="danger"
        size="sm"
        disabled={!canManage || counts.owned === 0 || busy !== '' || loading}
        loading={busy === 'remove'}
        onclick={() => void remove()}
        title={counts.owned === 0 ? 'No skill folders owned by this agent' : 'Remove skill folders owned by this agent'}
      >
        <Trash2 size={13} />{removeLabel}
      </Button>
    </div>
  </div>

  {#if error}
    <Banner tone="err" message={error} />
  {:else if agents.length === 0}
    <p class="empty">No agent with Agent Skills support is available. Connect an agent above first.</p>
  {:else if scope === 'local'}
    <p class="empty">Switch the install scope above to manage skills.</p>
  {:else if needsProject}
    <p class="empty">Open a project from the top bar to manage project-scoped skills.</p>
  {:else if !selected}
    <p class="empty">Choose an agent to manage its skills.</p>
  {:else if loading}
    <div class="skeletons">
      {#each Array(3) as _, i (i)}
        <Skeleton width="100%" height="52px" />
      {/each}
    </div>
  {:else if states.length === 0}
    <p class="empty">No bundled skills found.</p>
  {:else}
    <div class="table-wrap">
      <table class="data">
        <thead>
          <tr><th>Skill</th><th>Version</th><th>Location</th><th>Status</th></tr>
        </thead>
        <tbody>
          {#each states as st (st.name)}
            {@const badge = skillBadge(st)}
            <tr>
              <td>
                <div class="skill">
                  <span class="name">{st.name}</span>
                  {#if st.description}<span class="desc" title={st.description}>{st.description}</span>{/if}
                </div>
              </td>
              <td class="mono"><span class="version" class:outdated={st.outdated} class:muted={!st.visible}>{versionLabel(st)}</span></td>
              <td>
                <div class="loc-row">
                  {#if st.visible}
                    <code class="loc mono" title={st.dir}>{st.dir}</code>
                    {#if st.shared}<Badge tone="info" size="sm">Shared</Badge>{/if}
                  {:else}
                    <code class="loc mono target" title={st.target}>→ {st.target || '—'}</code>
                  {/if}
                </div>
              </td>
              <td><Badge tone={badge.tone} size="sm">{badge.label}</Badge></td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-5);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-1);
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-4);
    flex-wrap: wrap;
  }
  .titles h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 0 2px;
    font-size: var(--fs-lg);
    font-weight: 600;
    color: var(--text-0);
  }
  .titles p {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-sm);
  }
  .picker {
    width: 240px;
    max-width: 100%;
  }
  .field-label {
    display: block;
    margin-bottom: 4px;
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .hint {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-sm);
    line-height: 1.5;
  }
  .controls {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    flex-wrap: wrap;
  }
  .summary {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    min-height: 24px;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .empty {
    margin: 0;
    padding: var(--space-4);
    border: 1px dashed var(--border);
    border-radius: var(--radius);
    color: var(--text-2);
    font-size: var(--fs-sm);
    text-align: center;
  }
  .skeletons {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .table-wrap {
    overflow-x: auto;
  }
  .skill {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 180px;
  }
  .skill .name {
    color: var(--text-0);
    font-weight: 500;
  }
  .desc {
    display: -webkit-box;
    max-width: 520px;
    overflow: hidden;
    color: var(--text-2);
    font-size: var(--fs-xs);
    line-height: 1.35;
    -webkit-box-orient: vertical;
    line-clamp: 2;
    -webkit-line-clamp: 2;
  }
  .version {
    color: var(--text-1);
    font-size: var(--fs-xs);
    white-space: nowrap;
  }
  .version.outdated {
    color: var(--warn);
  }
  .version.muted {
    color: var(--text-2);
  }
  .loc-row {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .loc {
    max-width: 340px;
    overflow: hidden;
    color: var(--text-2);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .loc.target {
    color: var(--text-2);
  }
</style>
