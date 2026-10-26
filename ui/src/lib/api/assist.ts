import { call } from './transport';
import type {
  CheckSpec,
  ChecksResult,
  Diagnosis,
  DiagnosisIssue,
  EnvSourceSpec,
  GitignoreStatus,
  ProbeResult,
  SaveRunResult,
  SaveSuggestion
} from './types';

type Rec = Record<string, unknown>;

function rec(v: unknown): Rec {
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as Rec) : {};
}

function s(v: unknown, d = ''): string {
  return typeof v === 'string' ? v : d;
}

function n(v: unknown, d = 0): number {
  return typeof v === 'number' && Number.isFinite(v) ? v : d;
}

function list(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}

function strs(v: unknown): string[] {
  return list(v).filter((x): x is string => typeof x === 'string');
}

function pick(o: Rec, ...keys: string[]): unknown {
  for (const k of keys) if (o[k] !== undefined && o[k] !== null) return o[k];
  return undefined;
}

export function toEnvSource(raw: unknown): EnvSourceSpec {
  const o = rec(raw);
  const out: EnvSourceSpec = {};
  const file = s(o['file']);
  if (file) out.file = file;
  if (o['optional'] === true) out.optional = true;
  const vars = o['vars'];
  if (Array.isArray(vars)) out.vars = strs(vars);
  else if (vars && typeof vars === 'object') out.vars = Object.entries(vars as Rec).map(([k, v]) => `${k}=${v == null ? '' : String(v)}`);
  return out;
}

export function toCheck(raw: unknown): CheckSpec | null {
  const o = rec(raw);
  const name = s(o['name']);
  if (!name) return null;
  const c: CheckSpec = { name };
  const method = s(o['method']);
  if (method) c.method = method;
  const url = s(o['url']);
  if (url) c.url = url;
  const tcp = s(o['tcp']);
  if (tcp) c.tcp = tcp;
  const headers = rec(o['headers']);
  if (Object.keys(headers).length > 0) c.headers = Object.fromEntries(Object.entries(headers).map(([k, v]) => [k, String(v)]));
  const body = s(o['body']);
  if (body) c.body = body;
  const es = n(pick(o, 'expect_status', 'expectStatus'));
  if (es) c.expect_status = es;
  const ec = s(pick(o, 'expect_contains', 'expectContains'));
  if (ec) c.expect_contains = ec;
  const t = n(pick(o, 'timeout_ms', 'timeoutMs'));
  if (t) c.timeout_ms = t;
  return c;
}

export function toSuggestion(raw: unknown): SaveSuggestion | null {
  const o = rec(raw);
  if (!s(o['process_id']) && !s(o['processId'])) return null;
  return {
    processId: s(pick(o, 'process_id', 'processId')),
    kind: s(o['kind']),
    reason: s(o['reason']),
    app: s(o['app']),
    mode: s(o['mode']),
    newApp: pick(o, 'new_app', 'newApp') === true,
    modeExists: pick(o, 'mode_exists', 'modeExists') === true,
    type: s(o['type']),
    command: strs(o['command']),
    workdir: s(o['workdir']),
    envSources: list(pick(o, 'env_sources', 'envSources')).map(toEnvSource),
    yaml: s(o['yaml']),
    ask: s(o['ask'])
  };
}

function toIssue(raw: unknown): DiagnosisIssue {
  const o = rec(raw);
  return {
    category: s(o['category']),
    summary: s(o['summary']),
    actor: s(o['actor']),
    evidence: strs(o['evidence']),
    steps: strs(o['steps']),
    fixes: list(o['fixes']).map((f) => {
      const r = rec(f);
      return {
        kind: s(r['kind']),
        description: s(r['description']),
        command: s(r['command']),
        workdir: s(r['workdir']),
        needsApproval: pick(r, 'needs_approval', 'needsApproval') === true
      };
    })
  };
}

export function toDiagnosis(raw: unknown): Diagnosis {
  const o = rec(raw);
  const svc = rec(o['service']);
  const exit = pick(o, 'exit_code', 'exitCode');
  return {
    processId: s(pick(o, 'process_id', 'processId')),
    status: s(o['status']),
    exitCode: typeof exit === 'number' ? exit : null,
    failing: o['failing'] === true,
    category: s(o['category'], 'none'),
    actor: s(o['actor']),
    summary: s(o['summary']),
    issues: list(o['issues']).map(toIssue),
    missingEnv: list(pick(o, 'missing_env', 'missingEnv')).map((m) => {
      const r = rec(m);
      return { key: s(r['key']), hint: s(r['hint']), example: s(r['example']), source: s(r['source']), addTo: s(pick(r, 'add_to', 'addTo')) };
    }),
    envFiles: strs(pick(o, 'env_files', 'envFiles')),
    service: Object.keys(svc).length > 0
      ? { name: s(svc['name']), host: s(svc['host']), port: n(svc['port']), image: s(svc['image']), runHint: s(pick(svc, 'run_hint', 'runHint')), projectApp: s(pick(svc, 'project_app', 'projectApp')) }
      : null,
    tail: strs(o['tail']),
    guidance: s(o['guidance'])
  };
}

export function toProbe(raw: unknown): ProbeResult {
  const o = rec(raw);
  const headers = rec(o['headers']);
  return {
    name: s(o['name']),
    processId: s(pick(o, 'process_id', 'processId')),
    method: s(o['method']),
    url: s(o['url']),
    tcp: s(o['tcp']),
    ok: o['ok'] === true,
    status: n(o['status']),
    statusText: s(pick(o, 'status_text', 'statusText')),
    durationMs: n(pick(o, 'duration_ms', 'durationMs')),
    headers: Object.fromEntries(Object.entries(headers).map(([k, v]) => [k, String(v)])),
    body: s(o['body']),
    bodyBytes: n(pick(o, 'body_bytes', 'bodyBytes')),
    bodyTruncated: pick(o, 'body_truncated', 'bodyTruncated') === true,
    error: s(o['error']),
    failures: strs(o['failures']),
    logs: list(o['logs']).map((l) => {
      const r = rec(l);
      return { id: n(r['id']), timestamp: s(r['timestamp']), stream: s(r['stream'], 'stdout'), line: s(r['line']) };
    }),
    logsTruncated: pick(o, 'logs_truncated', 'logsTruncated') === true,
    errorLogs: n(pick(o, 'error_logs', 'errorLogs')),
    listeningPorts: list(pick(o, 'listening_ports', 'listeningPorts')).filter((x): x is number => typeof x === 'number'),
    processStatus: s(pick(o, 'process_status', 'processStatus')),
    check: toCheck(o['check']),
    needsConfirmation: o['needsConfirmation'] === true,
    confirmReason: s(o['reason'])
  };
}

function toGitignore(raw: unknown): GitignoreStatus {
  const o = rec(raw);
  return {
    repo: o['repo'] === true,
    repoRoot: s(pick(o, 'repo_root', 'repoRoot')),
    gitignore: s(o['gitignore']),
    missing: list(o['missing']).map((m) => {
      const r = rec(m);
      return { path: s(r['path']), kind: s(r['kind']), reason: s(r['reason']), exists: r['exists'] === true };
    }),
    tracked: strs(o['tracked'])
  };
}

export const AssistService = {
  diagnose: async (processId: string): Promise<Diagnosis> =>
    toDiagnosis(await call<{ processId: string }, Rec>('ProcessService', 'Diagnose', { processId })),
  suggestion: async (processId: string): Promise<SaveSuggestion | null> => {
    const r = await call<{ processId: string }, { suggestion: unknown }>('ProcessService', 'SaveSuggestion', { processId });
    return r.suggestion ? toSuggestion(r.suggestion) : null;
  },
  probe: async (req: {
    processId: string;
    method?: string;
    url?: string;
    tcp?: string;
    port?: number;
    headers?: Record<string, string>;
    body?: string;
    expectStatus?: number;
    expectContains?: string;
    timeoutMs?: number;
    confirmed?: boolean;
  }): Promise<ProbeResult> => {
    const body: Rec = {
      process_id: req.processId,
      method: req.method,
      url: req.url,
      tcp: req.tcp,
      port: req.port,
      headers: req.headers,
      body: req.body,
      expect_status: req.expectStatus,
      expect_contains: req.expectContains,
      timeout_ms: req.timeoutMs,
      confirmed: req.confirmed
    };
    const r = await call<Rec, Rec>('ProcessService', 'Probe', body, { timeoutMs: (req.timeoutMs ?? 10000) + 20000 });
    return toProbe(r);
  },
  runChecks: async (req: { processId?: string; workspaceId?: string; app?: string; mode?: string; names?: string[] }): Promise<ChecksResult> => {
    const r = await call<Rec, Rec>('ProcessService', 'RunChecks', { process_id: req.processId, workspaceId: req.workspaceId, app: req.app, mode: req.mode, names: req.names }, { timeoutMs: 300000 });
    return {
      processId: s(pick(r, 'process_id', 'processId')),
      app: s(r['app']),
      mode: s(r['mode']),
      passed: n(r['passed']),
      failed: n(r['failed']),
      results: list(r['results']).map(toProbe)
    };
  },
  saveRun: async (req: {
    processId: string;
    action?: 'save' | 'dismiss';
    app?: string;
    mode?: string;
    scope?: 'project' | 'workspace';
    description?: string;
    setDefault?: boolean;
    overwrite?: boolean;
    envSources?: EnvSourceSpec[];
    checks?: CheckSpec[];
  }): Promise<SaveRunResult> => {
    const r = await call<Rec, Rec>('ConfigService', 'SaveRun', {
      process_id: req.processId,
      action: req.action,
      app: req.app,
      mode: req.mode,
      scope: req.scope,
      description: req.description,
      set_default: req.setDefault,
      overwrite: req.overwrite,
      env_sources: req.envSources,
      checks: req.checks
    });
    return {
      saved: r['saved'] === true,
      dismissed: r['dismissed'] === true,
      app: s(r['app']),
      mode: s(r['mode']),
      scope: s(r['scope']),
      newApp: pick(r, 'new_app', 'newApp') === true,
      replaced: r['replaced'] === true,
      unchanged: r['unchanged'] === true,
      revision: n(r['revision']),
      yaml: s(r['yaml']),
      startWith: s(pick(r, 'start_with', 'startWith')),
      message: s(r['message']),
      gitignore: r['gitignore'] ? toGitignore(r['gitignore']) : null
    };
  },
  gitignoreStatus: async (workspaceId: string): Promise<GitignoreStatus> =>
    toGitignore(await call<{ workspaceId: string }, Rec>('ConfigService', 'GitignoreStatus', { workspaceId })),
  updateGitignore: async (workspaceId: string, entries: string[], action: 'add' | 'dismiss' = 'add'): Promise<{ added: string[]; hint: string }> => {
    const r = await call<Rec, Rec>('ConfigService', 'UpdateGitignore', { workspaceId, entries, action });
    return { added: strs(r['added']), hint: s(r['hint']) };
  }
};
