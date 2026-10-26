<script lang="ts">
  import Copy from '@lucide/svelte/icons/copy';
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import type { IntegrationSnippet } from '../../lib/api/types';
  import Banner from '../../lib/ui/Banner.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import { getPlatform } from '../../lib/platform';
  import { toasts } from '../../lib/toasts.svelte';

  let { snippet, loading, error, onclose }: { snippet: IntegrationSnippet | null; loading: boolean; error: string; onclose: () => void } = $props();

  async function copy(text: string, what: string) {
    try {
      await getPlatform().copyText(text);
      toasts.ok(`${what} copied`);
    } catch {
      toasts.err('Copy failed');
    }
  }
</script>

<Dialog title={snippet ? `Set up ${snippet.harness}` : 'Set up this agent'} width={640} onClose={onclose}>
  <div class="body">
    {#if loading}
      <Skeleton width="60%" height="14px" />
      <Skeleton width="100%" height="120px" />
    {:else if error}
      <Banner tone="err" title="Could not build the snippet" message={error} />
    {:else if snippet}
      {#if snippet.steps}<p class="steps">{snippet.steps}</p>{/if}
      {#if snippet.path}
        <p class="target">Target file <code class="path" title={snippet.path}>{snippet.path}</code></p>
      {/if}
      <div class="block">
        <div class="bar">
          <span>{snippet.format || 'config'}</span>
          <Button variant="ghost" size="sm" onclick={() => void copy(snippet.text, 'Snippet')}><Copy size={13} />Copy</Button>
        </div>
        <pre class="code">{snippet.text}</pre>
      </div>
      {#if snippet.cli}
        <div class="block">
          <div class="bar">
            <span>Command line</span>
            <Button variant="ghost" size="sm" onclick={() => void copy(snippet.cli, 'Command')}><Copy size={13} />Copy</Button>
          </div>
          <pre class="code wrap">{snippet.cli}</pre>
        </div>
      {/if}
      {#if snippet.deeplink}
        <a class="deeplink btn primary" href={snippet.deeplink}><ExternalLink size={14} />Open in app</a>
      {/if}
    {/if}
  </div>
</Dialog>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    max-height: 60vh;
    overflow: auto;
  }
  .steps {
    margin: 0;
    color: var(--text-1);
    font-size: var(--fs-sm);
  }
  .target {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-xs);
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .path {
    font-family: var(--font-mono);
    color: var(--text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .block {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    background: var(--bg-0);
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    padding: 2px 4px 2px 10px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-2);
    font-size: var(--fs-xs);
  }
  .code {
    margin: 0;
    padding: var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    color: var(--text-0);
    overflow: auto;
    white-space: pre;
  }
  .code.wrap {
    white-space: pre-wrap;
    word-break: break-all;
  }
  .deeplink {
    align-self: flex-start;
    text-decoration: none;
  }
</style>
