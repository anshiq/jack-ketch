package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"jack-ketch/internal/config"
	"jack-ketch/internal/diagnose"
	"jack-ketch/internal/logs"
	"jack-ketch/internal/process"
	"jack-ketch/pkg/api"
)

const (
	stableAfter          = 10 * time.Second
	stableWithReadiness  = 30 * time.Second
	defaultProbeSettle   = 300 * time.Millisecond
	maxProbeBody         = 16 * 1024
	maxProbeLogs         = 200
	diagnoseWindow       = 400
	ConfirmationRequired = "confirmation_required"
)

type processOrigin struct {
	app      string
	mode     string
	envFiles []string
}

func (r *Runtime) SetPortOwner(fn func(port int) string) {
	r.portOwnerMu.Lock()
	r.portOwner = fn
	r.portOwnerMu.Unlock()
}

func (r *Runtime) recordOrigin(id string, o processOrigin) {
	r.envSrcMu.Lock()
	r.origins[id] = o
	r.envSrcMu.Unlock()
}

func (r *Runtime) origin(id string) processOrigin {
	r.envSrcMu.RLock()
	defer r.envSrcMu.RUnlock()
	return r.origins[id]
}

func (r *Runtime) readinessMatcher(proc *process.ManagedProcess) func(string) bool {
	if len(proc.Spec.Readiness) > 0 {
		return proc.Spec.Supervision.CompileReadiness().Ready
	}
	if prof := r.profiles.Lookup(proc.Profile); prof != nil && len(prof.Readiness) > 0 {
		return prof.Ready
	}
	return nil
}

func (r *Runtime) instanceEntries(proc *process.ManagedProcess, max int) []logs.Entry {
	entries := proc.Logs.From(proc.EntryFrom())
	if len(entries) > max {
		entries = entries[len(entries)-max:]
	}
	return entries
}

var stderrErrorRe = regexp.MustCompile(`(?i)(error|exception|traceback|fatal|panic|refused|ECONN|EADDRINUSE)`)

func (r *Runtime) Outcome(id string) (*api.RunOutcome, error) {
	proc, ok := r.manager.Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", id)
	}
	info := proc.Info()
	out := &api.RunOutcome{ProcessID: id, Status: string(info.Status), ExitCode: info.ExitCode, Health: string(proc.Health().State), Profile: info.Profile}
	if !info.StartedAt.IsZero() {
		end := time.Now()
		if info.ExitedAt != nil {
			end = *info.ExitedAt
		}
		out.UptimeMS = end.Sub(info.StartedAt).Milliseconds()
	}
	entries := r.instanceEntries(proc, 2000)
	if match := r.readinessMatcher(proc); match != nil {
		out.HasReadiness = true
		for _, e := range entries {
			if match(e.Line) {
				out.Ready = true
				out.ReadyLine = e.Line
				break
			}
		}
	}
	uptime := time.Duration(out.UptimeMS) * time.Millisecond
	healthy := out.Health != "unhealthy"
	switch info.Status {
	case process.StatusRunning, process.StatusStarting:
		switch {
		case out.Ready && healthy:
			out.Success, out.Kind, out.Reason = true, "server", "ready"
		case !out.HasReadiness && uptime >= stableAfter && healthy && !recentStderrErrors(entries):
			out.Success, out.Kind, out.Reason = true, "server", "stable"
		case out.HasReadiness && uptime >= stableWithReadiness && healthy && !recentStderrErrors(entries):
			out.Success, out.Kind, out.Reason = true, "server", "stable"
		}
	case process.StatusExited:
		if info.ExitCode != nil && *info.ExitCode == 0 {
			out.Success, out.Kind, out.Reason = true, "task", "exit_0"
		}
	case process.StatusStopped:
		if out.Ready || (!out.HasReadiness && uptime >= stableAfter && !recentStderrErrors(entries)) {
			out.Success, out.Kind, out.Reason = true, "server", "ran_until_stopped"
		}
	}
	return out, nil
}

func recentStderrErrors(entries []logs.Entry) bool {
	n := 0
	for i := len(entries) - 1; i >= 0 && n < 50; i-- {
		if entries[i].Stream != logs.StreamStderr {
			continue
		}
		n++
		if stderrErrorRe.MatchString(entries[i].Line) {
			return true
		}
	}
	return false
}

func (r *Runtime) Diagnose(id string) (*api.Diagnosis, error) {
	proc, ok := r.manager.Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", id)
	}
	info := proc.Info()
	entries := r.instanceEntries(proc, diagnoseWindow)
	lines := make([]diagnose.Line, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, diagnose.Line{Stream: string(e.Stream), Text: e.Line})
	}
	o := r.origin(id)
	app := o.app
	if app == "" {
		app = proc.Spec.App
	}
	r.portOwnerMu.RLock()
	owner := r.portOwner
	r.portOwnerMu.RUnlock()
	if owner == nil {
		owner = r.localPortOwner
	}
	selfPID := info.PID
	in := diagnose.Input{
		ProcessID: id, Status: string(info.Status), ExitCode: info.ExitCode, Health: string(proc.Health().State),
		Command: info.Command, Args: info.Args, WorkDir: info.WorkDir, ProjectDir: r.getCfg().ProjectDir,
		Profile: info.Profile, App: app, Mode: o.mode, Lines: lines, Env: proc.Spec.Env, EnvFiles: o.envFiles,
		PortOwner: func(port int) string {
			desc := owner(port)
			if desc != "" && selfPID > 0 && strings.Contains(desc, id) {
				return ""
			}
			return desc
		},
		ServiceApps: r.serviceApps(),
	}
	return diagnose.Analyze(in), nil
}

func (r *Runtime) localPortOwner(port int) string {
	for _, p := range r.manager.List() {
		info := p.Info()
		if info.PID <= 0 || (info.Status != process.StatusRunning && info.Status != process.StatusStarting) {
			continue
		}
		for _, lp := range process.ListeningPortsForPID(info.PID) {
			if lp == port {
				return describeProcess(info.ID, p.Spec.App, p.Spec.Mode, info.Command)
			}
		}
	}
	return ""
}

func describeProcess(id, app, mode, command string) string {
	switch {
	case app != "" && mode != "":
		return fmt.Sprintf("process %s (app %s, mode %s)", id, app, mode)
	case app != "":
		return fmt.Sprintf("process %s (app %s)", id, app)
	}
	return fmt.Sprintf("process %s (%s)", id, command)
}

var serviceWords = []string{"postgres", "mysql", "mariadb", "redis", "mongo", "rabbit", "kafka", "elastic", "memcache", "minio", "localstack", "mailpit", "nats"}

func (r *Runtime) serviceApps() map[string]string {
	out := map[string]string{}
	cfg := r.getCfg()
	for _, name := range cfg.Names() {
		app := cfg.Config.Apps[name]
		hay := strings.ToLower(name + " " + strings.Join(app.Command, " "))
		for _, w := range serviceWords {
			if strings.Contains(hay, w) {
				key := w
				switch w {
				case "mongo":
					key = "mongodb"
				case "rabbit":
					key = "rabbitmq"
				case "elastic":
					key = "elasticsearch"
				case "memcache":
					key = "memcached"
				}
				if _, ok := out[key]; !ok {
					out[key] = name
				}
			}
		}
	}
	return out
}

func loopbackHost(h string) bool {
	h = strings.Trim(strings.ToLower(h), "[]")
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}

func safeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

type ConfirmError struct {
	Reason string
}

func (e *ConfirmError) Error() string {
	return ConfirmationRequired + ": " + e.Reason + ". Ask the user to approve this request, then call again with confirmed=true."
}

var envRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func expandFromEnv(s string, env []string) string {
	return envRefRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := envRefRe.FindStringSubmatch(m)
		key := sub[1]
		if key == "" {
			key = sub[2]
		}
		for i := len(env) - 1; i >= 0; i-- {
			k, v, ok := strings.Cut(env[i], "=")
			if ok && k == key {
				return v
			}
		}
		return m
	})
}

func envLookup(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(env[i], "=")
		if ok && k == key {
			return v
		}
	}
	return ""
}

func (r *Runtime) probeBase(proc *process.ManagedProcess, port int) (string, []int, error) {
	info := proc.Info()
	var ports []int
	if info.PID > 0 {
		ports = process.ListeningPortsForPID(info.PID)
	}
	if port > 0 {
		return fmt.Sprintf("http://127.0.0.1:%d", port), ports, nil
	}
	if p := envLookup(proc.Spec.Env, "PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			if len(ports) == 0 || containsInt(ports, n) {
				return fmt.Sprintf("http://127.0.0.1:%d", n), ports, nil
			}
		}
	}
	if hc := proc.Spec.Supervision.Health.HTTP; hc != "" {
		if u, err := url.Parse(hc); err == nil && u.Host != "" {
			return u.Scheme + "://" + u.Host, ports, nil
		}
	}
	switch len(ports) {
	case 0:
		return "", ports, errors.New("the process is not listening on any TCP port yet; pass port=, a full url, or wait_for_log(ready=true) first")
	case 1:
		return fmt.Sprintf("http://127.0.0.1:%d", ports[0]), ports, nil
	}
	sort.Ints(ports)
	return fmt.Sprintf("http://127.0.0.1:%d", ports[0]), ports, nil
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (r *Runtime) Probe(ctx context.Context, req api.ProbeRequest) (*api.ProbeResult, error) {
	proc, ok := r.manager.Get(req.ProcessID)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", req.ProcessID)
	}
	return r.probe(ctx, proc, req, false)
}

func (r *Runtime) probe(ctx context.Context, proc *process.ManagedProcess, req api.ProbeRequest, preapproved bool) (*api.ProbeResult, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	res := &api.ProbeResult{ProcessID: proc.ID, Logs: []api.LogEntry{}}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if timeout > 120*time.Second {
		timeout = 120 * time.Second
	}
	settle := time.Duration(req.SettleMS) * time.Millisecond
	if req.SettleMS <= 0 {
		settle = defaultProbeSettle
	}
	if settle > 5*time.Second {
		settle = 5 * time.Second
	}
	maxBody := req.MaxBodyBytes
	if maxBody <= 0 || maxBody > 256*1024 {
		maxBody = maxProbeBody
	}
	env := proc.Spec.Env

	var target *url.URL
	if req.TCP != "" {
		res.TCP = expandFromEnv(req.TCP, env)
		host, _, err := net.SplitHostPort(res.TCP)
		if err != nil {
			return nil, fmt.Errorf("tcp must be host:port: %w", err)
		}
		if !preapproved && !req.Confirmed && !loopbackHost(host) {
			return nil, &ConfirmError{Reason: "the target " + res.TCP + " is not on this machine"}
		}
	} else {
		raw := expandFromEnv(strings.TrimSpace(req.URL), env)
		if raw == "" {
			raw = "/"
		}
		if strings.HasPrefix(raw, "/") || !strings.Contains(raw, "://") {
			base, ports, err := r.probeBase(proc, req.Port)
			res.ListeningPorts = ports
			if err != nil {
				res.Error = err.Error()
				res.ProcessStatus = string(proc.Info().Status)
				res.Failures = []string{err.Error()}
				return res, nil
			}
			if !strings.HasPrefix(raw, "/") {
				raw = "/" + raw
			}
			raw = base + raw
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("invalid url %q", raw)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("unsupported scheme %q (http or https)", u.Scheme)
		}
		target = u
		res.Method = method
		res.URL = u.String()
		if !preapproved && !req.Confirmed {
			switch {
			case !loopbackHost(u.Hostname()):
				return nil, &ConfirmError{Reason: "the target " + u.Host + " is not on this machine"}
			case !safeMethod(method):
				return nil, &ConfirmError{Reason: method + " requests can change data"}
			}
		}
	}

	cursor := proc.Logs.NextID()
	start := time.Now()
	if target == nil {
		d := net.Dialer{Timeout: timeout}
		conn, err := d.DialContext(ctx, "tcp", res.TCP)
		res.DurationMS = time.Since(start).Milliseconds()
		if err != nil {
			res.Error = err.Error()
			res.Failures = append(res.Failures, "connect failed: "+err.Error())
		} else {
			_ = conn.Close()
			res.OK = true
		}
	} else {
		var body io.Reader
		if req.Body != "" {
			body = strings.NewReader(expandFromEnv(req.Body, env))
		}
		hctx, cancel := context.WithTimeout(ctx, timeout)
		hreq, err := http.NewRequestWithContext(hctx, method, target.String(), body)
		if err != nil {
			cancel()
			return nil, err
		}
		for k, v := range req.Headers {
			hreq.Header.Set(k, expandFromEnv(v, env))
		}
		if req.Body != "" && hreq.Header.Get("Content-Type") == "" {
			trimmed := strings.TrimSpace(req.Body)
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
				hreq.Header.Set("Content-Type", "application/json")
			}
		}
		hreq.Header.Set("User-Agent", "jack-ketch-probe")
		client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(hreq)
		if err != nil {
			cancel()
			res.DurationMS = time.Since(start).Milliseconds()
			res.Error = err.Error()
			res.Failures = append(res.Failures, "request failed: "+err.Error())
		} else {
			data, rerr := io.ReadAll(io.LimitReader(resp.Body, int64(maxBody)+1))
			_ = resp.Body.Close()
			cancel()
			res.DurationMS = time.Since(start).Milliseconds()
			res.Status = resp.StatusCode
			res.StatusText = resp.Status
			res.Headers = pickHeaders(resp.Header)
			if len(data) > maxBody {
				data = data[:maxBody]
				res.BodyTruncated = true
			}
			res.BodyBytes = len(data)
			res.Body = string(bytes.ToValidUTF8(data, []byte("?")))
			if rerr != nil {
				res.Failures = append(res.Failures, "reading body: "+rerr.Error())
			}
			res.OK = true
		}
	}
	if settle > 0 {
		select {
		case <-time.After(settle):
		case <-ctx.Done():
		}
	}
	entries := proc.Logs.From(cursor)
	if len(entries) > maxProbeLogs {
		entries = entries[len(entries)-maxProbeLogs:]
		res.LogsTruncated = true
	}
	for _, e := range entries {
		res.Logs = append(res.Logs, toAPIEntry(e))
		if e.Stream == logs.StreamStderr && stderrErrorRe.MatchString(e.Line) {
			res.ErrorLogs++
		}
	}
	res.ProcessStatus = string(proc.Info().Status)
	if res.ProcessStatus != string(process.StatusRunning) && res.ProcessStatus != string(process.StatusStarting) {
		res.Failures = append(res.Failures, "the process is "+res.ProcessStatus+" after the request")
	}
	if res.OK && target != nil {
		switch {
		case req.ExpectStatus != 0 && res.Status != req.ExpectStatus:
			res.Failures = append(res.Failures, fmt.Sprintf("expected status %d, got %d", req.ExpectStatus, res.Status))
		case req.ExpectStatus == 0 && res.Status >= 400:
			res.Failures = append(res.Failures, fmt.Sprintf("status %d", res.Status))
		}
		if req.ExpectContains != "" && !strings.Contains(res.Body, req.ExpectContains) {
			res.Failures = append(res.Failures, fmt.Sprintf("body does not contain %q", req.ExpectContains))
		}
	}
	if res.Error != "" && res.ListeningPorts == nil {
		if info := proc.Info(); info.PID > 0 {
			res.ListeningPorts = process.ListeningPortsForPID(info.PID)
		}
	}
	res.OK = res.OK && len(res.Failures) == 0
	check := &api.CheckSpec{Name: suggestCheckName(method, target, res.TCP), Method: method, ExpectStatus: req.ExpectStatus, ExpectContains: req.ExpectContains, Headers: req.Headers, Body: req.Body}
	if target != nil {
		check.URL = strings.TrimSpace(req.URL)
		if check.URL == "" {
			check.URL = "/"
		}
		if check.ExpectStatus == 0 && res.Status > 0 && res.Status < 400 {
			check.ExpectStatus = res.Status
		}
		if check.Method == http.MethodGet {
			check.Method = ""
		}
	} else {
		check.TCP, check.Method = req.TCP, ""
	}
	res.Check = check
	return res, nil
}

func suggestCheckName(method string, u *url.URL, tcp string) string {
	if u == nil {
		return "tcp-" + strings.NewReplacer(":", "-", ".", "-").Replace(tcp)
	}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		p = "root"
	}
	name := strings.NewReplacer("/", "-", "{", "", "}", "", ":", "").Replace(p)
	if method != http.MethodGet {
		name = strings.ToLower(method) + "-" + name
	}
	return name
}

var keptHeaders = []string{"Content-Type", "Content-Length", "Location", "Set-Cookie", "Cache-Control", "X-Request-Id", "X-Request-ID", "Www-Authenticate", "Retry-After", "Access-Control-Allow-Origin"}

func pickHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for _, k := range keptHeaders {
		if v := h.Get(k); v != "" {
			if k == "Set-Cookie" {
				v = "(set)"
			}
			out[k] = v
		}
	}
	return out
}

func (r *Runtime) checksFor(proc *process.ManagedProcess, app, mode string) ([]config.Check, string, string, error) {
	if app == "" {
		app = proc.Spec.App
		mode = proc.Spec.Mode
	}
	if app == "" {
		return nil, "", "", errors.New("this process was not started from a configured app, so it has no saved checks; save it first (save_run_config) or use probe")
	}
	base, ok := r.getCfg().Config.Apps[app]
	if !ok {
		return nil, app, mode, fmt.Errorf("unknown app %q", app)
	}
	resolved, err := base.ResolveMode(mode)
	if err != nil {
		return nil, app, mode, fmt.Errorf("app %q %w", app, err)
	}
	return resolved.Checks, app, resolved.Mode, nil
}

func (r *Runtime) findRunning(app, mode string) *process.ManagedProcess {
	var best *process.ManagedProcess
	for _, p := range r.manager.List() {
		if p.Spec.App != app || (mode != "" && p.Spec.Mode != mode) {
			continue
		}
		st := p.Info().Status
		if st == process.StatusRunning || st == process.StatusStarting {
			if best == nil || p.Info().StartedAt.After(best.Info().StartedAt) {
				best = p
			}
		}
	}
	return best
}

func (r *Runtime) RunChecks(ctx context.Context, req api.RunChecksRequest) (*api.RunChecksResult, error) {
	var proc *process.ManagedProcess
	if req.ProcessID != "" {
		p, ok := r.manager.Get(req.ProcessID)
		if !ok {
			return nil, fmt.Errorf("unknown process %q", req.ProcessID)
		}
		proc = p
	} else {
		if req.App == "" {
			return nil, errors.New("run_checks needs process_id, or app (and optionally mode) of a running process")
		}
		base, ok := r.getCfg().Config.Apps[req.App]
		if !ok {
			return nil, fmt.Errorf("unknown app %q", req.App)
		}
		mode, err := base.PickMode(req.Mode)
		if err != nil {
			return nil, fmt.Errorf("app %q %w", req.App, err)
		}
		proc = r.findRunning(req.App, mode)
		if proc == nil {
			return nil, fmt.Errorf("no running process for app %q mode %q; start it first", req.App, mode)
		}
	}
	appName, modeName := req.App, req.Mode
	if req.ProcessID != "" {
		appName, modeName = "", ""
	}
	checks, app, mode, err := r.checksFor(proc, appName, modeName)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, n := range req.Names {
		want[n] = true
	}
	out := &api.RunChecksResult{ProcessID: proc.ID, App: app, Mode: mode, Results: []api.ProbeResult{}}
	for _, c := range checks {
		if len(want) > 0 && !want[c.Name] {
			continue
		}
		pr, err := r.probe(ctx, proc, api.ProbeRequest{
			ProcessID: proc.ID, Method: c.EffectiveMethod(), URL: c.URL, TCP: c.TCP, Headers: c.Headers, Body: c.Body,
			ExpectStatus: c.ExpectStatus, ExpectContains: c.ExpectContains, TimeoutMS: int(c.EffectiveTimeout().Milliseconds()), SettleMS: 200,
		}, true)
		if err != nil {
			pr = &api.ProbeResult{ProcessID: proc.ID, Error: err.Error(), Failures: []string{err.Error()}, Logs: []api.LogEntry{}}
		}
		pr.Name = c.Name
		pr.Check = nil
		if pr.OK {
			out.Passed++
		} else {
			out.Failed++
		}
		out.Results = append(out.Results, *pr)
	}
	if len(out.Results) == 0 {
		if len(want) > 0 {
			return nil, fmt.Errorf("no checks named %s on app %q mode %q", strings.Join(req.Names, ", "), app, mode)
		}
		return nil, fmt.Errorf("app %q mode %q has no checks yet; probe the endpoints, then save them with save_run_config(checks=[...])", app, mode)
	}
	return out, nil
}

func envSourceLabels(chain []config.LabeledEnvSource) []string {
	out := make([]string, 0, len(chain))
	for _, s := range chain {
		if s.File != "" {
			label := "file " + s.File
			if s.Optional {
				label += " (optional)"
			}
			out = append(out, label)
			continue
		}
		keys := make([]string, 0, len(s.Vars))
		for _, kv := range s.Vars {
			k, _, _ := strings.Cut(kv, "=")
			keys = append(keys, k)
		}
		out = append(out, "vars "+strings.Join(keys, ", "))
	}
	return out
}

func checkNames(checks []config.Check) []string {
	out := make([]string, 0, len(checks))
	for _, c := range checks {
		out = append(out, c.Name)
	}
	return out
}

func appModes(app config.AppConfig) []api.ModeInfo {
	var out []api.ModeInfo
	def, _ := app.PickMode("")
	for _, name := range app.ModeNames() {
		m := app.Modes[name]
		resolved, err := app.ResolveMode(name)
		info := api.ModeInfo{Name: name, Description: m.Description, Command: m.Command, WorkDir: m.WorkDir, Default: name == def}
		if err == nil {
			if len(info.Command) == 0 {
				info.Command = resolved.Command
			}
			info.EnvSources = envSourceLabels(resolved.EnvChain)
			info.Checks = checkNames(resolved.Checks)
		}
		out = append(out, info)
	}
	return out
}

func DescribeApp(app config.AppConfig, info *api.AppInfo) {
	info.Description = app.Description
	info.DefaultMode = app.DefaultMode
	info.Modes = appModes(app)
	if base, err := app.ResolveMode(""); err == nil {
		info.EnvSources = envSourceLabels(base.EnvChain)
		info.Checks = checkNames(base.Checks)
		if base.Mode != "" && info.DefaultMode == "" {
			info.DefaultMode = base.Mode
		}
	}
}
