package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/runtime"
	"jack-ketch/pkg/api"
)

const mcpClient = "mcp"

const maxStartWaitMS = 600000

func (h *handlers) waitAfterStart(ctx context.Context, req *mcp.CallToolRequest, rt runtime.Facade, res *api.StartResult, timeoutMS int) {
	eff, clamped := h.clampWait(timeoutMS)
	stop := h.progress(ctx, req, time.Duration(eff)*time.Millisecond, "waiting for "+res.ProcessID)
	defer stop()
	start := time.Now()
	wait := &api.StartWait{Mode: "ready"}
	lr, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Ready: true, TimeoutMS: eff})
	switch {
	case err != nil:
		wait.Mode = "exit"
		remaining := eff - int(time.Since(start).Milliseconds())
		if remaining < 1 {
			remaining = 1
		}
		if er, eerr := rt.WaitForExit(ctx, api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: remaining}); eerr == nil {
			wait.Exited, wait.Timeout, wait.ExitCode = er.Exited, er.Timeout, er.ExitCode
		}
	case lr.Matched:
		wait.Ready = true
		if lr.Entry != nil {
			wait.Line = lr.Entry.Line
		}
	default:
		wait.Exited, wait.Timeout = lr.Exited, lr.Timeout
		if lr.Exited {
			if er, eerr := rt.WaitForExit(ctx, api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 1000}); eerr == nil {
				wait.ExitCode = er.ExitCode
			}
		}
	}
	wait.WaitedMS = int(time.Since(start).Milliseconds())
	if wait.Timeout && clamped {
		wait.Clamped = true
		next := "wait_for_log(process_id, ready=true)"
		if wait.Mode == "exit" {
			next = "wait_for_exit(process_id)"
		}
		wait.Hint = fmt.Sprintf(clampHint, eff, next)
	}
	res.Wait = wait
	if st, serr := rt.Status(res.ProcessID); serr == nil {
		res.Status = st.Status
	}
}

type startIn struct {
	App       string   `json:"app,omitempty" jsonschema:"Name of an app from list_apps. Mutually exclusive with command."`
	Command   string   `json:"command,omitempty" jsonschema:"Executable to launch (npm, go, python, ./mvnw, ...). Mutually exclusive with app."`
	Args      []string `json:"args,omitempty" jsonschema:"Arguments for command."`
	WorkDir   string   `json:"workdir,omitempty" jsonschema:"Working directory, absolute or relative to the project root."`
	Env       []string `json:"env,omitempty" jsonschema:"Extra KEY=VALUE environment pairs."`
	EnvFile   string   `json:"env_file,omitempty" jsonschema:"Dotenv file applied to this start, relative to the project root."`
	Lifetime  string   `json:"lifetime,omitempty" jsonschema:"persistent (default, outlives this session) or session (stopped about 30 seconds after this session ends)."`
	TimeoutMS *int     `json:"timeout_ms,omitempty" jsonschema:"Omit to return immediately. When set, wait up to this many milliseconds for readiness (servers) or exit (one-shot commands); the wait is capped per call for this client, see wait.clamped."`
	Workspace string   `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type procIDIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
}

type getLogsIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Stream    string `json:"stream,omitempty" jsonschema:"all (default), stdout or stderr."`
	Lines     int    `json:"lines,omitempty" jsonschema:"Maximum lines (default 100, capped by the project config)."`
	Contains  string `json:"contains,omitempty" jsonschema:"Only lines containing this substring."`
	Pattern   string `json:"pattern,omitempty" jsonschema:"Only lines matching this regular expression."`
	Level     string `json:"level,omitempty" jsonschema:"Only lines at this level parsed from level=... or \"level\":...: debug, info, warn or error."`
}

type clearLogsIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Stream    string `json:"stream,omitempty" jsonschema:"Which buffer to clear: all (default), stdout or stderr."`
}

type sendStdinIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Data      string `json:"data" jsonschema:"Raw bytes to write to the process stdin."`
}

type waitForLogIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Contains  string `json:"contains,omitempty" jsonschema:"Wait for a line containing this substring. One of contains/pattern/ready is required."`
	Pattern   string `json:"pattern,omitempty" jsonschema:"Wait for a line matching this regular expression."`
	Ready     bool   `json:"ready,omitempty" jsonschema:"Wait for the app's readiness line, using the detected profile's readiness patterns."`
	TimeoutMS int    `json:"timeout_ms,omitempty" jsonschema:"Timeout in milliseconds (default 30000, max 600000)."`
}

type waitForExitIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	TimeoutMS int    `json:"timeout_ms,omitempty" jsonschema:"Timeout in milliseconds (default 30000, max 600000)."`
}

type removeProcessIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Force     bool   `json:"force,omitempty" jsonschema:"Stop the process first if it is still running, then remove it. Default false (running processes are refused)."`
}

type signalProcessIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Signal    string `json:"signal" jsonschema:"Signal name (SIG prefix optional): SIGINT, SIGTERM, SIGHUP, SIGQUIT, SIGUSR1, SIGUSR2 or SIGKILL. Delivered to the entire process group (mycli -> uv -> python), not just the direct child."`
}

type processEnvIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Live      bool   `json:"live,omitempty" jsonschema:"false (default): the env the runtime constructed (layers merged at start). true: read /proc/<pid>/environ — the ground-truth env, including anything uv/poetry/nvm injected after start. Unix only."`
	Reveal    bool   `json:"reveal,omitempty" jsonschema:"false (default): values of secret-like keys (secret/token/password/key patterns) are masked as ***. true: return raw values."`
}

type openShellIn struct {
	ProcessID string `json:"process_id,omitempty" jsonschema:"Clone the environment+workdir of this running process. Mutually exclusive with app and pid."`
	App       string `json:"app,omitempty" jsonschema:"Clone the resolved environment+workdir of this configured app from the project config. Mutually exclusive with process_id and pid."`
	PID       int    `json:"pid,omitempty" jsonschema:"OS process id: shell into any process's workdir+env via /proc/<pid> (Linux). Mutually exclusive with process_id and app."`
	Shell     string `json:"shell,omitempty" jsonschema:"Shell to launch (absolute path or name). Default $SHELL, then /bin/sh."`
	Workspace string `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type setRestartPolicyIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Policy    string `json:"policy" jsonschema:"Restart policy: never | on-failure | always. on-failure restarts only on non-zero exit; always restarts regardless. Manual stop_process never triggers a restart."`
}

type subscribeEventsIn struct {
	ProcessID string   `json:"process_id,omitempty" jsonschema:"Restrict delivery to a single process; empty subscribes to all processes."`
	Types     []string `json:"types,omitempty" jsonschema:"Restrict to these event types (process.started, process.exited, process.crashed, process.healthy, process.unhealthy, ...); empty subscribes to all."`
	Since     string   `json:"since,omitempty" jsonschema:"Backfill cursor: an event id to receive events after, \"last\" or empty to start fresh from now."`
	Workspace string   `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type getEventsIn struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"The subscription_id returned by subscribe_events."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum events to drain; empty or <=0 drains all buffered."`
}

type unsubscribeEventsIn struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"The subscription_id returned by subscribe_events."`
}

type getAuditLogIn struct {
	Lines     int    `json:"lines,omitempty" jsonschema:"Maximum audit lines to return (default 100)."`
	Contains  string `json:"contains,omitempty" jsonschema:"Only return lines containing this substring."`
	Workspace string `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type workspaceIn struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

func registerTools(server *mcp.Server, h *handlers) {
	addTool(server, h,
		&mcp.Tool{Name: "start_process", Title: "Start process", Annotations: mutating("Start process", false, false), Description: "Start a development process (dev server, watcher, worker, or an app-tied build/test/migration) under jack-ketch supervision. Prefer app=<name> from list_apps; otherwise pass command and args. Returns a process_id immediately; with timeout_ms it also waits until the app is ready (readiness line) or the command exits, capped per call for this client (see wait.clamped). Call list_processes first to avoid starting duplicates. Not for one-off shell commands (ls, cat, grep, git, docker, kubectl, package managers)."},
		func(ctx context.Context, req *mcp.CallToolRequest, in startIn) (*mcp.CallToolResult, *api.StartResult, error) {
			if in.TimeoutMS != nil && (*in.TimeoutMS < 0 || *in.TimeoutMS > maxStartWaitMS) {
				return nil, nil, fmt.Errorf("timeout_ms must be between 0 and %d", maxStartWaitMS)
			}
			rt, err := h.scoped(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.Start(ctx, api.StartRequest{
				App: in.App, Command: in.Command, Args: in.Args, WorkDir: in.WorkDir, Env: in.Env,
				EnvFile: in.EnvFile, Lifetime: in.Lifetime,
			})
			if err != nil {
				return nil, nil, err
			}
			if in.TimeoutMS != nil {
				h.waitAfterStart(ctx, req, rt, res, *in.TimeoutMS)
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "stop_process", Title: "Stop process", Annotations: mutating("Stop process", true, true), Description: "Stop a process: SIGTERM to its whole process group, then SIGKILL after the grace period. Stopping an already stopped process is a no-op."},
		func(ctx context.Context, req *mcp.CallToolRequest, in procIDIn) (*mcp.CallToolResult, map[string]string, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			if err := rt.Stop(ctx, in.ProcessID); err != nil {
				return nil, nil, err
			}
			return nil, map[string]string{"process_id": in.ProcessID, "status": "stopped"}, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "restart_process", Title: "Restart process", Annotations: mutating("Restart process", true, false), Description: "Restart a process with the same start specification after code or config changes. Keeps the process_id and its log history."},
		func(ctx context.Context, req *mcp.CallToolRequest, in procIDIn) (*mcp.CallToolResult, *api.StartResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			if err := rt.Restart(ctx, in.ProcessID); err != nil {
				return nil, nil, err
			}
			st, err := rt.Status(in.ProcessID)
			if err != nil {
				return nil, nil, err
			}
			return nil, &api.StartResult{
				ProcessID: st.ProcessID, InstanceID: st.InstanceID, Status: st.Status,
				Profile: st.Profile, Command: st.Command, Args: st.Args, WorkDir: st.WorkDir,
			}, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "process_status", Title: "Process status", Annotations: readOnly("Process status"), Description: "Lifecycle state of one process: status, pid, exit code, restarts, health (healthy/unhealthy) and restart policy."},
		func(ctx context.Context, req *mcp.CallToolRequest, in procIDIn) (*mcp.CallToolResult, *api.StatusResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.Status(in.ProcessID)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "list_processes", Title: "List processes", Annotations: readOnly("List processes"), Description: "List managed processes, including exited ones kept for a few minutes. Call this first to re-attach instead of starting duplicates. scope: workspace (default), project, or all."},
		func(ctx context.Context, req *mcp.CallToolRequest, in listProcessesIn) (*mcp.CallToolResult, *api.ListResult, error) {
			res, err := listScoped(ctx, h, req, in)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "get_logs", Title: "Get logs", Annotations: readOnly("Get logs"), Description: "Read captured stdout/stderr of a process. Bounded by lines (default 100); filter with stream (stdout or stderr), contains (substring), pattern (regex) and level (debug, info, warn, error). Start with stream=stderr when diagnosing. Do not poll in a loop; use wait_for_log to wait."},
		func(ctx context.Context, req *mcp.CallToolRequest, in getLogsIn) (*mcp.CallToolResult, *api.GetLogsResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.GetLogs(api.GetLogsRequest{
				ProcessID: in.ProcessID, Stream: in.Stream, Lines: in.Lines, Contains: in.Contains,
				Pattern: in.Pattern, Level: in.Level,
			})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "clear_logs", Title: "Clear logs", Annotations: mutating("Clear logs", true, true), Description: "Clear the captured log buffer(s) of a process."},
		func(ctx context.Context, req *mcp.CallToolRequest, in clearLogsIn) (*mcp.CallToolResult, *api.ClearLogsResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.ClearLogs(in.ProcessID, in.Stream)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "send_stdin", Title: "Send stdin", Annotations: mutating("Send stdin", true, false), Description: "Write raw text to a running process's stdin, for example \"y\\n\" or \"q\\n\"."},
		func(ctx context.Context, req *mcp.CallToolRequest, in sendStdinIn) (*mcp.CallToolResult, *api.SendStdinResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.SendStdin(in.ProcessID, in.Data)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "wait_for_log", Title: "Wait for log line", Annotations: readOnly("Wait for log line"), Description: "Block until a log line matches (contains, pattern, or the app's readiness line with ready=true), the process exits, or the timeout elapses. Matches existing and future lines. Each call is capped for this client; if the result has clamped=true and still_running=true, call it again."},
		func(ctx context.Context, req *mcp.CallToolRequest, in waitForLogIn) (*mcp.CallToolResult, *api.WaitForLogResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			eff, clamped := h.clampWait(in.TimeoutMS)
			stop := h.progress(ctx, req, time.Duration(eff)*time.Millisecond, "waiting for a log line from "+in.ProcessID)
			start := time.Now()
			res, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: in.ProcessID, Contains: in.Contains, Pattern: in.Pattern, Ready: in.Ready, TimeoutMS: eff})
			stop()
			if err != nil {
				return nil, nil, err
			}
			res.WaitedMS = int(time.Since(start).Milliseconds())
			res.StillRunning = res.Timeout && !res.Exited
			if res.Timeout && clamped {
				res.Clamped = true
				res.Hint = fmt.Sprintf(clampHint, eff, "wait_for_log")
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "wait_for_exit", Title: "Wait for exit", Annotations: readOnly("Wait for exit"), Description: "Block until a process exits and return its exit code. Each call is capped for this client; call again when clamped=true."},
		func(ctx context.Context, req *mcp.CallToolRequest, in waitForExitIn) (*mcp.CallToolResult, *api.WaitForExitResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			eff, clamped := h.clampWait(in.TimeoutMS)
			stop := h.progress(ctx, req, time.Duration(eff)*time.Millisecond, "waiting for "+in.ProcessID+" to exit")
			start := time.Now()
			res, err := rt.WaitForExit(ctx, api.WaitForExitParams{ProcessID: in.ProcessID, TimeoutMS: eff})
			stop()
			if err != nil {
				return nil, nil, err
			}
			res.WaitedMS = int(time.Since(start).Milliseconds())
			res.StillRunning = res.Timeout
			if res.Timeout && clamped {
				res.Clamped = true
				res.Hint = fmt.Sprintf(clampHint, eff, "wait_for_exit")
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "list_apps", Title: "List apps", Annotations: readOnly("List apps"), Description: "List apps declared in the project config with command, workdir, type and whether they were auto-generated. Prefer start_process(app=...)."},
		func(ctx context.Context, req *mcp.CallToolRequest, in workspaceIn) (*mcp.CallToolResult, *api.ListAppsResult, error) {
			rt, err := h.scoped(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.Apps()
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "remove_process", Title: "Remove process", Annotations: mutating("Remove process", true, false), Description: "Forget a terminated process and free its log buffers. A running process is refused unless force=true, which stops it first."},
		func(ctx context.Context, req *mcp.CallToolRequest, in removeProcessIn) (*mcp.CallToolResult, *api.RemoveProcessResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.RemoveProcess(in.ProcessID, in.Force)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "signal_process", Title: "Signal process", Annotations: mutating("Signal process", true, false), Description: "Send one signal (SIGINT, SIGTERM, SIGHUP, SIGQUIT, SIGUSR1, SIGUSR2 or SIGKILL) to a process's whole process group, for example SIGHUP to reload config."},
		func(ctx context.Context, req *mcp.CallToolRequest, in signalProcessIn) (*mcp.CallToolResult, *api.SignalProcessResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.SignalProcess(ctx, api.SignalProcessRequest{ProcessID: in.ProcessID, Signal: in.Signal})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "get_process_env", Title: "Process environment", Annotations: readOnly("Process environment"), Description: "Show the environment a process runs with: the layers merged at start, or live=true to read /proc on Linux. Secret-like values are masked unless reveal=true."},
		func(ctx context.Context, req *mcp.CallToolRequest, in processEnvIn) (*mcp.CallToolResult, *api.ProcessEnvResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: in.ProcessID, Live: in.Live, Reveal: in.Reveal})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "open_shell", Title: "Open shell", Annotations: mutating("Open shell", false, false), Description: "Start an interactive shell in the environment and working directory of a process (process_id), a configured app (app) or an OS process (pid, Linux). Drive it with send_stdin and read it with get_logs; it stops automatically after this session ends. Use it to run commands with the app's toolchain, not for general shell work."},
		func(ctx context.Context, req *mcp.CallToolRequest, in openShellIn) (*mcp.CallToolResult, *api.StartResult, error) {
			var rt runtime.Facade
			var err error
			if in.ProcessID != "" {
				rt, err = h.facade(ctx)
			} else {
				rt, err = h.scoped(ctx, req, in.Workspace)
			}
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.OpenShell(ctx, api.OpenShellRequest{ProcessID: in.ProcessID, App: in.App, PID: in.PID, Shell: in.Shell})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "set_restart_policy", Title: "Set restart policy", Annotations: mutating("Set restart policy", false, true), Description: "Set auto-restart for a process: never, on-failure or always. A manual stop_process never triggers a restart."},
		func(ctx context.Context, req *mcp.CallToolRequest, in setRestartPolicyIn) (*mcp.CallToolResult, *api.SetRestartPolicyResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.SetRestartPolicy(api.SetRestartPolicyRequest{ProcessID: in.ProcessID, Policy: in.Policy})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "subscribe_events", Title: "Subscribe to events", Annotations: readOnly("Subscribe to events"), Description: "Open a pull subscription to lifecycle events (process.started, process.exited, process.crashed, process.healthy, process.unhealthy, ...) filtered by process_id and types. Drain with get_events. Limits: 8 subscriptions per session, 256 buffered events each."},
		func(ctx context.Context, req *mcp.CallToolRequest, in subscribeEventsIn) (*mcp.CallToolResult, *api.SubscribeEventsResult, error) {
			var rt runtime.Facade
			var err error
			if in.ProcessID == "" {
				rt, err = h.scoped(ctx, req, in.Workspace)
			} else {
				rt, err = h.facade(ctx)
			}
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.SubscribeEvents(mcpClient, api.SubscribeEventsRequest{ProcessID: in.ProcessID, Types: in.Types, Since: in.Since})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "get_events", Title: "Get events", Annotations: readOnly("Get events"), Description: "Drain buffered events from a subscription without blocking; dropped counts events lost to buffer overflow."},
		func(ctx context.Context, req *mcp.CallToolRequest, in getEventsIn) (*mcp.CallToolResult, *api.GetEventsResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.GetEvents(api.GetEventsRequest{SubscriptionID: in.SubscriptionID, Limit: in.Limit})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "unsubscribe_events", Title: "Unsubscribe events", Annotations: readOnly("Unsubscribe events"), Description: "Close an event subscription."},
		func(ctx context.Context, req *mcp.CallToolRequest, in unsubscribeEventsIn) (*mcp.CallToolResult, *api.UnsubscribeEventsResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.UnsubscribeEvents(mcpClient, in.SubscriptionID)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "runtime_stats", Title: "Runtime stats", Annotations: readOnly("Runtime stats"), Description: "Health of the runtime for this project: process counts by status, log pipeline drops and uptime."},
		func(ctx context.Context, req *mcp.CallToolRequest, in workspaceIn) (*mcp.CallToolResult, *api.RuntimeStats, error) {
			rt, err := h.scoped(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.RuntimeStats()
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "get_audit_log", Title: "Audit log", Annotations: readOnly("Audit log"), Description: "Recent mutating calls recorded by jack-ketch for this project, with secrets redacted."},
		func(ctx context.Context, req *mcp.CallToolRequest, in getAuditLogIn) (*mcp.CallToolResult, *api.AuditLogResult, error) {
			rt, err := h.scoped(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			res, err := rt.GetAuditLog(in.Lines, in.Contains)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})
}
