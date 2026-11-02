<script lang="ts">
  import Terminal from '@lucide/svelte/icons/terminal';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import type { CredentialField } from '../../lib/api/types';
  import { curlToFields, parseCurl } from '../../lib/curl';
  import { appliedAs, maskedValue } from '../../lib/credentialsView';
  import Dialog from '../../lib/ui/Dialog.svelte';

  let { onClose, onImport }: { onClose: () => void; onImport: (fields: CredentialField[], host: string) => void } = $props();

  let text = $state('');
  const parsed = $derived(parseCurl(text));
  const fields = $derived(parsed ? curlToFields(parsed) : []);
  const host = $derived.by(() => {
    try {
      return parsed?.url ? new URL(parsed.url).hostname : '';
    } catch {
      return '';
    }
  });
  const touched = $derived(text.trim() !== '');
</script>

<Dialog title="Import from cURL" description="In your browser's DevTools, right-click a request that works while signed in, choose Copy as cURL, and paste it here." icon={Terminal} width={640} {onClose}>
  {#snippet children()}
    <div class="body">
      <textarea class="mono" rows="7" bind:value={text} placeholder={"curl 'https://app.example.com/api/me' -H 'authorization: Bearer eyJ…' -H 'cookie: sid=…'"} aria-label="cURL command" spellcheck="false"></textarea>
      {#if touched && !parsed}
        <div class="banner warn"><TriangleAlert size={14} /><span>That does not look like a cURL command.</span></div>
      {:else if parsed && fields.length === 0}
        <div class="banner warn"><TriangleAlert size={14} /><span>No credentials found: only headers such as Authorization, Cookie or custom ones are imported.</span></div>
      {:else if fields.length > 0}
        <table class="found" aria-label="Detected fields">
          <thead><tr><th>Field</th><th>Applied as</th><th>Value</th></tr></thead>
          <tbody>
            {#each fields as f (f.name)}
              <tr>
                <td class="mono">{f.name}</td>
                <td class="mono dim">{appliedAs(f)}</td>
                <td class="mono dim">{maskedValue({ secret: f.secret, hasValue: f.hasValue, value: f.value })}</td>
              </tr>
            {/each}
          </tbody>
        </table>
        <p class="hint">You can rename fields and change which ones are secret before saving. Unrelated browser headers were skipped.</p>
      {/if}
    </div>
  {/snippet}
  {#snippet footer()}
    <button type="button" class="btn" onclick={onClose}>Cancel</button>
    <button type="button" class="btn primary" disabled={fields.length === 0} onclick={() => onImport(fields, host)}>Continue</button>
  {/snippet}
</Dialog>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  textarea {
    border-radius: var(--radius);
    border: 1px solid var(--border);
    background: var(--bg-0);
    color: var(--text-0);
    padding: 8px 10px;
    font-size: var(--fs-xs);
    resize: vertical;
  }
  textarea:focus {
    outline: none;
    border-color: var(--accent);
  }
  .found {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-xs);
  }
  .found th {
    text-align: left;
    color: var(--text-2);
    font-weight: 500;
    padding: 4px 8px;
    border-bottom: 1px solid var(--border);
  }
  .found td {
    padding: 4px 8px;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  }
  .dim {
    color: var(--text-2);
  }
</style>
