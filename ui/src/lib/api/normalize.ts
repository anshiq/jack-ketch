import type {
  AuditEntry,
  DaemonEvent,
  EntryStatus,
  HarnessInfo,
  IntegrationChange,
  IntegrationSnippet,
  LogLine,
  Process,
  ResourceSample,
  Revision,
  ScopeReport,
  Session,
  SkillState,
  VerifyCheck
} from './types';

function num(v: unknown): number | null {
  if (typeof v === 'number') return v;
  if (typeof v === 'string' && v !== '') {
    const n = Date.parse(v);
    if (!Number.isNaN(n)) return n;
    const i = Number(v);
    if (!Number.isNaN(i)) return i;
  }
  return null;
}

function str(v: unknown, fallback = ''): string {
  return typeof v === 'string' ? v : fallback;
}

function arr(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
}

function numArr(v: unknown): number[] {
  if (!Array.isArray(v)) return [];
  return v.map((x) => (typeof x === 'number' ? x : Number(x))).filter((x) => Number.isFinite(x));
}

export function toProcess(raw: Record<string, unknown>): Process {
  const id = str(raw['id'] || raw['processId'] || raw['process_id']);
  const started = num(raw['startedAt'] ?? raw['started_at']) ?? num(raw['startedat']);
  const exited = num(raw['exitedAt'] ?? raw['exited_at']);
  const exitRaw = raw['exitCode'] ?? raw['exit_code'];
  return {
    id,
    instanceId: str(raw['instanceId'] ?? raw['instance_id']),
    app: str(raw['app']),
    workspaceId: str(raw['workspaceId'] ?? raw['workspace_id']),
    projectId: str(raw['projectId'] ?? raw['project_id']),
    command: str(raw['command']),
    args: arr(raw['args']),
    workdir: str(raw['workdir']),
    profile: str(raw['profile']),
    status: (str(raw['status'], 'unknown') as Process['status']),
    pid: typeof raw['pid'] === 'number' ? raw['pid'] : 0,
    restarts: typeof raw['restarts'] === 'number' ? raw['restarts'] : 0,
    health: str(raw['health'], 'unknown'),
    startedAt: started,
    exitedAt: exited,
    exitCode: typeof exitRaw === 'number' ? exitRaw : null,
    exitSignal: str(raw['exitSignal'] ?? raw['exit_signal']),
    restartPolicy: str(raw['restartPolicy'] ?? raw['restart_policy']),
    stdoutLines: typeof raw['stdoutLines'] === 'number' ? raw['stdoutLines'] : (typeof raw['stdout_lines'] === 'number' ? (raw['stdout_lines'] as number) : 0),
    stderrLines: typeof raw['stderrLines'] === 'number' ? raw['stderrLines'] : (typeof raw['stderr_lines'] === 'number' ? (raw['stderr_lines'] as number) : 0),
    ports: numArr(raw['ports']),
    stale: raw['stale'] === true,
    loaded: raw['loaded'] !== false,
    lifetime: str(raw['lifetime'], 'persistent'),
    sessionId: str(raw['sessionId'] ?? raw['session_id'] ?? raw['startedBy']),
    mode: str(raw['mode']),
    saveable: raw['saveable'] === true
  };
}

export function toLogLine(raw: Record<string, unknown>, fallbackProc = ''): LogLine {
  const tsRaw = raw['timestamp'] ?? raw['ts'];
  let ts = 0;
  if (typeof tsRaw === 'number') ts = tsRaw > 1e12 ? tsRaw : tsRaw / 1e6;
  else if (typeof tsRaw === 'string') ts = Date.parse(tsRaw) || 0;
  return {
    id: typeof raw['id'] === 'number' ? raw['id'] : 0,
    ts,
    stream: str(raw['stream'], 'stdout'),
    level: str(raw['level'], 'info'),
    text: str(raw['line'] ?? raw['data'] ?? raw['text']),
    proc: str(raw['processId'] ?? raw['process_id'], fallbackProc),
    instanceId: str(raw['instanceId'] ?? raw['instance_id']),
    cursor: str(raw['cursor'])
  };
}

export function toEvent(raw: Record<string, unknown>): DaemonEvent {
  let payload: Record<string, unknown> = {};
  const p = raw['payload'];
  if (p && typeof p === 'object') payload = p as Record<string, unknown>;
  else if (typeof p === 'string' && p !== '') {
    try {
      payload = JSON.parse(p) as Record<string, unknown>;
    } catch {
      payload = { message: p };
    }
  } else if (typeof raw['payload_json'] === 'string' && (raw['payload_json'] as string) !== '') {
    try {
      payload = JSON.parse(raw['payload_json'] as string) as Record<string, unknown>;
    } catch {
      payload = { message: raw['payload_json'] };
    }
  }
  const tsRaw = raw['ts'] ?? raw['timestamp'];
  let ts = 0;
  if (typeof tsRaw === 'number') ts = tsRaw > 1e12 ? Math.round(tsRaw / 1e6) : tsRaw;
  else if (typeof tsRaw === 'string') ts = Date.parse(tsRaw) || 0;
  const idNum = typeof raw['id'] === 'number' ? raw['id'] : Number(raw['cursor']) || 0;
  const curRaw = raw['cursor'] ?? raw['id'];
  return {
    id: idNum,
    ts,
    type: str(raw['type']),
    processId: str(raw['processId'] ?? raw['process_id']),
    instanceId: str(raw['instanceId'] ?? raw['instance_id']),
    workspaceId: str(raw['workspaceId'] ?? raw['workspace_id']),
    projectId: str(raw['projectId'] ?? raw['project_id']),
    sessionId: str(raw['sessionId'] ?? raw['session_id']),
    payload,
    cursor: typeof curRaw === 'string' ? curRaw : String(curRaw ?? idNum)
  };
}

export function toRevision(raw: Record<string, unknown>): Revision {
  return {
    id: typeof raw['id'] === 'number' ? raw['id'] : 0,
    layer: str(raw['layer']),
    time: typeof raw['createdAt'] === 'number' ? (raw['createdAt'] as number) * 1000 : (num(raw['createdAt']) ?? 0),
    source: str(raw['source']),
    session: str(raw['sessionId'] ?? raw['session_id']),
    message: str(raw['message']),
    valid: raw['valid'] !== false,
    sha: str(raw['sha256'] ?? raw['sha'])
  };
}

export function toEntryStatus(raw: Record<string, unknown>): EntryStatus {
  return {
    path: str(raw['path']),
    fileExists: raw['fileExists'] === true,
    installed: raw['installed'] === true,
    command: str(raw['command']),
    args: arr(raw['args']),
    commandFound: raw['commandFound'] === true,
    upToDate: raw['upToDate'] === true,
    problems: arr(raw['problems'])
  };
}

export function toScopeReport(raw: Record<string, unknown>): ScopeReport {
  const scope = raw['scope'];
  const status = raw['status'];
  return {
    scope: scope === 'project' || scope === 'local' ? scope : 'user',
    status: status === 'installed' || status === 'outdated' || status === 'broken' || status === 'unavailable' ? status : 'absent',
    portable: raw['portable'] === true,
    note: str(raw['note']),
    entries: Array.isArray(raw['entries']) ? (raw['entries'] as unknown[]).map((e) => toEntryStatus(e as Record<string, unknown>)) : [],
    error: str(raw['error'])
  };
}

export function toHarnessInfo(raw: Record<string, unknown>): HarnessInfo {
  const detection = raw['detection'] && typeof raw['detection'] === 'object' ? (raw['detection'] as Record<string, unknown>) : {};
  return {
    id: str(raw['id']),
    displayName: str(raw['displayName']),
    tier: raw['tier'] === 2 ? 2 : 1,
    docsUrl: str(raw['docsUrl']),
    detected: (raw['detected'] ?? detection['detected']) === true,
    version: str(raw['version'] ?? detection['version']),
    evidence: arr(raw['evidence'] ?? detection['evidence']),
    scopes: Array.isArray(raw['scopes']) ? (raw['scopes'] as unknown[]).map((s) => toScopeReport(s as Record<string, unknown>)) : [],
    skillsSupported: raw['skillsSupported'] === true,
    manualSteps: str(raw['manualSteps']),
    restartHint: str(raw['restartHint']),
    maxWaitMs: typeof raw['maxWaitMs'] === 'number' ? raw['maxWaitMs'] : 0,
    connected: typeof raw['connected'] === 'number' ? raw['connected'] : 0
  };
}

export function toChange(raw: Record<string, unknown>): IntegrationChange {
  const scope = raw['scope'];
  return {
    harness: str(raw['harness']),
    scope: scope === 'project' || scope === 'local' ? scope : 'user',
    path: str(raw['path']),
    existed: raw['existed'] === true,
    changed: raw['changed'] === true,
    diff: str(raw['diff']),
    backup: str(raw['backup']),
    restartHint: str(raw['restartHint']),
    note: str(raw['note'])
  };
}

export function toSnippet(raw: Record<string, unknown>): IntegrationSnippet {
  return {
    harness: str(raw['harness']),
    scope: str(raw['scope']),
    format: str(raw['format']),
    path: str(raw['path']),
    text: str(raw['text']),
    cli: str(raw['cli']),
    deeplink: str(raw['deeplink']),
    steps: str(raw['steps'])
  };
}

export function toVerifyCheck(raw: Record<string, unknown>): VerifyCheck {
  return {
    name: str(raw['name']),
    ok: raw['ok'] === true,
    detail: str(raw['detail']),
    path: str(raw['path'])
  };
}

export function toSkillState(raw: Record<string, unknown>): SkillState {
  return {
    name: str(raw['name']),
    description: str(raw['description']),
    want: str(raw['want']),
    have: str(raw['have']),
    dir: str(raw['dir']),
    target: str(raw['target']),
    visible: raw['visible'] === true,
    own: raw['own'] === true,
    shared: raw['shared'] === true,
    outdated: raw['outdated'] === true
  };
}

function epochMs(v: unknown): number {
  if (typeof v === 'number') return v < 1e11 ? v * 1000 : v > 1e14 ? Math.round(v / 1e6) : v;
  return num(v) ?? 0;
}

export function toSession(raw: Record<string, unknown>): Session {
  const closedAt = epochMs(raw['closedAt'] ?? raw['closed_at']);
  return {
    id: str(raw['id'] ?? raw['sessionId'] ?? raw['session_id']),
    kind: str(raw['kind'], 'mcp'),
    harness: str(raw['harness']),
    harnessVersion: str(raw['harnessVersion'] ?? raw['harness_version']),
    clientName: str(raw['clientName'] ?? raw['client_name']),
    clientPid: typeof raw['clientPid'] === 'number' ? (raw['clientPid'] as number) : typeof raw['client_pid'] === 'number' ? (raw['client_pid'] as number) : 0,
    workspaceId: str(raw['workspaceId'] ?? raw['workspace_id']),
    started: epochMs(raw['startedAt'] ?? raw['started_at']),
    lastSeen: epochMs(raw['lastSeenAt'] ?? raw['last_seen_at'] ?? raw['lastSeen'] ?? raw['last_seen']),
    closedAt,
    closed: raw['closed'] === true || closedAt > 0
  };
}

export function toAuditEntry(raw: Record<string, unknown>): AuditEntry {
  return {
    id: typeof raw['id'] === 'number' ? raw['id'] : 0,
    time: epochMs(raw['ts']),
    action: str(raw['action']),
    args: str(raw['args']),
    result: str(raw['result']),
    duration: typeof raw['durationMs'] === 'number' ? (raw['durationMs'] as number) : (typeof raw['duration'] === 'number' ? (raw['duration'] as number) : 0),
    session: str(raw['sessionId'] ?? raw['session_id']),
    harness: str(raw['harness']),
    workspaceId: str(raw['workspaceId'] ?? raw['workspace_id']),
    projectId: str(raw['projectId'] ?? raw['project_id'])
  };
}

export function toResource(raw: Record<string, unknown>): ResourceSample {
  return {
    processId: str(raw['processId'] ?? raw['process_id']),
    pid: typeof raw['pid'] === 'number' ? (raw['pid'] as number) : 0,
    memoryBytes: typeof raw['memoryBytes'] === 'number' ? (raw['memoryBytes'] as number) : 0,
    cpuNanos: typeof raw['cpuNanos'] === 'number' ? (raw['cpuNanos'] as number) : 0,
    cpuPercent: typeof raw['cpuPercent'] === 'number' ? (raw['cpuPercent'] as number) : 0,
    rssBytes: typeof raw['rssBytes'] === 'number' ? (raw['rssBytes'] as number) : 0,
    threads: typeof raw['threads'] === 'number' ? (raw['threads'] as number) : 0,
    openFds: typeof raw['openFds'] === 'number' ? (raw['openFds'] as number) : 0,
    children: typeof raw['children'] === 'number' ? (raw['children'] as number) : 0,
    ports: numArr(raw['ports']),
    at: typeof raw['at'] === 'number' ? (raw['at'] as number) : Date.now()
  };
}
