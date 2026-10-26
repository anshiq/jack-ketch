// Package client is the public Go SDK for jack-ketch v3.
// It provides EnsureDaemon, typed service clients, stream helpers
// with auto-resume, and version handshake. All CLI, TUI, and
// bridge code uses this package.
//
// Transport: HTTP/JSON over the daemon UDS at Connect-compatible
// paths (/jackketch.v1.X/Y). The client header
// X-Jack-Ketch-Client: <kind>/<version> is sent on every call (§5.2).
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"jack-ketch/pkg/api"
)

const (
	// DefaultAPIVersion is the current API version.
	DefaultAPIVersion = "v1"
	// MinClientVersion is the minimum client version accepted.
	MinClientVersion = "v1"
	// ShimProtocolVersion is the current shim protocol version.
	ShimProtocolVersion = 1
	// ClientHeader identifies the caller per §5.2.
	ClientHeader = "X-Jack-Ketch-Client"
	// SessionHeader identifies the daemon session this client attached to.
	SessionHeader = "X-Jack-Ketch-Session"
	// HarnessHeader identifies the agent harness this client speaks for.
	HarnessHeader = "X-Jack-Ketch-Harness"
)

const defaultCallTimeout = 30 * time.Second

// Client is a client of the jack-ketch daemon.
type Client struct {
	socket     string
	httpClient *http.Client
	version    string
	mu         sync.RWMutex
	agent      string
	headers    map[string]string
	onDialErr  func() error
	recovering atomic.Bool
}

// NewTCP creates a client for the authenticated loopback TCP listener
// (web UI, remote tooling over SSH-forwarded ports).
func NewTCP(addr, token, agent string) *Client {
	if agent == "" {
		agent = "web/unknown"
	}
	c := &Client{
		httpClient: &http.Client{
			Transport: &tcpTransport{addr: addr, token: token},
		},
		agent:   agent,
		version: DefaultAPIVersion,
	}
	c.socket = "tcp://" + addr
	return c
}

// tcpTransport rewrites jackketchd URLs to the TCP listener with a bearer token.
type tcpTransport struct {
	addr  string
	token string
}

func (t *tcpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.addr
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

// New creates a client for a running daemon socket.
func New(socketPath, agent string) *Client {
	if agent == "" {
		agent = "cli/unknown"
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 2 * time.Second}
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		socket:     socketPath,
		httpClient: &http.Client{Transport: transport},
		agent:      agent,
		version:    DefaultAPIVersion,
	}
}

// EnsureDaemon ensures a daemon is running. If no daemon answers on the
// socket, it returns an error telling the user to start jackketchd (spawn is
// owned by internal/daemon to keep the flock in one place).
func EnsureDaemon(socketPath string) (*Client, error) {
	return EnsureDaemonAs(socketPath, "cli/ensure")
}

// EnsureDaemonAs is EnsureDaemon with an explicit client identity.
func EnsureDaemonAs(socketPath, agent string) (*Client, error) {
	c := New(socketPath, agent)
	if err := c.ping(); err != nil {
		return nil, fmt.Errorf("jackketchd not reachable at %s: %w (run `jack-ketch daemon start` or `jackketchd --foreground`)", socketPath, err)
	}
	return c, nil
}

// EnsureDaemonOrSpawn tries to dial; on refusal it attempts a spawn via
// the daemon package callback to avoid an import cycle. Pass nil to
// disable spawning.
func EnsureDaemonOrSpawn(socketPath string, spawn func() error) (*Client, error) {
	if c, err := EnsureDaemon(socketPath); err == nil {
		return c, nil
	} else if spawn == nil {
		return nil, err
	}
	if err := spawn(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := EnsureDaemon(socketPath); err == nil {
			return c, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("jackketchd did not become ready at %s", socketPath)
}

func (c *Client) ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.GetVersion(ctx)
	return err
}

func (c *Client) call(ctx context.Context, service, method string, req, resp any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultCallTimeout)
		defer cancel()
	}
	var data []byte
	if req != nil {
		var err error
		if data, err = json.Marshal(req); err != nil {
			return err
		}
	}
	httpResp, err := c.do(ctx, service, method, data)
	if err != nil && isDialError(err) && c.tryRecover() {
		httpResp, err = c.do(ctx, service, method, data)
	}
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4*1024))
		return fmt.Errorf("jackketchd %s/%s: %s: %s", service, method, httpResp.Status, string(body))
	}
	if resp == nil {
		return nil
	}
	return json.NewDecoder(httpResp.Body).Decode(resp)
}

func (c *Client) do(ctx context.Context, service, method string, data []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", "http://jackketchd/jackketch.v1."+service+"/"+method, bytesReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(ClientHeader, c.agentName())
	c.applyHeaders(httpReq)
	return c.httpClient.Do(httpReq)
}

// SetAgent sets the client identity sent in X-Jack-Ketch-Client.
func (c *Client) SetAgent(agent string) {
	c.mu.Lock()
	c.agent = agent
	c.mu.Unlock()
}

func (c *Client) agentName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.agent
}

// SetHeader sets an extra header sent on every call; an empty value removes it.
func (c *Client) SetHeader(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.headers == nil {
		c.headers = map[string]string{}
	}
	if value == "" {
		delete(c.headers, key)
		return
	}
	c.headers[key] = value
}

func (c *Client) applyHeaders(r *http.Request) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k, v := range c.headers {
		r.Header.Set(k, v)
	}
}

// OnDialError registers a recovery hook invoked once when a dial fails.
func (c *Client) OnDialError(fn func() error) {
	c.mu.Lock()
	c.onDialErr = fn
	c.mu.Unlock()
}

func (c *Client) tryRecover() bool {
	c.mu.RLock()
	fn := c.onDialErr
	c.mu.RUnlock()
	if fn == nil || !c.recovering.CompareAndSwap(false, true) {
		return false
	}
	defer c.recovering.Store(false)
	return fn() == nil
}

func isDialError(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT)
}

// Call invokes a daemon RPC by service/method with a JSON body. It is
// the escape hatch for callers (like the MCP bridge) that need endpoints
// without a dedicated typed method yet.
func (c *Client) Call(ctx context.Context, service, method string, req, resp any) error {
	return c.call(ctx, service, method, req, resp)
}

// SocketPath returns the daemon socket this client dials.
func (c *Client) SocketPath() string { return c.socket }

// GetVersion returns the daemon version information.
func (c *Client) GetVersion(ctx context.Context) (*VersionResponse, error) {
	var out VersionResponse
	if err := c.call(ctx, "SystemService", "GetVersion", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SystemService provides access to the SystemService.
func (c *Client) SystemService() *SystemServiceClient {
	return &SystemServiceClient{c}
}

// ProjectService provides access to the ProjectService.
func (c *Client) ProjectService() *ProjectServiceClient {
	return &ProjectServiceClient{c}
}

// ProcessService provides access to the ProcessService.
func (c *Client) ProcessService() *ProcessServiceClient {
	return &ProcessServiceClient{c}
}

// ConfigService provides access to the ConfigService.
func (c *Client) ConfigService() *ConfigServiceClient {
	return &ConfigServiceClient{c}
}

// LogService provides access to the LogService.
func (c *Client) LogService() *LogServiceClient {
	return &LogServiceClient{c}
}

// EventService provides access to the EventService.
func (c *Client) EventService() *EventServiceClient {
	return &EventServiceClient{c}
}

// SessionService provides access to the SessionService.
func (c *Client) SessionService() *SessionServiceClient {
	return &SessionServiceClient{c}
}

// VersionResponse is the response from GetVersion.
type VersionResponse struct {
	DaemonVersion    string `json:"daemonVersion"`
	APIVersion       string `json:"apiVersion"`
	MinClientVersion string `json:"minClientVersion"`
	ShimProtocol     int32  `json:"shimProtocol"`
}

// SystemServiceClient is a typed client for SystemService.
type SystemServiceClient struct {
	client *Client
}

func (c *SystemServiceClient) GetVersion(ctx context.Context) (*VersionResponse, error) {
	return c.client.GetVersion(ctx)
}

func (c *SystemServiceClient) Health(ctx context.Context) (*HealthResponse, error) {
	var out HealthResponse
	if err := c.client.call(ctx, "SystemService", "Health", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *SystemServiceClient) GetStats(ctx context.Context) (*StatsResponse, error) {
	var out StatsResponse
	if err := c.client.call(ctx, "SystemService", "GetStats", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *SystemServiceClient) Shutdown(ctx context.Context, keepProcesses bool) error {
	return c.client.call(ctx, "SystemService", "Shutdown",
		map[string]any{"keepProcesses": keepProcesses}, nil)
}

// RuntimeStats returns runtime health counters for a workspace.
func (c *SystemServiceClient) RuntimeStats(ctx context.Context, workspaceID string) (*api.RuntimeStats, error) {
	var out api.RuntimeStats
	if err := c.client.call(ctx, "SystemService", "GetRuntimeStats", map[string]any{"workspaceId": workspaceID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EnableTCP starts (or reuses) the authenticated TCP listener.
func (c *SystemServiceClient) EnableTCP(ctx context.Context, addr string) (string, bool, error) {
	var out struct {
		Addr    string `json:"addr"`
		Started bool   `json:"started"`
	}
	err := c.client.call(ctx, "SystemService", "EnableTCP", map[string]any{"addr": addr}, &out)
	return out.Addr, out.Started, err
}

// HealthResponse is the response from Health.
type HealthResponse struct {
	Healthy  bool   `json:"healthy"`
	Status   string `json:"status"`
	UptimeMs int64  `json:"uptimeMs"`
}

// StatsResponse is the response from GetStats.
type StatsResponse struct {
	Projects        int32 `json:"projects"`
	Processes       int32 `json:"processes"`
	Sessions        int32 `json:"sessions"`
	LogDiskBytes    int64 `json:"logDiskBytes"`
	IndexLagSeconds int64 `json:"indexLagSeconds"`
}

// ProjectServiceClient is a typed client for ProjectService.
type ProjectServiceClient struct {
	client *Client
}

// Resolve resolves a working directory to a project and workspace.
func (c *ProjectServiceClient) Resolve(ctx context.Context, path string) (*ResolveResponse, error) {
	if path == "" {
		if cwd, err := os.Getwd(); err == nil {
			path = cwd
		}
	}
	var out ResolveResponse
	if err := c.client.call(ctx, "ProjectService", "Resolve", map[string]any{"path": path}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListProjects lists all projects.
func (c *ProjectServiceClient) ListProjects(ctx context.Context) ([]*ProjectInfo, error) {
	var out struct {
		Projects []*ProjectInfo `json:"projects"`
	}
	if err := c.client.call(ctx, "ProjectService", "ListProjects", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

// ResolveResponse is the response from Resolve.
type ResolveResponse struct {
	ProjectID          string   `json:"projectId"`
	WorkspaceID        string   `json:"workspaceId"`
	ProjectName        string   `json:"projectName"`
	NewlyCreated       bool     `json:"newlyCreated"`
	UnresolvedLocators []string `json:"unresolvedLocators"`
}

// ProjectInfo is a project summary.
type ProjectInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ConfigMode     string `json:"configMode"`
	LastUsedAt     int64  `json:"lastUsedAt"`
	WorkspaceCount int    `json:"workspaceCount"`
}

func (c *ProjectServiceClient) ListWorkspaces(ctx context.Context, projectID string) ([]*WorkspaceInfo, error) {
	var out struct {
		Workspaces []*WorkspaceInfo `json:"workspaces"`
	}
	if err := c.client.call(ctx, "ProjectService", "ListWorkspaces", map[string]any{"projectId": projectID}, &out); err != nil {
		return nil, err
	}
	return out.Workspaces, nil
}

type WorkspaceInfo struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// ProcessServiceClient is a typed client for ProcessService.
type ProcessServiceClient struct {
	client *Client
}

// Start starts a process.
func (c *ProcessServiceClient) Start(ctx context.Context, req *StartRequest) (*StartResponse, error) {
	var out StartResponse
	if err := c.client.call(ctx, "ProcessService", "Start", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StartRequest is a request to start a process.
type StartRequest struct {
	WorkspaceID string            `json:"workspaceId"`
	App         string            `json:"app"`
	Mode        string            `json:"mode,omitempty"`
	Command     []string          `json:"command"`
	WorkDir     string            `json:"workdir"`
	Env         map[string]string `json:"env"`
	EnvFile     string            `json:"envFile,omitempty"`
	Lifetime    string            `json:"lifetime"`
	SessionID   string            `json:"sessionId,omitempty"`
}

// StartResponse is the response from Start.
type StartResponse struct {
	ProcessID  string `json:"processId"`
	InstanceID string `json:"instanceId"`
	Status     string `json:"status"`
	PID        int32  `json:"pid"`
}

// OpenShellRequest is the input to ProcessService/OpenShell.
type OpenShellRequest struct {
	WorkspaceID string `json:"workspaceId,omitempty"`
	ProcessID   string `json:"processId,omitempty"`
	App         string `json:"app,omitempty"`
	PID         int    `json:"pid,omitempty"`
	Shell       string `json:"shell,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	Lifetime    string `json:"lifetime,omitempty"`
}

// OpenShell starts an interactive shell inside a process's or app's environment.
func (c *ProcessServiceClient) OpenShell(ctx context.Context, req OpenShellRequest) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ProcessService", "OpenShell", req, &out)
	return out, err
}

// ConfigServiceClient is a typed client for ConfigService.
type ConfigServiceClient struct {
	client *Client
}

// Plan returns a config diff before applying.
func (c *ConfigServiceClient) Plan(ctx context.Context, workspaceID string) (*PlanResponse, error) {
	var out PlanResponse
	if err := c.client.call(ctx, "ConfigService", "Plan", map[string]any{"workspaceId": workspaceID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PlanResponse is the response from Plan.
type PlanResponse struct {
	RestartApps []string `json:"restartApps"`
	LiveApps    []string `json:"liveApps"`
	StaleApps   []string `json:"staleApps"`
}

// LogServiceClient is a typed client for LogService.
type LogServiceClient struct {
	client *Client
}

// Query fetches filtered log entries.
func (c *LogServiceClient) Query(ctx context.Context, req api.GetLogsRequest) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "LogService", "GetLogs", req, &out)
	return out, err
}

// EventServiceClient is a typed client for EventService.
type EventServiceClient struct {
	client *Client
}

// SubscribeRequest is the input to EventService/Subscribe.
type SubscribeRequest struct {
	SessionID   string   `json:"sessionId"`
	WorkspaceID string   `json:"workspaceId"`
	ProcessID   string   `json:"processId,omitempty"`
	Types       []string `json:"types,omitempty"`
	Since       string   `json:"since,omitempty"`
}

// Subscribe opens a pull subscription.
func (c *EventServiceClient) Subscribe(ctx context.Context, req SubscribeRequest) (string, error) {
	var out struct {
		SubscriptionID string `json:"subscriptionId"`
	}
	if err := c.client.call(ctx, "EventService", "Subscribe", req, &out); err != nil {
		return "", err
	}
	return out.SubscriptionID, nil
}

// Drain pulls buffered events from a subscription.
func (c *EventServiceClient) Drain(ctx context.Context, id string, limit int) (*api.GetEventsResult, error) {
	var out api.GetEventsResult
	if err := c.client.call(ctx, "EventService", "Drain",
		map[string]any{"subscriptionId": id, "limit": limit}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Unsubscribe closes a subscription.
func (c *EventServiceClient) Unsubscribe(ctx context.Context, id string) error {
	return c.client.call(ctx, "EventService", "Unsubscribe", map[string]any{"subscriptionId": id}, nil)
}

// SessionServiceClient is a typed client for SessionService.
type SessionServiceClient struct {
	client *Client
}

// RegisterRequest is the input to SessionService/Register.
type RegisterRequest struct {
	Kind           string `json:"kind"`
	Harness        string `json:"harness"`
	HarnessVersion string `json:"harnessVersion,omitempty"`
	ClientName     string `json:"clientName,omitempty"`
	ClientPID      int    `json:"clientPid"`
	WorkspaceID    string `json:"workspaceId"`
}

// Register registers a new session.
func (c *SessionServiceClient) Register(ctx context.Context, kind, harness string, pid int, workspaceID string) (*RegisterResponse, error) {
	return c.RegisterWith(ctx, RegisterRequest{Kind: kind, Harness: harness, ClientPID: pid, WorkspaceID: workspaceID})
}

// RegisterWith registers a new session with the full request.
func (c *SessionServiceClient) RegisterWith(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	var out RegisterResponse
	if err := c.client.call(ctx, "SessionService", "Register", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SessionUpdate is the input to SessionService/Update.
type SessionUpdate struct {
	SessionID      string `json:"sessionId"`
	Harness        string `json:"harness,omitempty"`
	HarnessVersion string `json:"harnessVersion,omitempty"`
	ClientName     string `json:"clientName,omitempty"`
	WorkspaceID    string `json:"workspaceId,omitempty"`
}

// Update updates a registered session.
func (c *SessionServiceClient) Update(ctx context.Context, req SessionUpdate) error {
	return c.client.call(ctx, "SessionService", "Update", req, nil)
}

// RegisterResponse is the response from Register.
type RegisterResponse struct {
	SessionID string `json:"sessionId"`
}
