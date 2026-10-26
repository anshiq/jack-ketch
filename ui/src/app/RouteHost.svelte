<script lang="ts">
  import { setContext } from 'svelte';
  import { matchPath } from '../lib/router.svelte';
  import { ROUTE_CONTEXT, type RouteEntry, type RouteView } from '../lib/routes';

  let { entry, active }: { entry: RouteEntry; active: boolean } = $props();

  const view: RouteView = {
    get path() {
      return entry.path;
    },
    get params() {
      return entry.params;
    },
    get active() {
      return active;
    },
    match: (pattern: string) => matchPath(pattern, entry.path)
  };

  setContext(ROUTE_CONTEXT, view);

  const View = $derived(entry.def.component);
</script>

<div class="route" data-route-active={active ? 'true' : 'false'} hidden={!active} inert={!active}>
  <View />
</div>

<style>
  .route {
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .route[hidden] {
    display: none;
  }
</style>
