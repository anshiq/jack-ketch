import { call, openStream, type StreamMessage, type StreamState } from './transport';
import {
  toAuditEntry,
  toChange,
  toEvent,
  toHarnessInfo,
  toMergeReport,
  toNestedProject,
  toProcess,
  toResource,
  toRevision,
  toSession,
  toSkillState,
  toSnippet,
  toVerifyCheck
} from './normalize';
import type {
  AuditEntry,
  DaemonEvent,
  DirectoryListing,
  HarnessInfo,
  IntegrationChange,
  IntegrationSnippet,
  MergeReport,
  NestedProject,
  PlanResult,
  Process,
  ResourceSample,
  Revision,
  ScopeName,
  Session,
  SkillState,
  VerifyCheck
} from './types';

export type { StreamMessage, StreamState };

export interface RunningProcessRef {
  processId: string;
  app?: string;
  command: string;
  status: string;
  workspaceId: string;
  workspacePath: string;
}

export interface PortManagedRef {
  processId: string;
  app?: string;
  mode?: string;
  status: string;
  workspaceId: string;
  workspacePath: string;
}

export interface PortDeclared {
  workspaceId: string;
  workspacePath: string;
  app: string;
  name: string;
  port: number;
  default?: number;
}

export interface PortEntry {
  port: number;
  address?: string;
  pid?: number;
  command?: string;
  listening: boolean;
  managed?: PortManagedRef;
  declared?: PortDeclared[];
  conflict?: string;
  self?: boolean;
}

export interface PortsView {
  supported: boolean;
  ports: PortEntry[];
}

export interface ErrorSeries {
  processId: string;
  app?: string;
  command: string;
  status: string;
  workspaceId: string;
  counts: number[];
  total: number;
}

export interface ErrorRate {
  from: number;
  to: number;
  bucketMs: number;
  series: ErrorSeries[];
}

export const TimelineService = {
  errorRate: (workspaceId: string, windowMs: number, buckets: number) =>
    call<{ workspaceId: string; windowMs: number; buckets: number }, ErrorRate>('LogService', 'ErrorRate', { workspaceId, windowMs, buckets })
};

export interface UpdateStatus {
  pending: boolean;
  runningVersion: string;
  installedVersion?: string;
  executable?: string;
  runningProcesses: number;
  processes?: RunningProcessRef[];
  notice?: string;
}

type CB = (s: StreamState) => void;

export const SystemService = {
  version: () => call<Record<string, never>, { daemonVersion: string; apiVersion: string; minClientVersion: string; update?: UpdateStatus }>('SystemService', 'GetVersion', {}),
  updateStatus: () => call<Record<string, never>, UpdateStatus>('SystemService', 'GetUpdateStatus', {}),
  ports: () => call<Record<string, never>, PortsView>('SystemService', 'ListPorts', {}),
  killPortHolder: (port: number, pid: number, force = false) =>
    call<{ port: number; pid: number; force: boolean }, { signalled: boolean }>('SystemService', 'KillPortHolder', { port, pid, force }),
  health: () => call<Record<string, never>, { healthy: boolean; status: string; uptimeMs: number; uptimeSeconds: number; version: string; socketPath: string; dataDir: string; tcpAddr: string }>('SystemService', 'Health', {}),
  stats: () => call<Record<string, never>, { projects: number; processes: number; sessions: number; processesRunning: number; processesFailed: number; logDiskBytes: number; indexLagSeconds: number; uptimeSeconds: number }>('SystemService', 'GetStats', {}),
  getSettings: () => call<Record<string, never>, { settings: Record<string, unknown>; schema: Record<string, { type: string; enum?: string[]; description: string }> }>('SystemService', 'GetSettings', {}),
  updateSettings: (req: Record<string, unknown>) => call<Record<string, unknown>, { settings: Record<string, unknown> }>('SystemService', 'UpdateSettings', req),
  listDirectory: (req: { dir: string; prefix?: string; baseDir?: string; dirsOnly?: boolean; limit?: number }) =>
    call<typeof req, DirectoryListing>('SystemService', 'ListDirectory', req),
  shutdown: () => call<Record<string, never>, { draining: boolean }>('SystemService', 'Shutdown', {})
};

export const ProjectService = {
  resolve: (path: string) => call<{ path: string }, { projectId: string; workspaceId: string; projectName: string }>('ProjectService', 'Resolve', { path }),
  list: async (includeWorkspaces = false) => {
    const r = await call<{ includeWorkspaces: boolean }, { projects: Record<string, unknown>[] }>('ProjectService', 'ListProjects', { includeWorkspaces });
    return r.projects ?? [];
  },
  get: (projectId: string) => call<{ projectId: string }, { project: Record<string, unknown> }>('ProjectService', 'GetProject', { projectId }),
  update: (projectId: string, name: string) => call<{ projectId: string; name: string }, unknown>('ProjectService', 'UpdateProject', { projectId, name }),
  workspaces: async (projectId: string) => {
    const r = await call<{ projectId: string }, { workspaces: Record<string, unknown>[] }>('ProjectService', 'ListWorkspaces', { projectId });
    return r.workspaces ?? [];
  },
  linkWorkspace: (projectId: string, path: string) => call<{ projectId: string; path: string }, unknown>('ProjectService', 'LinkWorkspace', { projectId, path }),
  forget: (projectId: string) => call<{ projectId: string }, unknown>('ProjectService', 'ForgetProject', { projectId }),
  gc: () => call<Record<string, never>, { removedWorkspaces: string[] }>('ProjectService', 'GC', {}),
  nested: async (): Promise<NestedProject[]> => {
    const r = await call<Record<string, never>, { nested: Record<string, unknown>[] }>('ProjectService', 'ListNestedProjects', {});
    return (r.nested ?? []).map(toNestedProject);
  },
  mergeNested: async (childProjectId: string, parentProjectId: string, dryRun: boolean): Promise<MergeReport> =>
    toMergeReport(await call<Record<string, unknown>, Record<string, unknown>>('ProjectService', 'MergeNestedProject', { childProjectId, parentProjectId, dryRun }))
};

export const ProcessService = {
  list: async (workspaceId: string, allWorkspaces = false): Promise<Process[]> => {
    const r = await call<{ workspaceId: string; allWorkspaces: boolean }, { processes: Record<string, unknown>[] }>('ProcessService', 'List', { workspaceId, allWorkspaces });
    return (r.processes ?? []).map(toProcess);
  },
  get: async (processId: string): Promise<Process> => {
    const r = await call<{ processId: string }, Record<string, unknown>>('ProcessService', 'Get', { processId });
    return toProcess(r);
  },
  start: (req: Record<string, unknown>) => call<Record<string, unknown>, { processId: string; instanceId: string }>('ProcessService', 'Start', req),
  stop: (processId: string, signal?: string) => call<Record<string, unknown>, unknown>('ProcessService', 'Stop', { processId, signal }),
  restart: (processId: string) => call<{ processId: string }, { newInstanceId: string }>('ProcessService', 'Restart', { processId }),
  signal: (processId: string, signal: string) => call<{ processId: string; signal: string }, unknown>('ProcessService', 'Signal', { processId, signal }),
  sendStdin: (processId: string, data: string) => call<{ processId: string; data: string }, unknown>('ProcessService', 'SendStdin', { processId, data }),
  remove: (processId: string, force = false) => call<{ processId: string; force: boolean }, unknown>('ProcessService', 'Remove', { processId, force }),
  setRestartPolicy: (processId: string, policy: string) => call<{ processId: string; policy: string }, unknown>('ProcessService', 'SetRestartPolicy', { processId, policy }),
  getEnv: (processId: string, reveal = false, live = false) => call<{ processId: string; reveal: boolean; live: boolean }, { env: string[]; source?: Record<string, string>; redacted?: number }>('ProcessService', 'GetEnv', { processId, reveal, live }),
  openShell: (req: Record<string, unknown>) => call<Record<string, unknown>, { processId: string; instanceId: string }>('ProcessService', 'OpenShell', req),
  getResourceUsage: async (processId: string): Promise<ResourceSample> => {
    const r = await call<{ processId: string }, Record<string, unknown>>('ProcessService', 'GetResourceUsage', { processId });
    return toResource(r);
  },
  waitForExit: (processId: string, timeoutMs?: number) => call<{ processId: string; timeoutMs?: number }, { exited: boolean; exitCode?: number }>('ProcessService', 'WaitForExit', { processId, timeoutMs }),
  startStack: (workspaceId: string, apps: string[], timeoutMs?: number) => call<{ workspaceId: string; apps: string[]; timeoutMs?: number }, { started: { app: string; processId: string }[]; order: string[]; failed?: string; error?: string }>('ProcessService', 'StartStack', { workspaceId, apps, timeoutMs }),
};

export const LogService = {
  get: (processId: string, lines = 500) => call<{ processId: string; lines: number }, { entries: { line: string; stream: string; timestamp?: string; id?: number }[] }>('LogService', 'GetLogs', { processId, lines }),
  search: (req: Record<string, unknown>) => call<Record<string, unknown>, { matches: Record<string, unknown>[]; truncatedScan: boolean }>('LogService', 'SearchLogs', req),
  clear: (processId: string, stream = 'all') => call<{ processId: string; stream: string }, unknown>('LogService', 'ClearLogs', { processId, stream }),
  stats: (processId: string) => call<{ processId: string }, { stdoutLines: number; stderrLines: number; totalLines: number; diskBytes: number; segmentCount: number; firstTimestamp: string; lastTimestamp: string }>('LogService', 'GetLogStats', { processId }),
  waitForLog: (processId: string, contains?: string, pattern?: string, timeoutMs?: number) => call<Record<string, unknown>, { matched: boolean; entry?: { line: string } }>('LogService', 'WaitForLog', { processId, contains, pattern, timeoutMs }),
  exportLogs: (processId: string, format = 'ndjson', onMessage: (m: StreamMessage) => void, onState?: CB) =>
    openStream('LogService', 'ExportLogs', { processId, format }, { onMessage, onState })
};

export const ConfigService = {
  get: (workspaceId: string) => call<{ workspaceId: string }, { projectId: string; workspaceId: string; revision: number; configRevision: number; configSource: string; pendingProposal: { id: string; yaml: string; summary: { app: string; action: string; reason: string }[]; createdAt: number } | null; raw: Record<string, string>; layerErrors: Record<string, { line: number; column: number; path: string; message: string }[]>; layers: { name: string; path: string; exists: boolean; writable: boolean }[]; apps: Record<string, unknown> }>('ConfigService', 'GetConfig', { workspaceId }),
  schema: () => call<Record<string, never>, { schema: unknown }>('ConfigService', 'GetSchema', {}),
  validate: (yaml: string) => call<{ yaml: string }, { valid: boolean; errors: { line: number; column: number; path: string; message: string }[]; warnings?: string[] }>('ConfigService', 'Validate', { yaml }),
  plan: (workspaceId: string, yaml: string, baseRevision?: number) => call<{ workspaceId: string; yaml: string; baseRevision?: number }, PlanResult>('ConfigService', 'Plan', { workspaceId, yaml, baseRevision }),
  apply: (req: { projectId?: string; workspaceId?: string; layer?: string; yaml: string; baseRevision?: number; message?: string; restartAffected?: boolean }) =>
    call<Record<string, unknown>, { applied: boolean; revision?: number; restarted?: string[]; errors?: unknown[] }>('ConfigService', 'Apply', req),
  revisions: async (projectId: string, limit = 20): Promise<Revision[]> => {
    const r = await call<{ projectId: string; limit: number }, { revisions: Record<string, unknown>[] }>('ConfigService', 'ListRevisions', { projectId, limit });
    return (r.revisions ?? []).map(toRevision);
  },
  revision: (projectId: string, id: number) => call<{ projectId: string; revision: number }, { revision: Record<string, unknown>; content: string; previousContent: string }>('ConfigService', 'GetRevision', { projectId, revision: id }),
  rollback: (projectId: string, revision: number) => call<{ projectId: string; revision: number }, { applied: boolean; revision: number }>('ConfigService', 'Rollback', { projectId, revision }),
  resolveProposal: (projectId: string, proposalId: string, action: 'approve' | 'dismiss') =>
    call<{ projectId: string; proposalId: string; action: string }, { applied?: boolean; dismissed?: boolean; revision?: number }>('ConfigService', 'ResolveProposal', { projectId, proposalId, action }),
};

export const EventService = {
  list: async (
    workspaceId: string,
    limit = 100,
    extra?: { types?: string[]; before?: string; since?: string; processId?: string; order?: 'asc' | 'desc' }
  ): Promise<{ events: DaemonEvent[]; nextBefore: string; latestId: number; hasMore: boolean }> => {
    const r = await call<Record<string, unknown>, { events: Record<string, unknown>[]; nextBefore: string; latestId?: number; hasMore?: boolean }>('EventService', 'ListEvents', { workspaceId, limit, ...extra });
    return {
      events: (r.events ?? []).map(toEvent),
      nextBefore: r.nextBefore ?? '',
      latestId: r.latestId ?? 0,
      hasMore: r.hasMore === true
    };
  },
  watch: (workspaceId: string, onMessage: (m: StreamMessage) => void, onState?: CB, extra?: { types?: string[]; processIds?: string[]; since?: string; resume?: () => Record<string, unknown> }) =>
    openStream('EventService', 'WatchEvents', { workspaceId, types: extra?.types, processIds: extra?.processIds, since: extra?.since }, { onMessage, onState, resume: extra?.resume }),
  subscribe: (sessionId: string, types: string[], processId?: string) => call<Record<string, unknown>, { subscriptionId: string }>('EventService', 'Subscribe', { sessionId, types, processId }),
  drain: (subscriptionId: string, limit?: number) => call<Record<string, unknown>, { events: unknown[]; dropped: number }>('EventService', 'Drain', { subscriptionId, limit }),
  unsubscribe: (subscriptionId: string) => call<{ subscriptionId: string }, unknown>('EventService', 'Unsubscribe', { subscriptionId })
};

export const SessionService = {
  list: async (): Promise<Session[]> => {
    const r = await call<Record<string, never>, { sessions: Record<string, unknown>[] }>('SessionService', 'ListSessions', {});
    return (r.sessions ?? []).map(toSession);
  },
  get: (sessionId: string) => call<{ sessionId: string }, { session: Record<string, unknown> }>('SessionService', 'GetSession', { sessionId }),
  close: (sessionId: string) => call<{ sessionId: string }, unknown>('SessionService', 'Close', { sessionId }),
  register: (req: Record<string, unknown>) => call<Record<string, unknown>, { sessionId: string }>('SessionService', 'Register', req),
  ping: (sessionId: string) => call<{ sessionId: string }, { ok: boolean }>('SessionService', 'Ping', { sessionId }),
};

export const IntegrationService = {
  listHarnesses: async (projectPath = ''): Promise<HarnessInfo[]> => {
    const r = await call<{ projectPath: string }, { harnesses: Record<string, unknown>[] }>('IntegrationService', 'ListHarnesses', { projectPath }, { timeoutMs: 30000 });
    return (r.harnesses ?? []).map(toHarnessInfo);
  },
  preview: async (harness: string, scope: ScopeName, projectPath: string, action: 'install' | 'remove' = 'install'): Promise<IntegrationChange[]> => {
    const r = await call<Record<string, unknown>, { changes: Record<string, unknown>[] }>('IntegrationService', 'PreviewInstall', { harness, scope, projectPath, action });
    return (r.changes ?? []).map(toChange);
  },
  install: async (harness: string, scope: ScopeName, projectPath: string): Promise<IntegrationChange[]> => {
    const r = await call<Record<string, unknown>, { changes: Record<string, unknown>[] }>('IntegrationService', 'InstallMCP', { harness, scope, projectPath });
    return (r.changes ?? []).map(toChange);
  },
  remove: async (harness: string, scope: ScopeName, projectPath: string): Promise<IntegrationChange[]> => {
    const r = await call<Record<string, unknown>, { changes: Record<string, unknown>[] }>('IntegrationService', 'RemoveMCP', { harness, scope, projectPath });
    return (r.changes ?? []).map(toChange);
  },
  snippet: async (harness: string, scope: ScopeName, projectPath: string): Promise<IntegrationSnippet> =>
    toSnippet(await call<Record<string, unknown>, Record<string, unknown>>('IntegrationService', 'GetSnippet', { harness, scope, projectPath })),
  verify: async (harness: string, scope: ScopeName, projectPath: string): Promise<{ ok: boolean; checks: VerifyCheck[] }> => {
    const r = await call<Record<string, unknown>, { ok: boolean; checks: Record<string, unknown>[] }>('IntegrationService', 'Verify', { harness, scope, projectPath }, { timeoutMs: 60000 });
    return { ok: r.ok === true, checks: (r.checks ?? []).map(toVerifyCheck) };
  },
  listSkills: async (): Promise<{ name: string; version: string; description: string }[]> => {
    const r = await call<Record<string, never>, { skills: Record<string, unknown>[] }>('IntegrationService', 'ListSkills', {});
    return (r.skills ?? []).map((s) => ({ name: String(s['name'] ?? ''), version: String(s['version'] ?? ''), description: String(s['description'] ?? '') }));
  },
  skillStatus: async (harness: string, scope: ScopeName, projectPath: string): Promise<SkillState[]> => {
    const r = await call<Record<string, unknown>, { skills: Record<string, unknown>[] }>('IntegrationService', 'SkillStatus', { harness, scope, projectPath });
    return (r.skills ?? []).map(toSkillState);
  },
  installSkills: async (harness: string, scope: ScopeName, projectPath: string): Promise<SkillState[]> => {
    const r = await call<Record<string, unknown>, { skills: Record<string, unknown>[] }>('IntegrationService', 'InstallSkills', { harness, scope, projectPath });
    return (r.skills ?? []).map(toSkillState);
  },
  removeSkills: (harness: string, scope: ScopeName, projectPath: string) =>
    call<Record<string, unknown>, { removed: string[] }>('IntegrationService', 'RemoveSkills', { harness, scope, projectPath }),
  checkUpdates: (scope: ScopeName, projectPath: string) => call<Record<string, unknown>, { updates: Record<string, unknown>[]; at: number }>('IntegrationService', 'CheckUpdates', { scope, projectPath })
};

export const AuditService = {
  list: async (workspaceId: string, limit = 100): Promise<AuditEntry[]> => {
    const r = await call<{ workspaceId: string; limit: number }, { entries: Record<string, unknown>[] }>('AuditService', 'ListAudit', { workspaceId, limit });
    return (r.entries ?? []).map(toAuditEntry);
  }
};
