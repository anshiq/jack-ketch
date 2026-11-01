import type { EnvSourceSpec, Process } from '../../lib/api/types';

const CATEGORY_LABELS: Record<string, string> = {
  none: 'No problem found',
  port_in_use: 'Port already in use',
  missing_env: 'Missing or invalid environment',
  invalid_credentials: 'Credentials rejected',
  service_unreachable: 'Dependency not reachable',
  migration_pending: 'Database migrations pending',
  missing_dependency: 'Dependency not installed',
  missing_tool: 'Tool not on PATH',
  toolchain_version: 'Wrong runtime version',
  permission: 'Permission denied',
  missing_file: 'Missing file',
  out_of_memory: 'Out of memory',
  code_error: 'Code error',
  start_failed: 'Could not start',
  unknown: 'Unrecognised failure'
};

export function categoryLabel(cat: string): string {
  return CATEGORY_LABELS[cat] ?? cat.replace(/_/g, ' ');
}

export function actorInfo(actor: string): { label: string; short: string; tone: 'user' | 'approval' | 'agent'; explain: string } {
  switch (actor) {
    case 'user':
      return {
        label: 'You need to act',
        short: 'you',
        tone: 'user',
        explain: 'Only you can provide this (values, credentials, access). Follow the steps below, then restart the process. Agents are told not to guess these values.'
      };
    case 'agent_with_approval':
      return {
        label: 'Fixable with your approval',
        short: 'approval',
        tone: 'approval',
        explain: 'An agent can fix this by installing dependencies, starting a container or running migrations, but asks you first. Commands marked "needs approval" change your machine or database.'
      };
    default:
      return {
        label: 'Agent can fix this',
        short: 'agent',
        tone: 'agent',
        explain: 'This is a code or configuration problem an agent can fix directly, then restart the process.'
      };
  }
}

export function isFailing(p: Process): boolean {
  if (p.status === 'failed' || p.status === 'crashed') return true;
  if ((p.status === 'exited' || p.status === 'stopped') && p.exitCode !== null && p.exitCode !== 0) return true;
  return p.health === 'unhealthy';
}

export function isHealthyRun(p: Process): boolean {
  if (p.status === 'running' || p.status === 'ready') return p.health !== 'unhealthy';
  return p.status === 'exited' && p.exitCode === 0;
}

export function cleanSources(list: EnvSourceSpec[]): EnvSourceSpec[] {
  const out: EnvSourceSpec[] = [];
  for (const s of list) {
    if (s.file !== undefined) {
      const file = s.file.trim();
      if (file) out.push(s.optional ? { file, optional: true } : { file });
      continue;
    }
    const vars = (s.vars ?? []).map((v) => v.trim()).filter((v) => /^[A-Za-z_][A-Za-z0-9_.-]*=/.test(v));
    if (vars.length > 0) out.push({ vars });
  }
  return out;
}

export function parseHeaders(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (!line) continue;
    const i = line.indexOf(':');
    if (i <= 0) continue;
    out[line.slice(0, i).trim()] = line.slice(i + 1).trim();
  }
  return out;
}

export function prettyBody(body: string): { text: string; json: boolean } {
  const t = body.trim();
  if (t.startsWith('{') || t.startsWith('[')) {
    try {
      return { text: JSON.stringify(JSON.parse(t), null, 2), json: true };
    } catch {
      return { text: body, json: false };
    }
  }
  return { text: body, json: false };
}

export const NAME_RE = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$/;
