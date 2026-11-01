package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"jack-ketch/internal/config"
	"jack-ketch/internal/integrate"
	"jack-ketch/pkg/api"
)

type RunOrigin struct {
	App       string   `json:"app,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Command   []string `json:"command,omitempty"`
	WorkDir   string   `json:"workdir,omitempty"`
	Env       []string `json:"env,omitempty"`
	EnvFile   string   `json:"envFile,omitempty"`
	Shell     bool     `json:"shell,omitempty"`
	SavedApp  string   `json:"savedApp,omitempty"`
	SavedMode string   `json:"savedMode,omitempty"`
}

func ParseOrigin(specJSON string) RunOrigin {
	var o RunOrigin
	if specJSON != "" {
		_ = json.Unmarshal([]byte(specJSON), &o)
	}
	return o
}

func (o RunOrigin) JSON() string {
	b, _ := json.Marshal(o)
	return string(b)
}

func (o RunOrigin) hasOverrides() bool { return len(o.Env) > 0 || o.EnvFile != "" }

var saveDenylist = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true, "csh": true, "tcsh": true,
	"pwsh": true, "powershell": true, "cmd": true, "env": true, "sudo": true, "doas": true, "nohup": true,
	"timeout": true, "docker": true, "podman": true, "kubectl": true, "helm": true, "git": true, "ls": true,
	"cat": true, "grep": true, "rg": true, "find": true, "echo": true, "curl": true, "wget": true, "sleep": true,
	"tail": true, "head": true, "true": true, "false": true, "ssh": true, "scp": true, "rsync": true, "tar": true,
}

func commandBase(cmd string) string {
	base := filepath.Base(cmd)
	return strings.TrimSuffix(strings.TrimSuffix(base, ".exe"), ".cmd")
}

func (e *Engine) SuggestSaving() bool { return e.settingBool("suggest_saving_runs", true) }

func dismissKey(projectID string) string { return "save_dismissed:" + projectID }

func (o RunOrigin) signature(wsPath string) string {
	env := append([]string(nil), o.Env...)
	sort.Strings(env)
	b, _ := json.Marshal([]any{o.App, o.Mode, o.Command, normalizeWorkdir(wsPath, o.WorkDir), env, o.EnvFile})
	return config.SHA256(b)[:24]
}

func (e *Engine) stringList(key string) []string {
	raw, ok, err := e.store.GetSetting(key)
	if err != nil || !ok {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func (e *Engine) putStringList(key string, list []string) error {
	if len(list) > 500 {
		list = list[len(list)-500:]
	}
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return e.store.SetSetting(key, string(b))
}

func (e *Engine) addToList(key, v string) error {
	list := e.stringList(key)
	for _, x := range list {
		if x == v {
			return nil
		}
	}
	return e.putStringList(key, append(list, v))
}

type saveDraft struct {
	app        string
	mode       string
	newApp     bool
	modeExists bool
	appType    string
	workdir    string
	modeSpec   config.Mode
	appendOnly bool
}

var slugRe = regexp.MustCompile(`[^a-z0-9_.-]+`)

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-.")
	}
	return s
}

func envVal(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(env[i], "=")
		if ok && k == key {
			return v
		}
	}
	return ""
}

func stageFromEnv(env []string) string {
	for _, k := range []string{"APP_ENV", "NODE_ENV", "RAILS_ENV", "RACK_ENV", "ENVIRONMENT", "ENV", "FLASK_ENV", "DJANGO_ENV", "SPRING_PROFILES_ACTIVE", "ASPNETCORE_ENVIRONMENT", "GO_ENV", "STAGE"} {
		switch strings.ToLower(envVal(env, k)) {
		case "production", "prod":
			return "prod"
		case "staging", "stage":
			return "staging"
		case "test", "testing":
			return "test"
		case "qa":
			return "qa"
		case "local":
			return "local"
		}
	}
	return ""
}

func hasArg(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}

func DeriveMode(command []string, env []string, task bool) string {
	if len(command) == 0 {
		return "default"
	}
	base := commandBase(command[0])
	args := command[1:]
	first := func() string {
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				return a
			}
		}
		return ""
	}
	mode := ""
	switch base {
	case "npm", "pnpm", "yarn", "bun":
		a := first()
		switch {
		case a == "run" || a == "run-script":
			for i, x := range args {
				if (x == "run" || x == "run-script") && i+1 < len(args) {
					mode = args[i+1]
					break
				}
			}
		case a == "start", a == "test", a == "dev", a == "build", a == "serve":
			mode = a
		case a == "install", a == "i", a == "ci", a == "add":
			mode = "install"
		case a != "" && (base == "yarn" || base == "pnpm" || base == "bun"):
			mode = a
		}
	case "npx", "bunx", "pnpx", "dlx":
		tool := first()
		switch tool {
		case "vite", "nodemon", "tsx", "ts-node-dev":
			mode = "dev"
			if hasArg(args, "build") {
				mode = "build"
			} else if hasArg(args, "preview") {
				mode = "preview"
			}
		case "next", "nuxt", "astro", "remix":
			mode = "dev"
			for _, x := range args[1:] {
				if x == "start" || x == "build" || x == "dev" || x == "preview" {
					mode = x
					break
				}
			}
		case "jest", "vitest", "mocha", "ava":
			mode = "test"
		case "playwright", "cypress":
			mode = "e2e"
		case "tsc":
			mode = "typecheck"
		case "eslint":
			mode = "lint"
		default:
			mode = tool
		}
	case "node", "deno", "tsx", "ts-node", "nodemon":
		mode = "dev"
		if base == "deno" && first() == "test" {
			mode = "test"
		}
	case "go":
		switch first() {
		case "run":
			mode = "dev"
		case "test":
			mode = "test"
		case "build":
			mode = "build"
		case "generate":
			mode = "generate"
		default:
			mode = first()
		}
	case "cargo":
		switch first() {
		case "run":
			mode = "dev"
			if hasArg(args, "--release", "-r") {
				mode = "release"
			}
		case "test", "build", "bench", "check":
			mode = first()
		case "watch":
			mode = "watch"
		}
	case "python", "python3", "uv", "poetry", "pipenv", "pdm", "hatch", "rye":
		rest := args
		for len(rest) > 0 && (rest[0] == "run" || rest[0] == "-m" || rest[0] == "--" || rest[0] == "-u" || strings.HasPrefix(filepath.Base(rest[0]), "python")) {
			rest = rest[1:]
		}
		if len(rest) > 0 {
			tool := filepath.Base(rest[0])
			sub := ""
			if len(rest) > 1 {
				sub = rest[1]
			}
			switch {
			case tool == "manage.py":
				switch sub {
				case "runserver", "":
					mode = "dev"
				default:
					mode = sub
				}
			case tool == "pytest" || tool == "unittest" || tool == "tox" || tool == "nox":
				mode = "test"
			case tool == "uvicorn" || tool == "hypercorn" || tool == "granian":
				mode = "serve"
				if hasArg(rest, "--reload") {
					mode = "dev"
				}
			case tool == "gunicorn":
				mode = "prod"
			case tool == "fastapi" || tool == "flask":
				mode = "dev"
				if sub == "run" && tool == "fastapi" {
					mode = "serve"
				}
			case tool == "celery":
				mode = "worker"
				if hasArg(rest, "beat") {
					mode = "beat"
				}
			case strings.HasSuffix(tool, ".py"):
				mode = strings.TrimSuffix(tool, ".py")
			default:
				mode = tool
			}
		}
	case "pytest", "jest", "vitest", "mocha", "rspec", "phpunit":
		mode = "test"
	case "uvicorn", "hypercorn":
		mode = "serve"
		if hasArg(args, "--reload") {
			mode = "dev"
		}
	case "gunicorn":
		mode = "prod"
	case "celery":
		mode = "worker"
	case "make", "just", "task", "mage":
		mode = first()
		if mode == "" {
			mode = "default"
		}
	case "mvn", "mvnw", "gradle", "gradlew":
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "spring-boot:run"), strings.Contains(joined, "bootRun"), strings.Contains(joined, "quarkus:dev"):
			mode = "dev"
		case strings.Contains(joined, "test"):
			mode = "test"
		case strings.Contains(joined, "package"), strings.Contains(joined, "build"), strings.Contains(joined, "install"):
			mode = "build"
		}
	case "rails":
		switch first() {
		case "server", "s":
			mode = "dev"
		default:
			mode = first()
		}
	case "dotnet":
		switch first() {
		case "run", "watch":
			mode = "dev"
		default:
			mode = first()
		}
	case "flutter":
		mode = first()
		if mode == "run" {
			mode = "dev"
		}
	case "air", "reflex", "watchexec", "entr":
		mode = "watch"
	}
	if hasArg(args, "--headless") || strings.EqualFold(envVal(env, "HEADLESS"), "true") || envVal(env, "HEADLESS") == "1" {
		if mode == "" || mode == "dev" || mode == "start" || mode == "default" {
			mode = "headless"
		} else {
			mode += "-headless"
		}
	}
	if hasArg(args, "--prod", "--production") && (mode == "" || mode == "dev" || mode == "start") {
		mode = "prod"
	}
	if stage := stageFromEnv(env); stage != "" && (mode == "" || mode == "dev" || mode == "start" || mode == "serve" || mode == "default") {
		mode = stage
	}
	if task && (mode == "dev" || mode == "") {
		mode = "run"
	}
	mode = slug(mode)
	if mode == "" || !config.ValidModeName(mode) {
		mode = "default"
	}
	return mode
}

func modeSpecFromOrigin(o RunOrigin) config.Mode {
	m := config.Mode{Command: append([]string(nil), o.Command...)}
	if o.EnvFile != "" {
		m.EnvSources = append(m.EnvSources, config.EnvSource{File: o.EnvFile})
	}
	if len(o.Env) > 0 {
		m.EnvSources = append(m.EnvSources, config.EnvSource{Vars: config.EnvVars(append([]string(nil), o.Env...))})
	}
	return m
}

func sameStrings(a, b []string) bool {
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

func appWorkdir(app config.V3App) string {
	wd := app.WorkDir
	if wd == "" {
		wd = "."
	}
	return strings.TrimPrefix(strings.TrimSuffix(wd, "/"), "./")
}

func (e *Engine) existingMatch(pr *ProjectRuntime, o RunOrigin) (string, string) {
	res := pr.ResolvedConfig()
	if res == nil || len(o.Command) == 0 {
		return "", ""
	}
	wd := normalizeWorkdir(pr.wsPath, o.WorkDir)
	names := make([]string, 0, len(res.Apps))
	for n := range res.Apps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		app := res.Apps[name]
		candidates := []string{""}
		candidates = append(candidates, app.ModeNames()...)
		for _, m := range candidates {
			r, err := app.ResolveMode(m)
			if err != nil {
				continue
			}
			mwd := r.WorkDir
			if mwd == "" {
				mwd = "."
			}
			mwd = strings.TrimPrefix(strings.TrimSuffix(mwd, "/"), "./")
			if !sameStrings(r.Command, o.Command) || mwd != wd {
				continue
			}
			if !o.hasOverrides() {
				return name, r.Mode
			}
			have := map[string]string{}
			files := map[string]bool{}
			for _, src := range r.EnvChain {
				if src.File != "" {
					files[src.File] = true
				}
				for _, kv := range src.Vars {
					k, v, _ := strings.Cut(kv, "=")
					have[k] = v
				}
			}
			ok := o.EnvFile == "" || files[o.EnvFile]
			for _, kv := range o.Env {
				k, v, _ := strings.Cut(kv, "=")
				if have[k] != v {
					ok = false
				}
			}
			if ok {
				return name, r.Mode
			}
		}
	}
	return "", ""
}

func (e *Engine) draftSave(pr *ProjectRuntime, o RunOrigin, profile string, task bool, wantApp, wantMode string) (saveDraft, error) {
	res := pr.ResolvedConfig()
	apps := map[string]config.V3App{}
	if res != nil {
		apps = res.Apps
	}
	d := saveDraft{}
	if profile != "" && profile != "generic" {
		d.appType = profile
	}
	if o.App != "" {
		base, ok := apps[o.App]
		if !ok {
			return d, fmt.Errorf("app %q is no longer in the config", o.App)
		}
		d.app = o.App
		if wantApp != "" && wantApp != o.App {
			d.app = wantApp
		}
		_, d.newApp = apps[d.app]
		d.newApp = !d.newApp
		var src config.Mode
		if o.Mode != "" {
			src = base.Modes[o.Mode]
		}
		resolved, err := base.ResolveMode(o.Mode)
		if err != nil {
			return d, err
		}
		d.mode = wantMode
		if d.mode == "" && d.app == o.App && !o.hasOverrides() {
			d.mode = o.Mode
		} else if d.mode == "" {
			stage := stageFromEnv(o.Env)
			switch {
			case stage != "":
				d.mode = stage
			case o.Mode != "":
				d.mode = o.Mode + "-custom"
			default:
				d.mode = "custom"
			}
		}
		if !d.newApp && d.mode == o.Mode {
			d.appendOnly = true
			d.modeSpec = modeSpecFromOrigin(RunOrigin{Env: o.Env, EnvFile: o.EnvFile})
			d.modeSpec.Command = nil
			d.modeExists = true
			return d, nil
		}
		m := src
		m.Description = ""
		if len(m.Command) == 0 {
			m.Command = append([]string(nil), resolved.Command...)
		}
		if d.newApp {
			m.WorkDir = resolved.WorkDir
			m.EnvSources = nil
			for _, s := range resolved.EnvChain {
				m.EnvSources = append(m.EnvSources, s.EnvSource)
			}
			m.EnvFile, m.Env = "", nil
		}
		extra := modeSpecFromOrigin(RunOrigin{Env: o.Env, EnvFile: o.EnvFile})
		m.EnvSources = append(append([]config.EnvSource(nil), m.EnvSources...), extra.EnvSources...)
		d.modeSpec = m
		if !d.newApp {
			_, d.modeExists = base.Modes[d.mode]
		}
		return d, nil
	}
	wd := normalizeWorkdir(pr.wsPath, o.WorkDir)
	d.workdir = wd
	d.modeSpec = modeSpecFromOrigin(o)
	if wantApp != "" {
		d.app = wantApp
		existing, ok := apps[wantApp]
		d.newApp = !ok
		if ok && appWorkdir(existing) != wd {
			if wd == "." {
				d.modeSpec.WorkDir = "."
			} else {
				d.modeSpec.WorkDir = wd
			}
		}
	} else {
		names := make([]string, 0, len(apps))
		for n := range apps {
			names = append(names, n)
		}
		sort.Strings(names)
		var sameDir []string
		for _, n := range names {
			if appWorkdir(apps[n]) == wd {
				sameDir = append(sameDir, n)
			}
		}
		pick := ""
		for _, n := range sameDir {
			if d.appType != "" && apps[n].Type == d.appType {
				pick = n
				break
			}
		}
		if pick == "" && len(sameDir) == 1 {
			pick = sameDir[0]
		}
		if pick != "" {
			d.app = pick
		} else {
			d.newApp = true
			base := filepath.Base(wd)
			if wd == "." {
				base = filepath.Base(pr.wsPath)
			}
			name := slug(base)
			if name == "" || !config.ValidAppName(name) {
				name = "app"
			}
			cand := name
			for i := 2; ; i++ {
				if _, taken := apps[cand]; !taken {
					break
				}
				cand = fmt.Sprintf("%s-%d", name, i)
			}
			d.app = cand
		}
	}
	d.mode = wantMode
	if d.mode == "" {
		d.mode = DeriveMode(o.Command, o.Env, task)
	}
	if !d.newApp {
		if app, ok := apps[d.app]; ok {
			if _, exists := app.Modes[d.mode]; exists && wantMode == "" {
				base := d.mode
				for i := 2; ; i++ {
					cand := fmt.Sprintf("%s-%d", base, i)
					if _, taken := app.Modes[cand]; !taken {
						d.mode = cand
						break
					}
				}
			}
			_, d.modeExists = app.Modes[d.mode]
		}
	}
	return d, nil
}

func renderModeYAML(app, mode string, d saveDraft, checks []config.Check, description string) string {
	m := d.modeSpec
	if description != "" {
		m.Description = description
	}
	if len(checks) > 0 {
		m.Checks = checks
	}
	if mode == "" {
		block := struct {
			Description string             `yaml:"description,omitempty"`
			EnvSources  []config.EnvSource `yaml:"env_sources,omitempty"`
			Checks      []config.Check     `yaml:"checks,omitempty"`
		}{description, m.EnvSources, m.Checks}
		var bn yaml.Node
		_ = bn.Encode(block)
		root := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode("apps"), {Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode(app), &bn}}}}
		return encodeNode(&root)
	}
	if d.newApp {
		block := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		addScalar(&block, "workdir", nonDot(d.workdir, m.WorkDir))
		if d.appType != "" {
			addScalar(&block, "type", d.appType)
		}
		addScalar(&block, "default_mode", mode)
		modes := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mm := m
		mm.WorkDir = ""
		var mn yaml.Node
		_ = mn.Encode(mm)
		modes.Content = append(modes.Content, strNode(mode), &mn)
		block.Content = append(block.Content, strNode("modes"), &modes)
		root := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode("apps"), {Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode(app), &block}}}}
		return encodeNode(&root)
	}
	var mn yaml.Node
	_ = mn.Encode(m)
	root := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode("apps"), {Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode(app), {Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode("modes"), {Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode(mode), &mn}}}}}}}}
	return encodeNode(&root)
}

func nonDot(a, b string) string {
	for _, v := range []string{b, a} {
		if v != "" && v != "." {
			return v
		}
	}
	return ""
}

func strNode(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }

func addScalar(m *yaml.Node, key, val string) {
	if val == "" {
		return
	}
	m.Content = append(m.Content, strNode(key), strNode(val))
}

func encodeNode(n *yaml.Node) string {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(n)
	_ = enc.Close()
	return buf.String()
}

func (e *Engine) SaveSuggestion(workspaceID, processID string) (*api.SaveSuggestion, error) {
	if !e.SuggestSaving() {
		return nil, nil
	}
	pr, rt, err := e.RuntimeFor(workspaceID)
	if err != nil {
		return nil, err
	}
	row, err := e.store.GetProcess(processID)
	if err != nil {
		return nil, nil
	}
	o := ParseOrigin(row.SpecJSON)
	if o.Shell || o.SavedApp != "" {
		return nil, nil
	}
	if o.App == "" && (len(o.Command) == 0 || saveDenylist[commandBase(o.Command[0])]) {
		return nil, nil
	}
	if o.App != "" && !o.hasOverrides() {
		return nil, nil
	}
	out, err := rt.Outcome(processID)
	if err != nil || !out.Success {
		return nil, nil
	}
	sig := o.signature(pr.wsPath)
	for _, s := range e.stringList(dismissKey(pr.projectID)) {
		if s == sig {
			return nil, nil
		}
	}
	if app, _ := e.existingMatch(pr, o); app != "" {
		return nil, nil
	}
	d, err := e.draftSave(pr, o, out.Profile, out.Kind == "task", "", "")
	if err != nil {
		return nil, nil
	}
	s := &api.SaveSuggestion{
		ProcessID: processID, Kind: out.Kind, Reason: out.Reason, App: d.app, Mode: d.mode, NewApp: d.newApp,
		ModeExists: d.modeExists, Type: d.appType, Command: d.modeSpec.Command, WorkDir: d.workdir,
		YAML: renderModeYAML(d.app, d.mode, d, nil, ""),
	}
	for _, src := range d.modeSpec.EnvSources {
		s.EnvSources = append(s.EnvSources, api.EnvSourceSpec{File: src.File, Optional: src.Optional, Vars: src.Vars})
	}
	what := "This run"
	switch out.Reason {
	case "ready":
		what = "The process started and reported ready"
	case "stable":
		what = "The process has been running without errors"
	case "exit_0":
		what = "The command finished successfully"
	case "ran_until_stopped":
		what = "The process ran fine until it was stopped"
	}
	target := fmt.Sprintf("app %q, mode %q", d.app, d.mode)
	if d.newApp {
		target = fmt.Sprintf("a new app %q with mode %q", d.app, d.mode)
	}
	s.Ask = fmt.Sprintf("%s. Save this setup (command, workdir, env) as %s so it can be started again with start_process(app=%q, mode=%q)? You can rename the app or mode, keep it only for this checkout, or say no.", what, target, d.app, d.mode)
	s.Next = fmt.Sprintf("Ask the user the question in ask (show yaml). Yes: save_run_config(process_id=%q, app=%q, mode=%q) with any names they prefer (scope=\"workspace\" to keep it to this checkout). No: save_run_config(process_id=%q, action=\"dismiss\") so it is not suggested again.", processID, d.app, d.mode, processID)
	return s, nil
}

func checksFromAPI(in []api.CheckSpec) ([]config.Check, error) {
	out := make([]config.Check, 0, len(in))
	for _, c := range in {
		cc := config.Check{Name: c.Name, Method: strings.ToUpper(c.Method), URL: c.URL, TCP: c.TCP, Headers: c.Headers, Body: c.Body, ExpectStatus: c.ExpectStatus, ExpectContains: c.ExpectContains}
		if cc.Method == "GET" {
			cc.Method = ""
		}
		if c.TimeoutMS > 0 {
			cc.Timeout = config.Duration(int64(c.TimeoutMS) * 1e6)
		}
		out = append(out, cc)
	}
	return out, nil
}

func (e *Engine) DismissSave(workspaceID, processID string) error {
	pr, _, err := e.RuntimeFor(workspaceID)
	if err != nil {
		return err
	}
	row, err := e.store.GetProcess(processID)
	if err != nil {
		return fmt.Errorf("unknown process %q", processID)
	}
	return e.addToList(dismissKey(pr.projectID), ParseOrigin(row.SpecJSON).signature(pr.wsPath))
}

func (e *Engine) SaveRun(workspaceID string, req api.SaveRunRequest, sessionID string) (*api.SaveRunResult, error) {
	if req.Action == "dismiss" {
		if err := e.DismissSave(workspaceID, req.ProcessID); err != nil {
			return nil, err
		}
		return &api.SaveRunResult{Dismissed: true, Message: "Not saved; this run will not be suggested again."}, nil
	}
	if req.Action != "" && req.Action != "save" {
		return nil, fmt.Errorf("invalid_argument: action must be save or dismiss")
	}
	pr, rt, err := e.RuntimeFor(workspaceID)
	if err != nil {
		return nil, err
	}
	row, err := e.store.GetProcess(req.ProcessID)
	if err != nil {
		return nil, fmt.Errorf("unknown process %q", req.ProcessID)
	}
	if row.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("invalid_argument: process %s belongs to another workspace", req.ProcessID)
	}
	o := ParseOrigin(row.SpecJSON)
	if o.Shell {
		return nil, fmt.Errorf("invalid_argument: shells opened with open_shell cannot be saved as apps")
	}
	if o.App == "" && len(o.Command) == 0 {
		return nil, fmt.Errorf("invalid_argument: process %s has no recorded start command", req.ProcessID)
	}
	if req.App != "" && !config.ValidAppName(req.App) {
		return nil, fmt.Errorf("invalid_argument: app name %q: use letters, digits, '.', '_' or '-'", req.App)
	}
	if req.Mode != "" && !config.ValidModeName(req.Mode) {
		return nil, fmt.Errorf("invalid_argument: mode name %q: use letters, digits, '.', '_' or '-'", req.Mode)
	}
	scope := req.Scope
	if scope == "" {
		scope = "project"
	}
	if scope != "project" && scope != "workspace" {
		return nil, fmt.Errorf("invalid_argument: scope must be project or workspace")
	}
	profile := ""
	task := false
	if out, err := rt.Outcome(req.ProcessID); err == nil {
		profile = out.Profile
		task = out.Kind == "task"
	}
	d, err := e.draftSave(pr, o, profile, task, req.App, req.Mode)
	if err != nil {
		return nil, err
	}
	if len(req.EnvSources) > 0 {
		d.modeSpec.EnvSources = nil
		for _, s := range req.EnvSources {
			d.modeSpec.EnvSources = append(d.modeSpec.EnvSources, config.EnvSource{File: s.File, Optional: s.Optional, Vars: config.EnvVars(s.Vars)})
		}
	}
	checks, err := checksFromAPI(req.Checks)
	if err != nil {
		return nil, err
	}
	layerWS := ""
	if scope == "workspace" {
		layerWS = workspaceID
	}
	cur, _, _ := e.store.GetConfig(pr.projectID, scope, layerWS)
	newYAML, outcome, err := upsertMode(cur, d, checks, req)
	if err != nil {
		return nil, err
	}
	res := &api.SaveRunResult{App: d.app, Mode: d.mode, Scope: scope, NewApp: d.newApp, StartWith: startWith(d.app, d.mode)}
	res.YAML = renderModeYAML(d.app, d.mode, d, checks, req.Description)
	if outcome == "unchanged" {
		res.Saved, res.Unchanged = true, true
		res.Message = "Already saved with the same settings."
	} else {
		if errs := config.Validate([]byte(newYAML)); len(errs) > 0 {
			return nil, fmt.Errorf("invalid_argument: saving would produce an invalid config: %v", errs[0])
		}
		msg := fmt.Sprintf("save %s mode %s from %s", d.app, d.mode, req.ProcessID)
		rev, _, err := e.ApplyProjectYAML(pr.projectID, scope, layerWS, newYAML, "saved", sessionID, msg, false)
		if err != nil {
			return nil, err
		}
		res.Saved, res.Revision, res.Replaced = true, rev, outcome == "replaced"
		res.Message = fmt.Sprintf("Saved. Next time: %s.", res.StartWith)
	}
	o.SavedApp, o.SavedMode = d.app, d.mode
	_ = e.store.UpdateProcessOrigin(req.ProcessID, d.app, o.JSON())
	return res, nil
}

func startWith(app, mode string) string {
	if mode == "" {
		return fmt.Sprintf("start_process(app=%q)", app)
	}
	return fmt.Sprintf("start_process(app=%q, mode=%q)", app, mode)
}

func upsertAppLevel(appNode *yaml.Node, d saveDraft, checks []config.Check, req api.SaveRunRequest) (string, error) {
	var curSources []config.EnvSource
	if n := findMapValue(appNode, "env_sources"); n != nil {
		if err := n.Decode(&curSources); err != nil {
			return "", err
		}
	}
	var curChecks []config.Check
	if n := findMapValue(appNode, "checks"); n != nil {
		if err := n.Decode(&curChecks); err != nil {
			return "", err
		}
	}
	nextSources := mergeSources(curSources, d.modeSpec.EnvSources)
	nextChecks := mergeCheckList(curChecks, checks)
	changed := false
	if !sameYAMLAny(curSources, nextSources) {
		if err := setNode(appNode, "env_sources", nextSources); err != nil {
			return "", err
		}
		changed = true
	}
	if !sameYAMLAny(curChecks, nextChecks) {
		if err := setNode(appNode, "checks", nextChecks); err != nil {
			return "", err
		}
		changed = true
	}
	if req.Description != "" {
		if cur := findMapValue(appNode, "description"); cur == nil || cur.Value != req.Description {
			setScalar(appNode, "description", req.Description)
			changed = true
		}
	}
	if !changed {
		return "unchanged", nil
	}
	return "updated", nil
}

func sameYAMLAny(a, b any) bool {
	ab, _ := yaml.Marshal(a)
	bb, _ := yaml.Marshal(b)
	return bytes.Equal(ab, bb)
}

func setNode(m *yaml.Node, key string, v any) error {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return err
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = &n
			return nil
		}
	}
	m.Content = append(m.Content, strNode(key), &n)
	return nil
}

func ensureMap(parent *yaml.Node, key string) *yaml.Node {
	if v := findMapValue(parent, key); v != nil {
		if v.Kind != yaml.MappingNode {
			v.Kind, v.Tag, v.Value, v.Style, v.Content = yaml.MappingNode, "!!map", "", 0, nil
		}
		return v
	}
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, strNode(key), m)
	return m
}

func setScalar(m *yaml.Node, key, val string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = strNode(val)
			return
		}
	}
	m.Content = append(m.Content, strNode(key), strNode(val))
}

func modeFromNode(n *yaml.Node) (config.Mode, error) {
	var m config.Mode
	if n == nil {
		return m, nil
	}
	err := n.Decode(&m)
	return m, err
}

func mergeSources(existing []config.EnvSource, add []config.EnvSource) []config.EnvSource {
	out := append([]config.EnvSource(nil), existing...)
	for _, s := range add {
		if s.File != "" {
			dup := false
			for _, x := range out {
				if x.File == s.File {
					dup = true
				}
			}
			if !dup {
				out = append(out, s)
			}
			continue
		}
		if len(out) > 0 && out[len(out)-1].Vars != nil {
			last := &out[len(out)-1]
			merged, _ := config.MergeEnv(config.EnvLayer{Vars: last.Vars}, config.EnvLayer{Vars: s.Vars})
			last.Vars = config.EnvVars(merged)
			continue
		}
		out = append(out, s)
	}
	return out
}

func upsertMode(cur []byte, d saveDraft, checks []config.Check, req api.SaveRunRequest) (string, string, error) {
	var doc yaml.Node
	if len(bytes.TrimSpace(cur)) > 0 {
		if err := yaml.Unmarshal(cur, &doc); err != nil {
			return "", "", err
		}
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{strNode("version"), {Kind: yaml.ScalarNode, Tag: "!!int", Value: "3"}}}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return "", "", errors.New("project config is not a mapping")
	}
	apps := ensureMap(root, "apps")
	appNode := findMapValue(apps, d.app)
	createdHere := appNode == nil
	if appNode == nil {
		appNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		apps.Content = append(apps.Content, strNode(d.app), appNode)
		if d.newApp {
			if wd := nonDot(d.workdir, d.modeSpec.WorkDir); wd != "" {
				setScalar(appNode, "workdir", wd)
			}
			if d.appType != "" {
				setScalar(appNode, "type", d.appType)
			}
		}
	} else if appNode.Kind != yaml.MappingNode {
		return "", "", fmt.Errorf("apps.%s is not a mapping", d.app)
	}
	if d.mode == "" {
		if !d.appendOnly {
			return "", "", fmt.Errorf("invalid_argument: a mode name is required")
		}
		outcome, err := upsertAppLevel(appNode, d, checks, req)
		if err != nil || outcome == "unchanged" {
			return "", outcome, err
		}
		return encodeDoc(&doc, outcome)
	}
	modes := ensureMap(appNode, "modes")
	modeNode := findMapValue(modes, d.mode)
	spec := d.modeSpec
	if d.newApp {
		spec.WorkDir = ""
	}
	outcome := "added"
	if modeNode != nil {
		existing, err := modeFromNode(modeNode)
		if err != nil {
			return "", "", err
		}
		next := existing
		if d.appendOnly {
			next.EnvSources = mergeSources(existing.EnvSources, spec.EnvSources)
		} else {
			candidate := spec
			candidate.Description = existing.Description
			candidate.Checks = existing.Checks
			same := sameModeYAML(existing, candidate)
			if !same && !req.Overwrite {
				return "", "", fmt.Errorf("invalid_argument: app %q already has a mode %q with different settings; pick another mode name or pass overwrite=true", d.app, d.mode)
			}
			if !same {
				next = candidate
			}
		}
		if req.Description != "" {
			next.Description = req.Description
		}
		next.Checks = mergeCheckList(next.Checks, checks)
		if sameModeYAML(existing, next) {
			return "", "unchanged", nil
		}
		var n yaml.Node
		if err := n.Encode(next); err != nil {
			return "", "", err
		}
		*modeNode = n
		outcome = "replaced"
		if d.appendOnly {
			outcome = "updated"
		}
	} else {
		if d.appendOnly {
			spec.Command = nil
		}
		if req.Description != "" {
			spec.Description = req.Description
		}
		spec.Checks = mergeCheckList(spec.Checks, checks)
		var n yaml.Node
		if err := n.Encode(spec); err != nil {
			return "", "", err
		}
		modes.Content = append(modes.Content, strNode(d.mode), &n)
	}
	hasDefault := findMapValue(appNode, "default_mode") != nil
	hasCommand := findMapValue(appNode, "command") != nil
	if req.SetDefault || (createdHere && d.newApp) || (!hasDefault && !hasCommand && len(modes.Content) == 2 && createdHere) {
		setScalar(appNode, "default_mode", d.mode)
	}
	reorderApp(appNode)
	return encodeDoc(&doc, outcome)
}

func encodeDoc(doc *yaml.Node, outcome string) (string, string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return "", "", err
	}
	if err := enc.Close(); err != nil {
		return "", "", err
	}
	return buf.String(), outcome, nil
}

func sameModeYAML(a, b config.Mode) bool {
	ab, _ := yaml.Marshal(a)
	bb, _ := yaml.Marshal(b)
	return bytes.Equal(ab, bb)
}

func mergeCheckList(base, add []config.Check) []config.Check {
	if len(add) == 0 {
		return base
	}
	out := append([]config.Check(nil), base...)
	for _, c := range add {
		replaced := false
		for i := range out {
			if out[i].Name == c.Name {
				out[i] = c
				replaced = true
			}
		}
		if !replaced {
			out = append(out, c)
		}
	}
	return out
}

func reorderApp(app *yaml.Node) {
	idx := -1
	for i := 0; i+1 < len(app.Content); i += 2 {
		if app.Content[i].Value == "modes" {
			idx = i
		}
	}
	if idx < 0 || idx == len(app.Content)-2 {
		return
	}
	k, v := app.Content[idx], app.Content[idx+1]
	app.Content = append(app.Content[:idx], app.Content[idx+2:]...)
	app.Content = append(app.Content, k, v)
}

func (e *Engine) GitignoreStatus(workspaceID string) (integrate.GitignoreStatus, error) {
	pr, err := e.GetOrCreateRuntime(workspaceID)
	if err != nil {
		return integrate.GitignoreStatus{}, err
	}
	EnsureConfig(pr)
	var files []string
	if res := pr.ResolvedConfig(); res != nil {
		for _, app := range res.Apps {
			for _, m := range append([]string{""}, app.ModeNames()...) {
				r, err := app.ResolveMode(m)
				if err != nil {
					continue
				}
				for _, s := range r.EnvChain {
					if s.File != "" {
						files = append(files, s.File)
					}
				}
			}
		}
	}
	return integrate.CheckGitignore(pr.wsPath, files, e.stringList("gitignore_dismissed:"+pr.projectID)), nil
}

func (e *Engine) UpdateGitignore(workspaceID string, entries []string, dismiss bool) (map[string]any, error) {
	pr, err := e.GetOrCreateRuntime(workspaceID)
	if err != nil {
		return nil, err
	}
	st, err := e.GitignoreStatus(workspaceID)
	if err != nil {
		return nil, err
	}
	if !st.Repo {
		return nil, fmt.Errorf("invalid_argument: %s is not inside a git repository", pr.wsPath)
	}
	if len(entries) == 0 {
		for _, m := range st.Missing {
			entries = append(entries, m.Path)
		}
	}
	if dismiss {
		for _, en := range entries {
			_ = e.addToList("gitignore_dismissed:"+pr.projectID, en)
		}
		return map[string]any{"dismissed": entries}, nil
	}
	added, err := integrate.AppendGitignore(st.RepoRoot, entries)
	if err != nil {
		return nil, err
	}
	after, _ := e.GitignoreStatus(workspaceID)
	out := map[string]any{"added": added, "gitignore": st.Gitignore, "status": after}
	if len(after.Tracked) > 0 {
		out["hint"] = "These files are already committed; ignoring them does not untrack them. If the user wants, run: git rm --cached -r " + strings.Join(after.Tracked, " ")
	}
	if added == nil {
		out["added"] = []string{}
	}
	return out, nil
}

func Saveable(o RunOrigin) bool {
	if o.Shell || o.SavedApp != "" {
		return false
	}
	if o.App != "" {
		return o.hasOverrides()
	}
	return len(o.Command) > 0 && !saveDenylist[commandBase(o.Command[0])]
}
