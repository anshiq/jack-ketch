<script lang="ts">
  import { cache } from '../data/query.svelte';

  let visible = $state(false);

  $effect(() => {
    const busy = cache.foreground > 0;
    if (!busy) {
      visible = false;
      return;
    }
    const t = setTimeout(() => (visible = true), 150);
    return () => clearTimeout(t);
  });
</script>

{#if visible}
  <div class="bar" role="progressbar" aria-label="Loading"></div>
{/if}

<style>
  .bar {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    height: 2px;
    z-index: 1200;
    background: linear-gradient(90deg, transparent, var(--accent), transparent);
    background-size: 40% 100%;
    animation: slide 1.1s linear infinite;
    pointer-events: none;
  }
  @keyframes slide {
    from {
      background-position: -40% 0;
    }
    to {
      background-position: 140% 0;
    }
  }
</style>
