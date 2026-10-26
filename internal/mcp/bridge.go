package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"jack-ketch/internal/integrate"
	"jack-ketch/internal/project"
	"jack-ketch/internal/runtime"
	"jack-ketch/pkg/api"
	"jack-ketch/pkg/client"
)

type wsRef struct {
	ws, proj, path string
}

type Bridge struct {
	client   *client.Client
	root     *Bridge
	mu       sync.RWMutex
	wsID     string
	projID   string
	wsPath   string
	sessID   string
	harness  string
	logger   *slog.Logger
	respawn  func() error
	stop     chan struct{}
	stopOnce sync.Once
	cacheMu  sync.Mutex
	wsCache  map[string]wsRef
}

var _ runtime.Facade = (*Bridge)(nil)

type BridgeOptions struct {
	SocketPath    string
	WorkspacePath string
	Harness       string
	Respawn       func() error
	Logger        *slog.Logger
}

func DialBridge(socketPath, workspacePath, harnessHint string, logger *slog.Logger) (*Bridge, error) {
	return DialBridgeWith(BridgeOptions{SocketPath: socketPath, WorkspacePath: workspacePath, Harness: harnessHint, Logger: logger})
}

func DialBridgeWith(o BridgeOptions) (*Bridge, error) {
	logger := o.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cl, err := client.EnsureDaemonAs(o.SocketPath, "mcp/"+Version)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := cl.GetVersion(ctx); err != nil {
		return nil, fmt.Errorf("bridge: daemon handshake: %w", err)
	}
	harness := o.Harness
	if harness == "" {
		harness = integrate.HarnessFromEnv()
	}
	if harness == "" {
		harness = "unknown"
	}
	b := &Bridge{client: cl, harness: harness, logger: logger, respawn: o.Respawn, stop: make(chan struct{}), wsCache: map[string]wsRef{}}
	b.root = b
	explicit := o.WorkspacePath != ""
	path := o.WorkspacePath
	if path == "" {
		path = DefaultWorkspacePath()
	}
	if path != "" {
		if err := b.bindPath(ctx, path); err != nil {
			if explicit {
				return nil, err
			}
			logger.Warn("bridge: could not bind the default workspace; starting unbound", "path", path, "error", err)
		}
	}
	if err := b.register(ctx); err != nil {
		return nil, err
	}
	cl.OnDialError(b.reconnect)
	go b.heartbeat()
	return b, nil
}

func (b *Bridge) register(ctx context.Context) error {
	r := b.root
	r.mu.RLock()
	ws, harness := r.wsID, r.harness
	r.mu.RUnlock()
	reg, err := r.client.SessionService().RegisterWith(ctx, client.RegisterRequest{Kind: "mcp", Harness: harness, ClientPID: os.Getpid(), WorkspaceID: ws})
	if err != nil {
		return fmt.Errorf("bridge: register session: %w", err)
	}
	r.mu.Lock()
	r.sessID = reg.SessionID
	r.mu.Unlock()
	r.client.SetHeader(client.SessionHeader, reg.SessionID)
	r.client.SetHeader(client.HarnessHeader, harness)
	return nil
}

func (b *Bridge) reconnect() error {
	r := b.root
	if r.respawn == nil {
		return errors.New("jack-ketch daemon is unreachable")
	}
	if err := r.respawn(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return r.register(ctx)
}

func (b *Bridge) heartbeat() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := b.client.SessionService().Ping(ctx, b.SessionID())
			if err != nil && (strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "closed")) {
				if rerr := b.register(ctx); rerr != nil {
					b.logger.Warn("bridge: re-register failed", "error", rerr)
				}
			}
			cancel()
		}
	}
}

func (b *Bridge) Client() *client.Client { return b.client }

func (b *Bridge) ws() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.wsID
}

func (b *Bridge) proj() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.projID
}

func (b *Bridge) WorkspaceID() string { return b.ws() }

func (b *Bridge) ProjectID() string { return b.proj() }

func (b *Bridge) WorkspacePath() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.wsPath
}

func (b *Bridge) Bound() bool { return b.ws() != "" }

func (b *Bridge) SessionID() string {
	r := b.root
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessID
}

func (b *Bridge) Harness() string {
	r := b.root
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.harness
}

func (b *Bridge) Close() error {
	if b.root != b {
		return nil
	}
	b.stopOnce.Do(func() { close(b.stop) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return b.client.SessionService().Close(ctx, b.SessionID())
}

func (b *Bridge) resolveRef(ctx context.Context, pathOrID string) (wsRef, error) {
	r := b.root
	r.cacheMu.Lock()
	if ref, ok := r.wsCache[pathOrID]; ok {
		r.cacheMu.Unlock()
		return ref, nil
	}
	r.cacheMu.Unlock()
	var ref wsRef
	if strings.HasPrefix(pathOrID, "ws_") {
		found, err := r.lookupWorkspaceID(ctx, pathOrID)
		if err != nil {
			return wsRef{}, err
		}
		ref = found
	} else {
		if !filepath.IsAbs(pathOrID) {
			return wsRef{}, fmt.Errorf("workspace must be an absolute path, got %q", pathOrID)
		}
		if st, err := os.Stat(pathOrID); err != nil || !st.IsDir() {
			return wsRef{}, fmt.Errorf("workspace %q is not a directory", pathOrID)
		}
		root := project.WorkspaceRoot(pathOrID)
		res, err := r.client.ProjectService().Resolve(ctx, root)
		if err != nil {
			return wsRef{}, fmt.Errorf("resolve %s: %w", root, err)
		}
		ref = wsRef{ws: res.WorkspaceID, proj: res.ProjectID, path: root}
	}
	r.cacheMu.Lock()
	r.wsCache[pathOrID] = ref
	r.cacheMu.Unlock()
	return ref, nil
}

func (b *Bridge) lookupWorkspaceID(ctx context.Context, id string) (wsRef, error) {
	projects, err := b.client.ProjectService().ListProjects(ctx)
	if err != nil {
		return wsRef{}, err
	}
	for _, p := range projects {
		wss, err := b.client.ProjectService().ListWorkspaces(ctx, p.ID)
		if err != nil {
			continue
		}
		for _, w := range wss {
			if w.ID == id {
				return wsRef{ws: w.ID, proj: p.ID, path: w.Path}, nil
			}
		}
	}
	return wsRef{}, fmt.Errorf("unknown workspace %q", id)
}

func (b *Bridge) bindPath(ctx context.Context, path string) error {
	ref, err := b.resolveRef(ctx, path)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.wsID, b.projID, b.wsPath = ref.ws, ref.proj, ref.path
	b.mu.Unlock()
	return nil
}

func (b *Bridge) ForWorkspace(ctx context.Context, pathOrID string) (*Bridge, error) {
	ref, err := b.resolveRef(ctx, pathOrID)
	if err != nil {
		return nil, err
	}
	return &Bridge{client: b.client, root: b.root, wsID: ref.ws, projID: ref.proj, wsPath: ref.path, logger: b.logger}, nil
}

func (b *Bridge) SetWorkspace(ctx context.Context, pathOrID string) (wsRef, error) {
	r := b.root
	ref, err := r.resolveRef(ctx, pathOrID)
	if err != nil {
		return wsRef{}, err
	}
	r.mu.Lock()
	r.wsID, r.projID, r.wsPath = ref.ws, ref.proj, ref.path
	r.mu.Unlock()
	_ = r.client.SessionService().Update(ctx, client.SessionUpdate{SessionID: r.SessionID(), WorkspaceID: ref.ws})
	return ref, nil
}

func (b *Bridge) UpdateClient(ctx context.Context, harness, clientName, clientVersion string) {
	r := b.root
	if harness != "" {
		r.mu.Lock()
		r.harness = harness
		r.mu.Unlock()
		r.client.SetHeader(client.HarnessHeader, harness)
	}
	_ = r.client.SessionService().Update(ctx, client.SessionUpdate{SessionID: r.SessionID(), Harness: harness, HarnessVersion: clientVersion, ClientName: clientName})
}

func convert(in any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (b *Bridge) Start(ctx context.Context, req api.StartRequest) (*api.StartResult, error) {
	env := map[string]string{}
	for _, kv := range req.Env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[k] = v
	}
	var cmd []string
	if req.Command != "" {
		cmd = append([]string{req.Command}, req.Args...)
	}
	res, err := b.client.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: b.ws(), App: req.App, Command: cmd,
		WorkDir: req.WorkDir, Env: env, EnvFile: req.EnvFile, Lifetime: req.Lifetime, SessionID: b.SessionID(),
	})
	if err != nil {
		return nil, err
	}
	out := &api.StartResult{
		ProcessID: res.ProcessID, InstanceID: res.InstanceID,
		Status: res.Status, Command: req.Command, Args: req.Args, WorkDir: req.WorkDir,
	}
	if st, err := b.Status(res.ProcessID); err == nil {
		out.Status, out.Command, out.Args, out.WorkDir, out.Profile = st.Status, st.Command, st.Args, st.WorkDir, st.Profile
	}
	return out, nil
}

func (b *Bridge) Stop(ctx context.Context, id string) error {
	return b.client.ProcessService().Stop(ctx, id)
}

func (b *Bridge) Restart(ctx context.Context, id string) error {
	_, err := b.client.ProcessService().Restart(ctx, id)
	return err
}

func (b *Bridge) SetRestartPolicy(req api.SetRestartPolicyRequest) (*api.SetRestartPolicyResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.client.ProcessService().SetRestartPolicy(ctx, req.ProcessID, req.Policy); err != nil {
		return nil, err
	}
	return &api.SetRestartPolicyResult{ProcessID: req.ProcessID, RestartPolicy: req.Policy}, nil
}

func (b *Bridge) Status(id string) (*api.StatusResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := b.client.ProcessService().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var out api.StatusResult
	if err := convert(got, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) List() (*api.ListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	items, err := b.client.ProcessService().List(ctx, b.ws(), false)
	if err != nil {
		return nil, err
	}
	out := &api.ListResult{}
	for _, it := range items {
		var s api.ProcessSummary
		if err := convert(it, &s); err != nil {
			continue
		}
		out.Processes = append(out.Processes, s)
	}
	return out, nil
}

func (b *Bridge) GetLogs(req api.GetLogsRequest) (*api.GetLogsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	got, err := b.client.LogService().Query(ctx, req)
	if err != nil {
		return nil, err
	}
	var out api.GetLogsResult
	if err := convert(got, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) ClearLogs(id, stream string) (*api.ClearLogsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.client.LogService().Clear(ctx, id, stream); err != nil {
		return nil, err
	}
	return &api.ClearLogsResult{ProcessID: id, Cleared: true}, nil
}

func (b *Bridge) SendStdin(id, data string) (*api.SendStdinResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.client.ProcessService().SendStdin(ctx, id, data); err != nil {
		return nil, err
	}
	return &api.SendStdinResult{Written: len(data)}, nil
}

func (b *Bridge) RemoveProcess(id string, force bool) (*api.RemoveProcessResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.client.ProcessService().Remove(ctx, id, force); err != nil {
		return nil, err
	}
	return &api.RemoveProcessResult{ProcessID: id, Removed: true}, nil
}

func waitBudget(ms int) time.Duration {
	if ms <= 0 {
		ms = 30000
	}
	return time.Duration(ms)*time.Millisecond + 15*time.Second
}

func (b *Bridge) WaitForLog(ctx context.Context, req api.WaitForLogRequest) (*api.WaitForLogResult, error) {
	ctx, cancel := context.WithTimeout(ctx, waitBudget(req.TimeoutMS))
	defer cancel()
	got, err := b.client.LogService().WaitForLog(ctx, map[string]any{
		"process_id": req.ProcessID, "contains": req.Contains,
		"pattern": req.Pattern, "ready": req.Ready, "timeout_ms": req.TimeoutMS,
	})
	if err != nil {
		return nil, err
	}
	var out api.WaitForLogResult
	if err := convert(got, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) WaitForExit(ctx context.Context, req api.WaitForExitParams) (*api.WaitForExitResult, error) {
	ctx, cancel := context.WithTimeout(ctx, waitBudget(req.TimeoutMS))
	defer cancel()
	got, err := b.client.ProcessService().WaitForExit(ctx, req.ProcessID, req.TimeoutMS)
	if err != nil {
		return nil, err
	}
	var out api.WaitForExitResult
	if err := convert(got, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func pick(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asStrings(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (b *Bridge) Apps() (*api.ListAppsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := b.client.ConfigService().Get(ctx, b.ws())
	if err != nil {
		return nil, err
	}
	auto := map[string]bool{}
	for _, name := range asStrings(cfg["autoApps"]) {
		auto[name] = true
	}
	apps, _ := cfg["apps"].(map[string]any)
	names := make([]string, 0, len(apps))
	for name := range apps {
		names = append(names, name)
	}
	sort.Strings(names)
	out := &api.ListAppsResult{Apps: []api.AppInfo{}}
	for _, name := range names {
		m, _ := apps[name].(map[string]any)
		info := api.AppInfo{Name: name, Configured: true, Auto: auto[name]}
		if cmd := asStrings(pick(m, "Command", "command")); len(cmd) > 0 {
			info.Command, info.Args = cmd[0], cmd[1:]
		}
		info.WorkDir = asString(pick(m, "WorkDir", "workdir"))
		info.Type = asString(pick(m, "Type", "type"))
		info.Lifetime = asString(pick(m, "Lifetime", "lifetime"))
		info.Autostart, _ = pick(m, "Autostart", "autostart").(bool)
		info.DependsOn = asStrings(pick(m, "DependsOn", "depends_on"))
		out.Apps = append(out.Apps, info)
	}
	return out, nil
}

func (b *Bridge) SignalProcess(ctx context.Context, req api.SignalProcessRequest) (*api.SignalProcessResult, error) {
	if err := b.client.ProcessService().Signal(ctx, req.ProcessID, req.Signal); err != nil {
		return nil, err
	}
	return &api.SignalProcessResult{ProcessID: req.ProcessID, Signal: req.Signal}, nil
}

func (b *Bridge) ProcessEnv(req api.ProcessEnvRequest) (*api.ProcessEnvResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := b.client.ProcessService().GetEnv(ctx, req.ProcessID, req.Reveal, req.Live)
	if err != nil {
		return nil, err
	}
	var out api.ProcessEnvResult
	if err := convert(got, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) OpenShell(ctx context.Context, req api.OpenShellRequest) (*api.StartResult, error) {
	got, err := b.client.ProcessService().OpenShell(ctx, client.OpenShellRequest{
		WorkspaceID: b.ws(), ProcessID: req.ProcessID, App: req.App, PID: req.PID, Shell: req.Shell, SessionID: b.SessionID(),
	})
	if err != nil {
		return nil, err
	}
	id, _ := got["processId"].(string)
	inst, _ := got["instanceId"].(string)
	status, _ := got["status"].(string)
	out := &api.StartResult{ProcessID: id, InstanceID: inst, Status: status}
	if st, err := b.Status(id); err == nil {
		out.Status, out.Command, out.Args, out.WorkDir, out.Profile = st.Status, st.Command, st.Args, st.WorkDir, st.Profile
	}
	return out, nil
}

func (b *Bridge) SubscribeEvents(clientName string, req api.SubscribeEventsRequest) (*api.SubscribeEventsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, err := b.client.EventService().Subscribe(ctx, client.SubscribeRequest{
		SessionID: b.SessionID(), WorkspaceID: b.ws(), ProcessID: req.ProcessID, Types: req.Types, Since: req.Since,
	})
	if err != nil {
		return nil, err
	}
	return &api.SubscribeEventsResult{SubscriptionID: id}, nil
}

func (b *Bridge) GetEvents(req api.GetEventsRequest) (*api.GetEventsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := b.client.EventService().Drain(ctx, req.SubscriptionID, req.Limit)
	if err != nil {
		return nil, err
	}
	if res.SubscriptionID == "" {
		res.SubscriptionID = req.SubscriptionID
	}
	if res.Events == nil {
		res.Events = []api.EventDTO{}
	}
	return res, nil
}

func (b *Bridge) UnsubscribeEvents(clientName, subscriptionID string) (*api.UnsubscribeEventsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.client.EventService().Unsubscribe(ctx, subscriptionID); err != nil {
		return nil, err
	}
	return &api.UnsubscribeEventsResult{SubscriptionID: subscriptionID, Unsubscribed: true}, nil
}

func (b *Bridge) RuntimeStats() (*api.RuntimeStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if b.Bound() {
		return b.client.SystemService().RuntimeStats(ctx, b.ws())
	}
	stats, err := b.client.SystemService().GetStats(ctx)
	if err != nil {
		return nil, err
	}
	return &api.RuntimeStats{TotalProcesses: int(stats.Processes), ProcessesByStatus: map[string]int{}}, nil
}

func (b *Bridge) GetAuditLog(lines int, contains string) (*api.AuditLogResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if lines <= 0 {
		lines = 100
	}
	rows, err := b.client.AuditService().List(ctx, b.ws(), lines)
	if err != nil {
		return nil, err
	}
	out := &api.AuditLogResult{Entries: []api.AuditEntry{}}
	for _, e := range rows {
		ts, _ := e["ts"].(float64)
		args := asString(e["args"])
		ae := api.AuditEntry{
			Time:   time.Unix(int64(ts), 0).UTC().Format(time.RFC3339),
			Tool:   asString(e["action"]),
			Result: asString(e["result"]),
			Caller: asString(e["harness"]),
		}
		if ae.Caller == "" {
			ae.Caller = asString(e["sessionId"])
		}
		if d, ok := e["durationMs"].(float64); ok {
			ae.DurationMS = int64(d)
		}
		if args != "" {
			var m map[string]any
			if json.Unmarshal([]byte(args), &m) == nil {
				ae.Args = m
			}
		}
		if contains != "" && !strings.Contains(ae.Tool+" "+args+" "+ae.Result, contains) {
			continue
		}
		out.Entries = append(out.Entries, ae)
	}
	out.Lines = len(out.Entries)
	return out, nil
}

func (b *Bridge) ProjectInfo(ctx context.Context) (map[string]any, error) {
	if !b.Bound() {
		return map[string]any{"bound": false, "hint": unboundHint}, nil
	}
	proj, err := b.client.ProjectService().GetProject(ctx, b.proj())
	if err != nil {
		return nil, err
	}
	cfg, err := b.client.ConfigService().Get(ctx, b.ws())
	if err != nil {
		return nil, err
	}
	sessions, _ := b.client.SessionService().List(ctx, b.ws())
	return map[string]any{
		"projectId": b.proj(), "workspaceId": b.ws(),
		"workspacePath": b.WorkspacePath(), "sessionId": b.SessionID(),
		"project": proj, "configSource": cfg["configSource"],
		"configRevision": cfg["configRevision"], "revision": cfg["revision"],
		"pendingProposal": cfg["pendingProposal"],
		"sessions":        sessions,
	}, nil
}

func (b *Bridge) ValidateConfig(ctx context.Context, yaml string) (map[string]any, error) {
	return b.client.ConfigService().Validate(ctx, yaml)
}

func (b *Bridge) PlanConfig(ctx context.Context, yaml string) (map[string]any, error) {
	return b.client.ConfigService().PlanYAML(ctx, b.ws(), yaml)
}

func (b *Bridge) ApplyConfig(ctx context.Context, yaml string, baseRevision int64, message string) (map[string]any, error) {
	return b.client.ConfigService().Apply(ctx, map[string]any{
		"projectId": b.proj(), "layer": "project",
		"yaml": yaml, "baseRevision": baseRevision,
		"message": message, "sessionId": b.SessionID(),
	})
}

func (b *Bridge) ResolveProposal(ctx context.Context, proposalID, action string) (map[string]any, error) {
	return b.client.ConfigService().ResolveProposal(ctx, b.proj(), proposalID, action)
}

func (b *Bridge) SearchLogs(ctx context.Context, query string, processIDs []string, regex, caseSensitive bool, stream, level string, contextLines, maxRows int) (map[string]any, error) {
	req := map[string]any{
		"workspaceId": b.ws(), "processIds": processIDs,
		"query": query, "regex": regex, "maxRows": maxRows,
	}
	if caseSensitive {
		req["caseSensitive"] = true
	}
	if stream != "" {
		req["streams"] = []string{stream}
	}
	if level != "" {
		req["minLevel"] = level
	}
	if contextLines > 0 {
		req["contextLines"] = contextLines
	}
	return b.client.LogService().Search(ctx, req)
}

func (b *Bridge) ListSessions(ctx context.Context) ([]map[string]any, error) {
	return b.client.SessionService().List(ctx, b.ws())
}
