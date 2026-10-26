import type { Component } from 'svelte';
import { matchPath } from './router.svelte';
import OverviewPage from '../routes/pages/OverviewPage.svelte';
import ProcessesPage from '../routes/pages/ProcessesPage.svelte';
import ProcessPage from '../routes/pages/ProcessPage.svelte';
import LogsPage from '../routes/pages/LogsPage.svelte';
import AppsPage from '../routes/pages/AppsPage.svelte';
import ConfigPage from '../routes/pages/ConfigPage.svelte';
import ProjectsPage from '../routes/pages/ProjectsPage.svelte';
import EventsPage from '../routes/pages/EventsPage.svelte';
import SessionsPage from '../routes/pages/SessionsPage.svelte';
import IntegrationsPage from '../routes/pages/IntegrationsPage.svelte';
import AuditPage from '../routes/pages/AuditPage.svelte';
import SettingsPage from '../routes/pages/SettingsPage.svelte';

export interface RouteDef {
  id: string;
  pattern: string;
  component: Component;
  keepAlive: boolean | 'perParam';
  title: string;
}

export const routes: RouteDef[] = [
  { id: 'process', pattern: '/processes/:id', component: ProcessPage, keepAlive: 'perParam', title: 'Process detail · jack-ketch' },
  { id: 'processes', pattern: '/processes', component: ProcessesPage, keepAlive: true, title: 'Processes · jack-ketch' },
  { id: 'logs', pattern: '/logs', component: LogsPage, keepAlive: true, title: 'Logs · jack-ketch' },
  { id: 'apps', pattern: '/apps', component: AppsPage, keepAlive: true, title: 'Apps · jack-ketch' },
  { id: 'config', pattern: '/config/:sub?', component: ConfigPage, keepAlive: true, title: 'Config · jack-ketch' },
  { id: 'projects', pattern: '/projects', component: ProjectsPage, keepAlive: true, title: 'Projects · jack-ketch' },
  { id: 'events', pattern: '/events', component: EventsPage, keepAlive: true, title: 'Events · jack-ketch' },
  { id: 'sessions', pattern: '/sessions', component: SessionsPage, keepAlive: true, title: 'Sessions · jack-ketch' },
  { id: 'integrations', pattern: '/integrations', component: IntegrationsPage, keepAlive: true, title: 'Integrations · jack-ketch' },
  { id: 'audit', pattern: '/audit', component: AuditPage, keepAlive: true, title: 'Audit · jack-ketch' },
  { id: 'settings', pattern: '/settings', component: SettingsPage, keepAlive: true, title: 'Settings · jack-ketch' },
  { id: 'overview', pattern: '/', component: OverviewPage, keepAlive: true, title: 'Overview · jack-ketch' }
];

export interface ResolvedRoute {
  def: RouteDef;
  path: string;
  params: Record<string, string>;
  key: string;
}

export function resolveRoute(path: string): ResolvedRoute | null {
  for (const def of routes) {
    const match = matchPath(def.pattern, path);
    if (!match) continue;
    const key = def.keepAlive === 'perParam' ? `${def.id}:${match.params['id'] ?? match.path}` : def.id;
    return { def, path, params: match.params, key };
  }
  return null;
}

export interface RouteEntry {
  key: string;
  def: RouteDef;
  path: string;
  params: Record<string, string>;
}

export interface RouteView {
  readonly path: string;
  readonly params: Record<string, string>;
  readonly active: boolean;
  match(pattern: string): ReturnType<typeof matchPath>;
}

export const ROUTE_CONTEXT = Symbol('route');
