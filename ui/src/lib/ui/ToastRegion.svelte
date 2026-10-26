<script lang="ts">
  import { flip } from 'svelte/animate';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import Info from '@lucide/svelte/icons/info';
  import X from '@lucide/svelte/icons/x';
  import { toasts } from '../toasts.svelte';
  import { slide, reducedMotion } from './motion';
</script>

<div class="region" aria-live="polite" aria-relevant="additions">
  {#each toasts.visible as t (t.id)}
    <div
      class="toast {t.tone}"
      role={t.tone === 'err' ? 'alert' : 'status'}
      in:slide={{ y: 12 }}
      out:slide={{ x: 24, duration: 160 }}
      animate:flip={{ duration: reducedMotion() ? 0 : 180 }}
      onmouseenter={() => toasts.pause(t.id)}
      onmouseleave={() => toasts.resume(t.id)}
      onfocusin={() => toasts.pause(t.id)}
      onfocusout={() => toasts.resume(t.id)}
    >
      <span class="ico">
        {#if t.tone === 'ok'}<CircleCheck size={16} />{:else if t.tone === 'err'}<CircleAlert size={16} />{:else}<Info size={16} />{/if}
      </span>
      <span class="msg">{t.message}</span>
      {#if t.count > 1}<span class="count">×{t.count}</span>{/if}
      {#if t.action}
        <button
          class="action"
          type="button"
          onclick={() => {
            t.action?.onClick();
            toasts.dismiss(t.id);
          }}
        >
          {t.action.label}
        </button>
      {/if}
      <button class="close" type="button" onclick={() => toasts.dismiss(t.id)} aria-label="Dismiss notification"><X size={14} /></button>
    </div>
  {/each}
  {#if toasts.overflow > 0}<div class="more">+{toasts.overflow} more</div>{/if}
</div>

<style>
  .region {
    position: fixed;
    bottom: calc(var(--statusbar-h) + var(--space-5));
    right: var(--space-6);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    width: min(400px, calc(100vw - 2 * var(--space-5)));
    z-index: var(--z-toast, 1000);
    pointer-events: none;
  }
  .toast {
    --c: var(--info);
    pointer-events: auto;
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: 8px 10px;
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-left: 3px solid var(--c);
    border-radius: var(--radius);
    box-shadow: var(--shadow-pop);
    font-size: var(--fs-sm);
    color: var(--text-0);
  }
  .toast.ok {
    --c: var(--ok);
  }
  .toast.err {
    --c: var(--err);
  }
  .ico {
    display: inline-flex;
    padding-top: 1px;
    color: var(--c);
    flex: none;
  }
  .msg {
    flex: 1;
    min-width: 0;
    padding-top: 1px;
    line-height: 1.45;
    overflow-wrap: anywhere;
    white-space: pre-line;
  }
  .count {
    flex: none;
    align-self: center;
    padding: 1px 6px;
    border-radius: 999px;
    background: var(--bg-active);
    color: var(--text-1);
    font-size: var(--fs-micro);
    font-weight: 700;
  }
  .more {
    align-self: flex-end;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    color: var(--text-2);
    font-size: var(--fs-micro);
  }
  .action {
    height: 24px;
    padding: 0 8px;
    border: none;
    border-radius: var(--radius-sm);
    background: var(--accent-subtle);
    color: var(--accent);
    font-size: var(--fs-xs);
    font-weight: 600;
    white-space: nowrap;
    cursor: pointer;
  }
  .action:hover {
    background: color-mix(in srgb, var(--accent) 22%, transparent);
  }
  .close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 22px;
    height: 22px;
    padding: 0;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
  }
  .close:hover {
    background: var(--bg-hover);
    color: var(--text-0);
  }
</style>
