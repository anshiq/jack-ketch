export type ProcessStatus = 'running' | 'starting' | 'ready' | 'exited' | 'stopped' | 'failed' | 'crashed' | 'unknown';

export interface Process {
  id: string;
  instanceId: string;
  app: string;
  workspaceId: string;
  projectId: string;
  command: string;
  args: string[];
  workdir: string;
  profile: string;
  status: ProcessStatus;
  pid: number;
  restarts: number;
  health: string;
  startedAt: number | null;
  exitedAt: number | null;
  exitCode: number | null;
  exitSignal: string;
  restartPolicy: string;
  stdoutLines: number;
  stderrLines: number;
  ports: number[];
  stale: boolean;
  loaded: boolean;
  lifetime: string;
  sessionId: string;
  mode: string;
  saveable: boolean;
}

export interface LogLine {
  id: number;
  ts: number;
  stream: string;
  level: string;
  text: string;
  proc: string;
  instanceId: string;
  cursor: string;
}

export interface LogFrame {
  kind: string;
  processId?: string;
  lines?: LogLine[];
  line?: LogLine;
  data?: string;
  cursor?: string;
}

export interface DaemonEvent {
  id: number;
  ts: number;
  type: string;
  processId: string;
  instanceId: string;
  workspaceId: string;
  projectId: string;
  sessionId: string;
  payload: Record<string, unknown>;
  cursor: string;
}

export interface Project {
  id: string;
  name: string;
  configMode: string;
  workspaceCount: number;
  lastUsed: number;
}

export interface Workspace {
  id: string;
  projectId: string;
  path: string;
  confirmed: boolean;
  lastSeen: number;
  missing: boolean;
}

export interface ConfigBundle {
  projectId: string;
  workspaceId: string;
  revision: number;
  raw: Record<string, string>;
  layers: { name: string; path: string; exists: boolean; writable: boolean }[];
  apps: Record<string, unknown>;
}

export interface PlanResult {
  changes: { app: string; kind: string; fields: string[]; affectedProcIds?: string[] }[];
  stale: boolean;
  latestRevision: number;
  diff: string;
}

export interface Revision {
  id: number;
  layer: string;
  time: number;
  source: string;
  session: string;
  message: string;
  valid: boolean;
  sha: string;
}

export type ScopeName = 'user' | 'project' | 'local';

export type ScopeStatus = 'installed' | 'outdated' | 'broken' | 'absent' | 'unavailable';

export interface EntryStatus {
  path: string;
  fileExists: boolean;
  installed: boolean;
  command: string;
  args: string[];
  commandFound: boolean;
  upToDate: boolean;
  problems: string[];
}

export interface ScopeReport {
  scope: ScopeName;
  status: ScopeStatus;
  portable: boolean;
  note: string;
  entries: EntryStatus[];
  error: string;
}

export interface HarnessInfo {
  id: string;
  displayName: string;
  tier: 1 | 2;
  docsUrl: string;
  detected: boolean;
  version: string;
  evidence: string[];
  scopes: ScopeReport[];
  skillsSupported: boolean;
  manualSteps: string;
  restartHint: string;
  maxWaitMs: number;
  connected: number;
}

export interface IntegrationChange {
  harness: string;
  scope: ScopeName;
  path: string;
  existed: boolean;
  changed: boolean;
  diff: string;
  backup: string;
  restartHint: string;
  note: string;
}

export interface IntegrationSnippet {
  harness: string;
  scope: string;
  format: string;
  path: string;
  text: string;
  cli: string;
  deeplink: string;
  steps: string;
}

export interface VerifyCheck {
  name: string;
  ok: boolean;
  detail: string;
  path: string;
}

export interface SkillState {
  name: string;
  description: string;
  want: string;
  have: string;
  dir: string;
  target: string;
  visible: boolean;
  own: boolean;
  shared: boolean;
  outdated: boolean;
}

export interface Session {
  id: string;
  kind: string;
  harness: string;
  harnessVersion: string;
  clientName: string;
  clientPid: number;
  workspaceId: string;
  started: number;
  lastSeen: number;
  closedAt: number;
  closed: boolean;
}

export interface AuditEntry {
  id: number;
  time: number;
  action: string;
  args: string;
  result: string;
  duration: number;
  session: string;
  harness: string;
  workspaceId: string;
  projectId: string;
}

export interface Settings {
  values: Record<string, unknown>;
  schema: Record<string, { type: string; enum?: string[]; description: string }>;
}

export interface ResourceSample {
  processId: string;
  pid: number;
  memoryBytes: number;
  cpuNanos: number;
  cpuPercent: number;
  rssBytes: number;
  threads: number;
  openFds: number;
  children: number;
  ports: number[];
  at: number;
}

export interface EnvSourceSpec {
  file?: string;
  optional?: boolean;
  vars?: string[];
}

export interface CheckSpec {
  name: string;
  method?: string;
  url?: string;
  tcp?: string;
  headers?: Record<string, string>;
  body?: string;
  expect_status?: number;
  expect_contains?: string;
  timeout_ms?: number;
}

export interface SaveSuggestion {
  processId: string;
  kind: string;
  reason: string;
  app: string;
  mode: string;
  newApp: boolean;
  modeExists: boolean;
  type: string;
  command: string[];
  workdir: string;
  envSources: EnvSourceSpec[];
  yaml: string;
  ask: string;
}

export interface SuggestedFix {
  kind: string;
  description: string;
  command: string;
  workdir: string;
  needsApproval: boolean;
}

export interface MissingEnvVar {
  key: string;
  hint: string;
  example: string;
  source: string;
  addTo: string;
}

export interface DiagnosisIssue {
  category: string;
  summary: string;
  actor: string;
  evidence: string[];
  steps: string[];
  fixes: SuggestedFix[];
}

export interface Diagnosis {
  processId: string;
  status: string;
  exitCode: number | null;
  failing: boolean;
  category: string;
  actor: string;
  summary: string;
  issues: DiagnosisIssue[];
  missingEnv: MissingEnvVar[];
  envFiles: string[];
  service: { name: string; host: string; port: number; image: string; runHint: string; projectApp: string } | null;
  tail: string[];
  guidance: string;
}

export interface ProbeLog {
  id: number;
  timestamp: string;
  stream: string;
  line: string;
}

export interface ProbeResult {
  name: string;
  processId: string;
  method: string;
  url: string;
  tcp: string;
  ok: boolean;
  status: number;
  statusText: string;
  durationMs: number;
  headers: Record<string, string>;
  body: string;
  bodyBytes: number;
  bodyTruncated: boolean;
  error: string;
  failures: string[];
  logs: ProbeLog[];
  logsTruncated: boolean;
  errorLogs: number;
  listeningPorts: number[];
  processStatus: string;
  check: CheckSpec | null;
  needsConfirmation: boolean;
  confirmReason: string;
}

export interface ChecksResult {
  processId: string;
  app: string;
  mode: string;
  passed: number;
  failed: number;
  results: ProbeResult[];
}

export interface GitignoreEntry {
  path: string;
  kind: string;
  reason: string;
  exists: boolean;
}

export interface GitignoreStatus {
  repo: boolean;
  repoRoot: string;
  gitignore: string;
  missing: GitignoreEntry[];
  tracked: string[];
}

export interface SaveRunResult {
  saved: boolean;
  dismissed: boolean;
  app: string;
  mode: string;
  scope: string;
  newApp: boolean;
  replaced: boolean;
  unchanged: boolean;
  revision: number;
  yaml: string;
  startWith: string;
  message: string;
  gitignore: GitignoreStatus | null;
}
