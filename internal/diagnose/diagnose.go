package diagnose

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"jack-ketch/pkg/api"
)

const (
	CatNone             = "none"
	CatPortInUse        = "port_in_use"
	CatMissingEnv       = "missing_env"
	CatInvalidCreds     = "invalid_credentials"
	CatServiceDown      = "service_unreachable"
	CatMigration        = "migration_pending"
	CatMissingDep       = "missing_dependency"
	CatMissingTool      = "missing_tool"
	CatToolchain        = "toolchain_version"
	CatPermission       = "permission"
	CatMissingFile      = "missing_file"
	CatOutOfMemory      = "out_of_memory"
	CatCodeError        = "code_error"
	CatStartFailed      = "start_failed"
	CatUnknown          = "unknown"
	ActorUser           = "user"
	ActorAgent          = "agent"
	ActorAgentApproval  = "agent_with_approval"
	maxEvidencePerIssue = 6
)

var priority = []string{
	CatStartFailed, CatPortInUse, CatMissingEnv, CatInvalidCreds, CatServiceDown, CatMigration,
	CatMissingDep, CatMissingTool, CatToolchain, CatPermission, CatMissingFile, CatOutOfMemory, CatCodeError,
}

type Line struct {
	Stream string
	Text   string
}

type Input struct {
	ProcessID   string
	Status      string
	ExitCode    *int
	Health      string
	Command     string
	Args        []string
	WorkDir     string
	ProjectDir  string
	Profile     string
	App         string
	Mode        string
	Lines       []Line
	Env         []string
	EnvFiles    []string
	StartError  string
	OOMKilled   bool
	PortOwner   func(port int) string
	ServiceApps map[string]string
}

type match struct {
	line   string
	groups []string
}

type analysis struct {
	in      Input
	files   *projectFiles
	hits    map[string][]match
	envKeys map[string]bool
	order   []string
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07`)

func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

type rule struct {
	cat string
	re  *regexp.Regexp
}

func r(cat, pat string) rule { return rule{cat: cat, re: regexp.MustCompile(pat)} }

var rules = []rule{
	r(CatPortInUse, `EADDRINUSE[^\n]*?:(\d{2,5})\b`),
	r(CatPortInUse, `(?i)address already in use[^\n]*?:(\d{2,5})\b`),
	r(CatPortInUse, `(?i)bind(?:ing)?[^\n]*?:(\d{2,5})[^\n]*address already in use`),
	r(CatPortInUse, `(?i)\bport (\d{2,5}) (?:is )?(?:already )?(?:in use|being used|taken)`),
	r(CatPortInUse, `(?i)port (\d{2,5}) was already in use`),
	r(CatPortInUse, `(?i)(address already in use|port is already allocated|EADDRINUSE)`),

	r(CatMissingEnv, `(?i)missing (?:required )?env(?:ironment)?[ _-]?var(?:iable)?s?[: ]+["'\x60]?([A-Z][A-Z0-9_]{1,})`),
	r(CatMissingEnv, `(?i)env(?:ironment)? var(?:iable)? ["'\x60]?([A-Z][A-Z0-9_]{1,})["'\x60]? (?:is )?(?:not set|not defined|missing|required|undefined|empty)`),
	r(CatMissingEnv, `\b([A-Z][A-Z0-9_]{2,}) (?:is|must be) (?:not set|required|missing|undefined|not defined|set)\b`),
	r(CatMissingEnv, `Environment variable not found: ([A-Z][A-Z0-9_]+)`),
	r(CatMissingEnv, `required key ([A-Z][A-Z0-9_]+) missing value`),
	r(CatMissingEnv, `required environment variable "([A-Z][A-Z0-9_]+)" is not set`),
	r(CatMissingEnv, `(?i)set the ([A-Z][A-Z0-9_]+) environment variable`),
	r(CatMissingEnv, `os\.environ\[['"]([A-Z][A-Z0-9_]+)['"]\]`),
	r(CatMissingEnv, `^KeyError: '([A-Z][A-Z0-9_]{2,})'`),
	r(CatMissingEnv, `Could not resolve placeholder '([A-Za-z0-9_.-]+)'`),
	r(CatMissingEnv, `["']?([A-Z][A-Z0-9_]{2,})["']?\s*:\s*\[\s*['"](?:Required|Invalid[^'"]*)['"]`),
	r(CatMissingEnv, `(?i)(Invalid environment variables|ImproperlyConfigured|ConfigError: missing)`),

	r(CatInvalidCreds, `(?i)password authentication failed for user "?([\w.-]+)`),
	r(CatInvalidCreds, `(?i)Access denied for user '([^']+)'`),
	r(CatInvalidCreds, `(?i)(WRONGPASS|NOAUTH Authentication required|SASL authentication failed|Authentication failed|invalid api[ _-]?key|invalid (?:access )?token|incorrect api key|invalid credentials|InvalidClientTokenId|SignatureDoesNotMatch|401 Unauthorized)`),

	r(CatServiceDown, `ECONNREFUSED\s+\[?([\w.:-]+?)\]?:(\d{2,5})\b`),
	r(CatServiceDown, `dial tcp \[?([\w.:-]+?)\]?:(\d{2,5}): connect: connection refused`),
	r(CatServiceDown, `(?i)connection to server at "([^"]+)"[^\n]*?port (\d{2,5}) failed`),
	r(CatServiceDown, `(?i)is the server running on host "([^"]+)"[^\n]*?port (\d{2,5})`),
	r(CatServiceDown, `(?i)Can't connect to (?:local )?MySQL server on '([^':]+)(?::(\d+))?'`),
	r(CatServiceDown, `(?i)Redis connection to ([\w.-]+):(\d{2,5}) failed`),
	r(CatServiceDown, `(?i)Error \d+ connecting to ([\w.-]+):(\d{2,5})`),
	r(CatServiceDown, `(?i)connect(?:ion)? refused[^\n]*?\b([\w.-]+):(\d{2,5})\b`),
	r(CatServiceDown, `getaddrinfo (?:ENOTFOUND|EAI_AGAIN) ([\w.-]+)`),
	r(CatServiceDown, `(?i)(could not connect to server|MongoServerSelectionError|ServerSelectionTimeoutError|Connection refused|ECONNREFUSED)`),

	r(CatMigration, `relation "([\w.]+)" does not exist`),
	r(CatMigration, `no such table: ([\w.]+)`),
	r(CatMigration, `Table '([^']+)' doesn't exist`),
	r(CatMigration, `The table [\x60"']?([\w.]+)[\x60"']? does not exist in the current database`),
	r(CatMigration, `(?i)(you have \d+ unapplied migration|pending migrations?|migrations? (?:are )?pending|ActiveRecord::PendingMigrationError|database schema is not empty|run .{0,20}migrate)`),

	r(CatMissingDep, `Cannot find module '([^']+)'`),
	r(CatMissingDep, `Cannot find package '([^']+)'`),
	r(CatMissingDep, `Module not found: (?:Error: )?Can't resolve '([^']+)'`),
	r(CatMissingDep, `Failed to resolve import "([^"]+)"`),
	r(CatMissingDep, `ModuleNotFoundError: No module named '([^']+)'`),
	r(CatMissingDep, `ImportError: No module named '?([\w.]+)`),
	r(CatMissingDep, `no required module provides package ([^\s;]+)`),
	r(CatMissingDep, `missing go\.sum entry for module providing package ([^\s;(]+)`),
	r(CatMissingDep, `cannot find package "([^"]+)"`),
	r(CatMissingDep, `(?i)Could not find gem '([^']+)'`),
	r(CatMissingDep, `(Bundler::GemNotFound|Could not resolve dependencies|Could not find artifact|Could not resolve all (?:files|dependencies)|vendor/autoload\.php|go: updates to go\.mod needed|Run .?npm install|Run .?yarn install|Run .?pnpm install)`),

	r(CatMissingTool, `exec: "([^"]+)": executable file not found`),
	r(CatMissingTool, `sh: \d+: ([\w.@/-]+): not found`),
	r(CatMissingTool, `(?:^|\s)([\w.@/-]+): command not found`),
	r(CatMissingTool, `spawn ([\w.@/-]+) ENOENT`),
	r(CatMissingTool, `'([\w.-]+)' is not recognized as an internal or external command`),
	r(CatMissingTool, `fork/exec ([^:]+): no such file or directory`),

	r(CatToolchain, `The engine "(\w+)" is incompatible with this module\. Expected version "([^"]+)"\. Got "([^"]+)"`),
	r(CatToolchain, `(?i)You are using Node\.js ([\d.]+)\.[^\n]*requires Node\.js(?: version)? ([^\s,.]+(?:\.[\dx]+)*)`),
	r(CatToolchain, `go\.mod requires go >= ([\d.]+) \(running go ([\d.]+)`),
	r(CatToolchain, `(?i)requires a different Python: ([\d.]+) not in '([^']+)'`),
	r(CatToolchain, `UnsupportedClassVersionError`),
	r(CatToolchain, `(?i)(unsupported engine|requires node(?:\.js)? (?:version )?[>=~^]+ ?[\d.]+|python [\d.]+ or (?:higher|newer) is required)`),

	r(CatPermission, `listen EACCES: permission denied [\w.:\[\]]*:(\d+)`),
	r(CatPermission, `EACCES: permission denied, \w+ '([^']+)'`),
	r(CatPermission, `PermissionError: \[Errno 13\] Permission denied: '([^']+)'`),
	r(CatPermission, `open ([^:]+): permission denied`),
	r(CatPermission, `(?i)(permission denied|operation not permitted|EACCES|EPERM)`),

	r(CatMissingFile, `ENOENT: no such file or directory, \w+ '([^']+)'`),
	r(CatMissingFile, `FileNotFoundError: \[Errno 2\] No such file or directory: '([^']+)'`),
	r(CatMissingFile, `open ([^:\s]+): no such file or directory`),

	r(CatOutOfMemory, `(JavaScript heap out of memory|Reached heap limit|java\.lang\.OutOfMemoryError|^MemoryError|fatal error: runtime: out of memory|Cannot allocate memory)`),

	r(CatCodeError, `APPLICATION FAILED TO START`),
	r(CatCodeError, `^([\w./-]+\.go):(\d+):(\d+): (.+)$`),
	r(CatCodeError, `([\w./@-]+\.(?:tsx?|jsx?|mjs|cjs|vue|svelte))[:(](\d+)[:,](\d+)\)?:? (?:- )?error(?: TS\d+)?:? (.+)`),
	r(CatCodeError, `error TS(\d+): (.+)`),
	r(CatCodeError, `File "([^"]+)", line (\d+)`),
	r(CatCodeError, `^(?:Uncaught )?(SyntaxError|TypeError|ReferenceError|RangeError|IndentationError|NameError|AttributeError|ValueError|ImportError|RuntimeError|AssertionError|ZeroDivisionError|NullPointerException|IllegalStateException|IllegalArgumentException)\b:?(.*)$`),
	r(CatCodeError, `^panic: (.+)$`),
	r(CatCodeError, `error\[E\d+\]: (.+)`),
	r(CatCodeError, `(Failed to compile|Build failed|Compilation failed|ERROR in |✘ \[ERROR\]|Traceback \(most recent call last\)|Exception in thread "main")`),
}

var pydanticHeaderRe = regexp.MustCompile(`validation errors? for (\w*(?:Settings|Config|Env)\w*)`)
var pydanticFieldRe = regexp.MustCompile(`^([a-z_][a-z0-9_]*)$`)
var pydanticMissingRe = regexp.MustCompile(`^\s*Field required`)

var errorish = regexp.MustCompile(`(?i)(error|exception|fatal|failed|panic|traceback|refused|denied|not found|cannot|can't|unable|invalid|missing|EADDRINUSE|ECONNREFUSED|ENOENT|EACCES|killed)`)

func Analyze(in Input) *api.Diagnosis {
	a := &analysis{in: in, hits: map[string][]match{}, envKeys: envKeySet(in.Env)}
	a.files = scanProject(in.WorkDir, in.ProjectDir)
	lines := make([]Line, 0, len(in.Lines))
	for _, l := range in.Lines {
		l.Text = strings.TrimRight(StripANSI(l.Text), "\r")
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		lines = append(lines, l)
	}
	if in.StartError != "" {
		a.add(CatStartFailed, match{line: in.StartError})
		for _, rl := range rules {
			if rl.cat != CatMissingTool && rl.cat != CatMissingFile && rl.cat != CatPermission {
				continue
			}
			if m := rl.re.FindStringSubmatch(in.StartError); m != nil {
				a.add(rl.cat, match{line: in.StartError, groups: m[1:]})
			}
		}
	}
	for _, l := range lines {
		for _, rl := range rules {
			m := rl.re.FindStringSubmatch(strings.TrimSpace(l.Text))
			if m == nil {
				m = rl.re.FindStringSubmatch(l.Text)
			}
			if m == nil {
				continue
			}
			a.add(rl.cat, match{line: strings.TrimSpace(l.Text), groups: m[1:]})
		}
	}
	a.scanPydantic(lines)
	if in.OOMKilled || (in.ExitCode != nil && *in.ExitCode == 137 && len(a.hits[CatOutOfMemory]) == 0 && strings.Contains(strings.ToLower(in.Status), "crash")) {
		a.add(CatOutOfMemory, match{line: "process was killed (exit 137 / OOM)"})
	}
	return a.build(lines)
}

func (a *analysis) add(cat string, m match) {
	if _, ok := a.hits[cat]; !ok {
		a.order = append(a.order, cat)
	}
	for _, prev := range a.hits[cat] {
		if prev.line == m.line {
			return
		}
	}
	a.hits[cat] = append(a.hits[cat], m)
}

func (a *analysis) scanPydantic(lines []Line) {
	inBlock := false
	for i, l := range lines {
		t := strings.TrimSpace(l.Text)
		if pydanticHeaderRe.MatchString(t) {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		if pydanticFieldRe.MatchString(t) && i+1 < len(lines) && pydanticMissingRe.MatchString(lines[i+1].Text) {
			a.add(CatMissingEnv, match{line: t + ": Field required", groups: []string{strings.ToUpper(t)}})
		}
	}
}

func envKeySet(env []string) map[string]bool {
	out := make(map[string]bool, len(env))
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.TrimSpace(v) != "" {
			out[k] = true
		}
	}
	return out
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(env[i], "=")
		if ok && k == key {
			return v
		}
	}
	return ""
}

func (a *analysis) failing() bool {
	st := strings.ToLower(a.in.Status)
	switch st {
	case "failed", "crashed":
		return true
	case "exited", "stopped":
		return a.in.ExitCode != nil && *a.in.ExitCode != 0
	}
	return a.in.Health == "unhealthy" || a.in.StartError != ""
}

func (a *analysis) build(lines []Line) *api.Diagnosis {
	d := &api.Diagnosis{ProcessID: a.in.ProcessID, Status: a.in.Status, ExitCode: a.in.ExitCode, Failing: a.failing()}
	d.Tail = tail(lines, 15)
	d.EnvFiles = append([]string(nil), a.in.EnvFiles...)
	var issues []api.Issue
	for _, cat := range priority {
		hits := a.hits[cat]
		if len(hits) == 0 {
			continue
		}
		if iss, ok := a.issue(cat, hits, d); ok {
			issues = append(issues, iss)
		}
	}
	if len(issues) == 0 {
		missing := a.exampleGaps(nil)
		if len(missing) > 0 && d.Failing {
			d.MissingEnv = missing
			issues = append(issues, a.envIssue(nil, missing, true))
		}
	}
	if len(issues) == 0 {
		if !d.Failing {
			d.Category = CatNone
			d.Actor = ActorAgent
			d.Summary = "No failure detected in the recent output."
			d.Guidance = "Nothing to fix. If the user reported a problem, reproduce it (probe the endpoint) and read get_logs around the request."
			return d
		}
		ev := errorLines(lines, maxEvidencePerIssue)
		if len(ev) == 0 {
			ev = tail(lines, 5)
		}
		summary := "The process failed without a recognised error pattern."
		if a.in.ExitCode != nil {
			summary = fmt.Sprintf("The process exited with code %d without a recognised error pattern.", *a.in.ExitCode)
		}
		issues = append(issues, api.Issue{
			Category: CatUnknown, Actor: ActorAgent, Summary: summary, Evidence: ev,
			Steps: []string{"Read the evidence and tail lines, find the first real error (not the last stack frame).", "Fix the cause, then restart_process and wait_for_log(ready=true)."},
		})
	}
	d.Issues = issues
	d.Category = issues[0].Category
	d.Actor = issues[0].Actor
	d.Summary = issues[0].Summary
	d.Guidance = guidance(issues)
	return d
}

func guidance(issues []api.Issue) string {
	actor := issues[0].Actor
	var b strings.Builder
	switch actor {
	case ActorUser:
		b.WriteString("Only the user can fix this (values or access you cannot know). Do not invent values or edit secrets yourself. Show the user the summary, the exact keys/files from steps and missing_env, and ask them to make the change. When they confirm, restart_process and wait_for_log(ready=true).")
	case ActorAgentApproval:
		b.WriteString("You can fix this, but it installs or starts something (dependencies, containers, migrations). Ask the user for permission first, showing the exact commands from fixes. Run approved commands with your own shell (not start_process), then restart_process and wait_for_log(ready=true).")
	default:
		b.WriteString("You can fix this yourself: change the code or config named in the evidence, then restart_process and wait_for_log(ready=true).")
	}
	if len(issues) > 1 {
		b.WriteString(" Fix the first issue first; later issues are often consequences of it.")
	}
	b.WriteString(" Once the process runs, the result carries save_suggestion: offer to save the working setup with save_run_config.")
	return b.String()
}

func (a *analysis) issue(cat string, hits []match, d *api.Diagnosis) (api.Issue, bool) {
	switch cat {
	case CatStartFailed:
		return api.Issue{Category: cat, Actor: ActorAgent, Summary: "The process could not be started: " + hits[0].line, Evidence: evidence(hits),
			Steps: []string{"Check the command, workdir and env file paths in the start request or app config.", "Fix them and start again."}}, true
	case CatPortInUse:
		return a.portIssue(hits), true
	case CatMissingEnv:
		keys := a.envKeysFrom(hits)
		missing := a.exampleGaps(keys)
		d.MissingEnv = missing
		return a.envIssue(hits, missing, false), true
	case CatInvalidCreds:
		return a.credsIssue(hits), true
	case CatServiceDown:
		iss, svc := a.serviceIssue(hits)
		if svc != nil {
			d.Service = svc
		}
		return iss, true
	case CatMigration:
		return a.migrationIssue(hits), true
	case CatMissingDep:
		return a.depIssue(hits)
	case CatMissingTool:
		return a.toolIssue(hits), true
	case CatToolchain:
		return a.toolchainIssue(hits), true
	case CatPermission:
		return a.permissionIssue(hits), true
	case CatMissingFile:
		return a.fileIssue(hits), true
	case CatOutOfMemory:
		return api.Issue{Category: cat, Actor: ActorAgent, Summary: "The process ran out of memory.", Evidence: evidence(hits),
			Steps: []string{"Look for runaway loops or very large inputs first.", "For Node, raise the heap: start with env NODE_OPTIONS=--max-old-space-size=4096.", "If the app declares limits.memory, raise it in the project config."},
			Fixes: []api.SuggestedFix{{Kind: "env", Description: "Raise the Node heap for this run", Command: "NODE_OPTIONS=--max-old-space-size=4096", NeedsApproval: false}}}, true
	case CatCodeError:
		return a.codeIssue(hits), true
	}
	return api.Issue{}, false
}

func evidence(hits []match) []string {
	var out []string
	seen := map[string]bool{}
	for _, h := range hits {
		if seen[h.line] {
			continue
		}
		seen[h.line] = true
		out = append(out, truncate(h.line, 400))
		if len(out) >= maxEvidencePerIssue {
			break
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func tail(lines []Line, n int) []string {
	start := len(lines) - n
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, len(lines)-start)
	for _, l := range lines[start:] {
		out = append(out, truncate(l.Text, 400))
	}
	return out
}

func errorLines(lines []Line, n int) []string {
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if errorish.MatchString(lines[i].Text) {
			out = append([]string{truncate(strings.TrimSpace(lines[i].Text), 400)}, out...)
		}
	}
	return out
}

func firstGroup(h match) string {
	for _, g := range h.groups {
		if g != "" {
			return g
		}
	}
	return ""
}

func (a *analysis) portIssue(hits []match) api.Issue {
	port := 0
	for _, h := range hits {
		if p, err := strconv.Atoi(firstGroup(h)); err == nil && p > 0 {
			port = p
			break
		}
	}
	if port == 0 {
		if p, err := strconv.Atoi(envValue(a.in.Env, "PORT")); err == nil {
			port = p
		}
	}
	iss := api.Issue{Category: CatPortInUse, Actor: ActorAgent, Evidence: evidence(hits)}
	owner := ""
	if port > 0 && a.in.PortOwner != nil {
		owner = a.in.PortOwner(port)
	}
	switch {
	case port > 0 && owner != "":
		iss.Summary = fmt.Sprintf("Port %d is already used by %s.", port, owner)
		iss.Steps = []string{
			fmt.Sprintf("Another jack-ketch process (%s) already listens on %d: if it is the same app, reuse it instead of starting a duplicate.", owner, port),
			"Otherwise stop the old one (stop_process) or start this one on a different port (env PORT=<free port>).",
		}
		iss.Fixes = []api.SuggestedFix{{Kind: "reuse", Description: "Reuse the running process instead of starting a new one (list_processes)", NeedsApproval: false}}
	case port > 0:
		iss.Summary = fmt.Sprintf("Port %d is already in use by a process outside jack-ketch.", port)
		iss.Steps = []string{
			fmt.Sprintf("Find the owner: lsof -iTCP:%d -sTCP:LISTEN (or ss -ltnp | grep :%d).", port, port),
			"If it is a stale copy of this app, ask the user before killing it; otherwise run this app on another port (env PORT=<free port>).",
		}
		iss.Fixes = []api.SuggestedFix{
			{Kind: "command", Description: "Show which process holds the port", Command: fmt.Sprintf("lsof -iTCP:%d -sTCP:LISTEN", port), NeedsApproval: false},
			{Kind: "env", Description: "Start on a free port instead", Command: "PORT=<free port>", NeedsApproval: false},
		}
	default:
		iss.Summary = "The port this app wants is already in use."
		iss.Steps = []string{"Check list_processes for a running copy of this app and reuse it.", "Or start on a different port (env PORT=<free port>)."}
	}
	return iss
}

var upperKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,}$`)

func (a *analysis) envKeysFrom(hits []match) []string {
	seen := map[string]bool{}
	var keys []string
	for _, h := range hits {
		for _, g := range h.groups {
			k := strings.TrimSpace(g)
			if k == "" {
				continue
			}
			if strings.Contains(k, ".") && !upperKeyRe.MatchString(k) {
				k = strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(k))
			}
			if !upperKeyRe.MatchString(k) || seen[k] || ignoredEnvWord[k] {
				continue
			}
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

var ignoredEnvWord = map[string]bool{"ERROR": true, "WARN": true, "INFO": true, "DEBUG": true, "FATAL": true, "TODO": true, "NOTE": true, "HTTP": true, "HTTPS": true, "URL": true, "API": true, "JSON": true, "THE": true, "NOT": true}

func (a *analysis) addTarget() string {
	for i := len(a.in.EnvFiles) - 1; i >= 0; i-- {
		f := a.in.EnvFiles[i]
		if f != "" && !strings.HasPrefix(f, "(") {
			return f
		}
	}
	return a.files.rel(".env")
}

func (a *analysis) exampleGaps(focus []string) []api.MissingEnvVar {
	examples := a.files.envExamples()
	target := a.addTarget()
	var out []api.MissingEnvVar
	seen := map[string]bool{}
	for _, k := range focus {
		seen[k] = true
		mv := api.MissingEnvVar{Key: k, AddTo: target}
		for _, ex := range examples {
			if e, ok := ex.entries[k]; ok {
				mv.Hint, mv.Example, mv.Source = e.hint, e.value, ex.name
				break
			}
		}
		if a.envKeys[k] {
			mv.Hint = strings.TrimSpace(mv.Hint + " (currently set, but the app rejected its value)")
		}
		out = append(out, mv)
	}
	for _, ex := range examples {
		for _, k := range ex.order {
			if seen[k] || a.envKeys[k] {
				continue
			}
			seen[k] = true
			e := ex.entries[k]
			out = append(out, api.MissingEnvVar{Key: k, Hint: e.hint, Example: e.value, Source: ex.name, AddTo: target})
		}
	}
	return out
}

func (a *analysis) envIssue(hits []match, missing []api.MissingEnvVar, fromExampleOnly bool) api.Issue {
	iss := api.Issue{Category: CatMissingEnv, Actor: ActorUser, Evidence: evidence(hits)}
	var keys []string
	for _, m := range missing {
		keys = append(keys, m.Key)
	}
	switch {
	case fromExampleOnly:
		iss.Summary = fmt.Sprintf("No clear error, but %d variable(s) from the env example are not set: %s.", len(keys), strings.Join(limitList(keys, 8), ", "))
	case len(keys) > 0:
		iss.Summary = fmt.Sprintf("Required environment variable(s) are missing or invalid: %s.", strings.Join(limitList(keys, 8), ", "))
	default:
		iss.Summary = "The app rejected its environment configuration."
	}
	target := a.addTarget()
	steps := []string{}
	if len(keys) > 0 {
		steps = append(steps, fmt.Sprintf("Open %s and add: %s", target, strings.Join(limitList(keys, 12), ", ")))
	} else {
		steps = append(steps, "Read the evidence lines to see which setting was rejected, then fix it in "+target+".")
	}
	for _, m := range missing {
		if len(steps) >= 8 {
			break
		}
		line := "  " + m.Key + "="
		if m.Example != "" {
			line += m.Example + "   (example value from " + m.Source + ")"
		}
		if m.Hint != "" {
			line += "   # " + m.Hint
		}
		steps = append(steps, line)
	}
	if len(a.in.EnvFiles) == 0 {
		steps = append(steps, "This run loads no env file through jack-ketch. Either the app reads .env itself (dotenv), or restart with env_file / save the run with env_sources so the file is loaded.")
	} else {
		steps = append(steps, "Env files loaded for this run (later wins): "+strings.Join(a.in.EnvFiles, ", ")+".")
	}
	steps = append(steps, "get_process_env(process_id) shows which value each key currently has and where it came from.")
	iss.Steps = steps
	iss.Fixes = []api.SuggestedFix{{Kind: "user_action", Description: "You add the missing values to " + target, NeedsApproval: false}}
	return iss
}

func limitList(v []string, n int) []string {
	if len(v) <= n {
		return v
	}
	return append(append([]string(nil), v[:n]...), fmt.Sprintf("+%d more", len(v)-n))
}

func (a *analysis) credsIssue(hits []match) api.Issue {
	var keys []string
	for _, kv := range a.in.Env {
		k, _, _ := strings.Cut(kv, "=")
		u := strings.ToUpper(k)
		if strings.Contains(u, "PASSWORD") || strings.Contains(u, "TOKEN") || strings.Contains(u, "API_KEY") || strings.Contains(u, "SECRET") || strings.HasSuffix(u, "_URL") || strings.HasSuffix(u, "_URI") || strings.HasSuffix(u, "_DSN") {
			if !strings.HasPrefix(u, "npm_") && !strings.HasPrefix(u, "NPM_") {
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	steps := []string{"The service rejected the credentials this app sent. Ask the user to check the values (they may be expired, for another environment, or mistyped)."}
	if len(keys) > 0 {
		steps = append(steps, "Likely keys: "+strings.Join(limitList(keys, 10), ", ")+" — get_process_env shows which file each came from.")
	}
	if len(a.in.EnvFiles) > 0 {
		steps = append(steps, "Env files loaded for this run (later wins): "+strings.Join(a.in.EnvFiles, ", ")+".")
	}
	return api.Issue{Category: CatInvalidCreds, Actor: ActorUser, Summary: "Authentication failed: the configured credentials were rejected.", Evidence: evidence(hits), Steps: steps,
		Fixes: []api.SuggestedFix{{Kind: "user_action", Description: "You correct the credential values", NeedsApproval: false}}}
}

func (a *analysis) codeIssue(hits []match) api.Issue {
	ev := evidence(hits)
	summary := "The code failed at startup: " + truncate(hits[0].line, 200)
	for _, h := range hits {
		if strings.Contains(h.line, "APPLICATION FAILED TO START") {
			summary = "Spring Boot refused to start; the Description/Action block in the logs explains why."
			break
		}
	}
	for i := len(hits) - 1; i >= 0; i-- {
		l := hits[i].line
		if strings.Contains(l, "Error") || strings.Contains(l, "error") || strings.HasPrefix(l, "panic:") {
			if !strings.HasPrefix(l, "Traceback") && !strings.HasPrefix(l, "File \"") {
				summary = "The code failed: " + truncate(l, 240)
				break
			}
		}
	}
	return api.Issue{Category: CatCodeError, Actor: ActorAgent, Summary: summary, Evidence: ev,
		Steps: []string{"Open the file and line named in the evidence and fix the error.", "restart_process, then wait_for_log(ready=true) or wait_for_exit for one-shot commands."}}
}
