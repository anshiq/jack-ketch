import type { CheckSpec, EnvSourceSpec, Process } from '../api/types';
import { toCheck, toEnvSource } from '../api/assist';

export interface ConfigMode {
  name: string;
  description: string;
  command: string[];
  workdir: string;
  envSources: EnvSourceSpec[];
  checks: CheckSpec[];
  isDefault: boolean;
}

export interface ConfigApp {
  name: string;
  type: string;
  command: string[];
  workdir: string;
  dependsOn: string[];
  autostart: boolean;
  lifetime: string;
  reload: string;
  pty: boolean;
  readiness: string[];
  healthHttp: string;
  healthTcp: string;
  restartPolicy: string;
  ports: { name: string; port: number }[];
  envCount: number;
  provenance: string;
  auto: boolean;
  description: string;
  defaultMode: string;
  envSources: EnvSourceSpec[];
  checks: CheckSpec[];
  modes: ConfigMode[];
}

type Rec = Record<string, unknown>;

function pick(o: Rec, ...keys: string[]): unknown {
  for (const k of keys) if (o[k] !== undefined && o[k] !== null) return o[k];
  return undefined;
}

function strs(v: unknown): string[] {
  return Array.isArray(v) ? v.map((x) => String(x)) : [];
}

function rec(v: unknown): Rec {
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as Rec) : {};
}

function legacySources(o: Rec): EnvSourceSpec[] {
  const out: EnvSourceSpec[] = [];
  const file = String(pick(o, 'EnvFile', 'env_file') ?? '');
  if (file) out.push({ file });
  const env = strs(pick(o, 'Env', 'env'));
  if (env.length > 0) out.push({ vars: env });
  const extra = pick(o, 'EnvSources', 'env_sources');
  if (Array.isArray(extra)) for (const s of extra) out.push(toEnvSource(s));
  return out;
}

function checksOf(v: unknown): CheckSpec[] {
  return Array.isArray(v) ? v.map(toCheck).filter((c): c is CheckSpec => c !== null) : [];
}

export function mergeChecks(base: CheckSpec[], over: CheckSpec[]): CheckSpec[] {
  const out = [...base];
  for (const c of over) {
    const i = out.findIndex((x) => x.name === c.name);
    if (i >= 0) out[i] = c;
    else out.push(c);
  }
  return out;
}

export function toConfigApp(name: string, raw: unknown, provenance = '', auto = false): ConfigApp {
  const o = rec(raw);
  const baseCommand = strs(pick(o, 'Command', 'command'));
  const baseSources = legacySources(o);
  const baseChecks = checksOf(pick(o, 'Checks', 'checks'));
  const modesRaw = rec(pick(o, 'Modes', 'modes'));
  const declaredDefault = String(pick(o, 'DefaultMode', 'default_mode') ?? '');
  const modeNames = Object.keys(modesRaw).sort((a, b) => a.localeCompare(b));
  let effectiveDefault = declaredDefault && modeNames.includes(declaredDefault) ? declaredDefault : '';
  if (!effectiveDefault && baseCommand.length === 0 && modeNames.length === 1) effectiveDefault = modeNames[0] as string;
  const modes: ConfigMode[] = modeNames.map((m) => {
    const mo = rec(modesRaw[m]);
    const cmd = strs(pick(mo, 'command', 'Command'));
    return {
      name: m,
      description: String(pick(mo, 'description', 'Description') ?? ''),
      command: cmd.length > 0 ? cmd : baseCommand,
      workdir: String(pick(mo, 'workdir', 'WorkDir') ?? ''),
      envSources: [...baseSources, ...legacySources(mo)],
      checks: mergeChecks(baseChecks, checksOf(pick(mo, 'checks', 'Checks'))),
      isDefault: m === effectiveDefault
    };
  });
  const health = rec(pick(o, 'HealthCheck', 'health_check'));
  const restart = rec(pick(o, 'Restart', 'restart'));
  const portsRaw = rec(pick(o, 'Ports', 'ports'));
  const ports = Object.entries(portsRaw)
    .map(([n, p]) => ({ name: n, port: Number(pick(rec(p), 'Default', 'default') ?? 0) }))
    .filter((p) => p.port > 0);
  return {
    name,
    type: String(pick(o, 'Type', 'type') ?? ''),
    command: strs(pick(o, 'Command', 'command')),
    workdir: String(pick(o, 'WorkDir', 'workdir') ?? ''),
    dependsOn: strs(pick(o, 'DependsOn', 'depends_on')),
    autostart: pick(o, 'Autostart', 'autostart') === true,
    lifetime: String(pick(o, 'Lifetime', 'lifetime') ?? 'persistent'),
    reload: String(pick(o, 'Reload', 'reload') ?? 'manual'),
    pty: pick(o, 'Pty', 'pty') === true,
    readiness: strs(pick(o, 'Readiness', 'readiness')),
    healthHttp: String(pick(health, 'HTTP', 'http') ?? ''),
    healthTcp: String(pick(health, 'TCP', 'tcp') ?? ''),
    restartPolicy: String(pick(restart, 'Policy', 'policy') ?? ''),
    ports,
    envCount: strs(pick(o, 'Env', 'env')).length,
    provenance,
    auto,
    description: String(pick(o, 'Description', 'description') ?? ''),
    defaultMode: effectiveDefault,
    envSources: baseSources,
    checks: baseChecks,
    modes
  };
}

export function toConfigApps(apps: unknown, provenance: Record<string, unknown> = {}, autoApps: unknown = []): ConfigApp[] {
  const autoSet = new Set(Array.isArray(autoApps) ? autoApps.map(String) : []);
  return Object.entries(rec(apps))
    .map(([name, a]) => toConfigApp(name, a, String(provenance[`apps.${name}`] ?? ''), autoSet.has(name)))
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function quoteArg(arg: string): string {
  if (arg === '') return "''";
  if (/^[A-Za-z0-9_@%+=:,./-]+$/.test(arg)) return arg;
  return `'${arg.replace(/'/g, `'\\''`)}'`;
}

export function commandLine(command: string[]): string {
  return command.map(quoteArg).join(' ');
}

const LIVE = new Set(['running', 'ready', 'starting']);

export function isLive(status: string): boolean {
  return LIVE.has(status);
}

export function matchProcesses(app: ConfigApp, procs: Process[]): Process[] {
  const byName = procs.filter((p) => p.app === app.name);
  if (byName.length > 0) return byName;
  if (app.command.length === 0) return [];
  return procs.filter((p) => {
    if (p.app) return false;
    if (p.command !== app.command[0]) return false;
    const args = p.args ?? [];
    return args.length === app.command.length - 1 && args.every((a, i) => a === app.command[i + 1]);
  });
}

export function modeView(app: ConfigApp, mode: string): { command: string[]; envSources: EnvSourceSpec[]; checks: CheckSpec[]; description: string } {
  const m = app.modes.find((x) => x.name === mode);
  if (m) return { command: m.command, envSources: m.envSources, checks: m.checks, description: m.description };
  return { command: app.command, envSources: app.envSources, checks: app.checks, description: app.description };
}

export function sourceLabel(s: EnvSourceSpec): string {
  if (s.file) return s.optional ? `${s.file}?` : s.file;
  const keys = (s.vars ?? []).map((kv) => kv.split('=')[0]);
  if (keys.length === 0) return 'vars';
  return keys.length > 3 ? `${keys.slice(0, 3).join(', ')} +${keys.length - 3}` : keys.join(', ');
}

export function primaryProcess(matches: Process[]): Process | null {
  return matches.find((p) => isLive(p.status)) ?? matches[0] ?? null;
}
