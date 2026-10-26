<script lang="ts">
  import { untrack } from 'svelte';
  import { router } from '../lib/router.svelte';
  import { resolveRoute, type ResolvedRoute, type RouteEntry } from '../lib/routes';
  import RouteHost from './RouteHost.svelte';
  import NotFoundPage from '../routes/pages/NotFoundPage.svelte';

  const MAX_PAGES = 8;
  const MAX_DETAILS = 3;

  let entries = $state<RouteEntry[]>([]);
  let activeKey = $state('');

  const current = $derived(resolveRoute(router.path));

  const ALIASES: Record<string, string> = { '/overview': '/', '/index.html': '/', '/process': '/processes', '/log': '/logs', '/project': '/projects' };

  $effect(() => {
    const route = current;
    untrack(() => {
      if (!route) {
        const [bare = '/', query] = router.path.split('?');
        const trimmed = bare.replace(/\/+$/, '') || '/';
        const alias = ALIASES[trimmed] ?? (trimmed !== bare && resolveRoute(trimmed) ? trimmed : null);
        if (alias) {
          void router.navigate(alias + (query ? `?${query}` : ''), { replace: true });
          return;
        }
        activeKey = '';
        return;
      }
      activate(route);
    });
  });

  function activate(route: ResolvedRoute) {
    activeKey = route.key;
    const index = entries.findIndex((e) => e.key === route.key);
    if (index < 0) {
      entries.push({ key: route.key, def: route.def, path: route.path, params: { ...route.params } });
    } else {
      const entry = entries[index];
      if (entry) {
        entry.path = route.path;
        entry.params = { ...route.params };
        entries.splice(index, 1);
        entries.push(entry);
      }
    }
    prune();
  }

  function prune() {
    trim(MAX_PAGES, (e) => e.def.keepAlive !== 'perParam');
    trim(MAX_DETAILS - 1, (e) => e.def.keepAlive === 'perParam');
  }

  function trim(limit: number, filter: (e: RouteEntry) => boolean) {
    let keep = entries.filter((e) => e.key !== activeKey && filter(e)).length;
    while (keep > limit) {
      const index = entries.findIndex((e) => e.key !== activeKey && filter(e));
      if (index < 0) return;
      entries.splice(index, 1);
      keep--;
    }
  }
</script>

{#each entries as entry (entry.key)}
  <RouteHost {entry} active={entry.key === activeKey} />
{/each}
{#if !current}
  <NotFoundPage />
{/if}
