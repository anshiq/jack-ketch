package mcp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/runtime"
)

// Version is the MCP server version reported during initialization. It is a
// var so release builds can stamp the git tag via -ldflags
// (-X jack-ketch/internal/mcp.Version={{.Version}}); local builds keep the
// default.
var Version = "0.2.1"

const Instructions = `jack-ketch runs and supervises this project's development processes: dev servers, watchers, workers, and app-tied builds, tests and migrations. Start them through jack-ketch instead of your own shell; a process started elsewhere has no captured logs, readiness detection, health checks or restart supervision.

Workflow:
1. Call list_processes first. Processes outlive your session, so re-attach to a running instance instead of starting a duplicate on the same port.
2. Call list_apps. Each app can have several modes (dev, prod, headless, test, staging...) with their own command and env. Prefer start_process(app=..., mode=...) over rebuilding a command line; fall back to start_process(command=..., args=[...]). Keep the returned process_id; every other tool uses it.
3. Confirm readiness with wait_for_log(process_id, ready=true) or start_process(..., timeout_ms=60000). Waits are capped per call for your client: when a result has clamped=true and still_running=true, call the wait tool again.
4. Diagnose with process_status, get_logs(process_id, stream="stderr", lines=100) narrowed with contains, pattern or level, and search_logs across the project. Never poll get_logs in a loop.
5. After code or config changes use restart_process (same process_id). Stop with stop_process; remove_process frees buffers. Exited processes are kept for a few minutes.

Saving runs: when a result carries save_suggestion, the run worked and is not saved yet. Ask the user its ask question and show its yaml; never save without a yes. Yes: save_run_config(process_id, app, mode) with any names or scope the user picks. No: save_run_config(process_id, action="dismiss"). Env is saved as ordered env_sources (env files and inline vars; later entries win).

Failures: when a result carries diagnosis (or after diagnose_process), follow its guidance. actor=user: do not guess values or edit secrets; give the user the steps and missing_env (key, file to add it to, hint, example) and wait for them. actor=agent_with_approval: ask permission first, showing the exact fixes commands (installs, containers, migrations); run approved installs in your own shell, then restart_process and wait again. actor=agent: fix it yourself. Once it runs, offer to save the working setup (save_suggestion).

Runtime verification: after implementing a fix or feature and its tests pass, ask the user whether they want to see it working at runtime. If yes, start or restart the affected app, wait until ready, then exercise the changed behaviour: probe for HTTP endpoints (returns the response plus the logs the request produced; non-GET or remote targets need the user's OK), start_process one-shots for CLIs and scripts, wait_for_log for workers and jobs. Show the user the evidence. Offer to save useful probes as checks (save_run_config checks=[probe result check]) so run_checks can re-verify in one call.

Git hygiene: when get_project_info or save_run_config returns gitignore.missing, ask the user whether to add those paths to .gitignore. Yes: update_gitignore. No: update_gitignore(action="dismiss").

Projects: tools act on this session's project. If no project is bound (for example a desktop app without a working folder), pass workspace="/absolute/path" or call set_workspace once.

Config: apps live in the jack-ketch database, not in repository files. Read get_project_info, then validate_config, plan_config and apply_config(base_revision=...). Never approve a pending config proposal without the user.

Events are pulled, never pushed: subscribe_events, then get_events. Supervised apps restart on their own according to their restart policy.

Do not run one-off shell commands through jack-ketch (ls, cat, grep, find, git, docker, kubectl, package managers); use your own tools for those.`

type Options struct {
	Facade          runtime.Facade
	Backend         func(ctx context.Context) (runtime.Facade, error)
	Mode            func() string
	Logger          *slog.Logger
	Harness         string
	HarnessExplicit bool
	MaxWaitMS       int
	Toolset         string
	ProgressEvery   time.Duration
}

func NewServer(rt runtime.Facade, logger *slog.Logger) *mcp.Server {
	return NewServerWith(Options{Facade: rt, Logger: logger})
}

func NewServerWith(o Options) *mcp.Server {
	logger := o.Logger
	if logger == nil {
		logger = slog.Default()
	}
	h := &handlers{logger: logger, toolset: toolsetFor(o.Toolset), progEvery: o.ProgressEvery, mode: o.Mode}
	if h.progEvery == 0 {
		h.progEvery = 5 * time.Second
	}
	if o.Backend != nil {
		h.backend = o.Backend
	} else {
		rt := o.Facade
		h.backend = func(context.Context) (runtime.Facade, error) { return rt, nil }
	}
	if h.mode == nil {
		h.mode = func() string {
			if _, ok := o.Facade.(*Bridge); ok {
				return "daemon"
			}
			return "embedded"
		}
	}
	h.limits = newLimitState(o.Harness, o.HarnessExplicit, o.MaxWaitMS)
	server := mcp.NewServer(
		&mcp.Implementation{Name: "jack-ketch", Title: "jack-ketch", Version: Version, WebsiteURL: "https://github.com/anshiq/jack-ketch"},
		&mcp.ServerOptions{Instructions: Instructions, Capabilities: &mcp.ServerCapabilities{}, CompletionHandler: h.complete},
	)
	registerTools(server, h)
	registerV3Tools(server, h)
	registerAssistTools(server, h)
	registerPrompts(server, h)
	registerResources(server, h)
	return server
}

type handlers struct {
	backend    func(ctx context.Context) (runtime.Facade, error)
	mode       func() string
	logger     *slog.Logger
	toolset    map[string]bool
	limits     *limitState
	progEvery  time.Duration
	clientMu   sync.Mutex
	clientSeen bool
}

func (h *handlers) facade(ctx context.Context) (runtime.Facade, error) { return h.backend(ctx) }

func (h *handlers) log(tool string, args any) {
	h.logger.Debug("tool call", "tool", tool, "args", args)
}
