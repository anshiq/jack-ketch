package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/runtime"
	"jack-ketch/pkg/api"
	"jack-ketch/pkg/client"
)

type verifyChangeIn struct {
	ProcessID    string `json:"process_id" jsonschema:"The running process that should pick up your change."`
	Restart      *bool  `json:"restart,omitempty" jsonschema:"Restart the process first (default true). Set false for a dev server that hot-reloads on its own; then set wait_contains or wait_pattern to the line its rebuild prints."`
	WaitContains string `json:"wait_contains,omitempty" jsonschema:"Line that means the change is live (for example \"compiled successfully\"). Default: the app's readiness line after a restart."`
	WaitPattern  string `json:"wait_pattern,omitempty" jsonschema:"Regular expression alternative to wait_contains."`
	FailPattern  string `json:"fail_pattern,omitempty" jsonschema:"Regular expression for a line that means the rebuild or start failed (for example \"(?i)error|failed to compile\"). Stops the wait early."`
	TimeoutMS    int    `json:"timeout_ms,omitempty" jsonschema:"How long to wait for the change to go live (default 60000)."`
	SettleMS     int    `json:"settle_ms,omitempty" jsonschema:"After a restart of a process without a readiness line, how long it must stay up to count as started (default 2000)."`
	Checks       *bool  `json:"checks,omitempty" jsonschema:"Run the app's saved checks afterwards (default true)."`
}

type verifyStep struct {
	Step   string `json:"step"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type verifyChangeOut struct {
	Verdict    string                `json:"verdict"`
	Summary    string                `json:"summary"`
	ProcessID  string                `json:"process_id"`
	InstanceID string                `json:"instance_id,omitempty"`
	Status     string                `json:"status,omitempty"`
	Steps      []verifyStep          `json:"steps"`
	Wait       *api.WaitForLogResult `json:"wait,omitempty"`
	Checks     *api.RunChecksResult  `json:"checks,omitempty"`
	NewErrors  []api.ErrorGroup      `json:"new_errors"`
	OldErrors  int                   `json:"still_failing_errors,omitempty"`
	Diagnosis  *api.Diagnosis        `json:"diagnosis,omitempty"`
	Cursor     string                `json:"cursor,omitempty"`
	Baseline   string                `json:"baseline"`
	Hint       string                `json:"hint,omitempty"`
}

type browserConsoleIn struct {
	ProcessID  string `json:"process_id,omitempty" jsonschema:"The dev server process (start_process) whose pages the browser will load. Required for action=start."`
	Action     string `json:"action,omitempty" jsonschema:"start (default), list or stop."`
	ID         string `json:"id,omitempty" jsonschema:"Proxy id for action=stop."`
	URL        string `json:"url,omitempty" jsonschema:"Dev server URL on this machine. Default: the port the process listens on."`
	Port       int    `json:"port,omitempty" jsonschema:"Port to use when the process listens on several."`
	Warnings   bool   `json:"warnings,omitempty" jsonschema:"Also capture console.warn (default: errors only)."`
	TTLMinutes int    `json:"ttl_minutes,omitempty" jsonschema:"How long the proxy stays open (default 60, max 720)."`
}

type browserConsoleOut struct {
	Proxy   *client.BrowserProxy  `json:"proxy,omitempty"`
	Proxies []client.BrowserProxy `json:"proxies,omitempty"`
	Stopped bool                  `json:"stopped,omitempty"`
	Hint    string                `json:"hint,omitempty"`
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func registerVerifyTools(server *mcp.Server, h *handlers) {
	addTool(server, h,
		&mcp.Tool{Name: "browser_console", Title: "Browser console capture", Annotations: mutating("Browser console capture", false, true), Description: "Make frontend errors visible to you. Opens a local proxy (127.0.0.1:<port>) in front of a dev server; pages loaded through it report console.error, uncaught exceptions, unhandled promise rejections and failed (5xx or network) requests into that process's logs as \"[browser] error: ...\" lines with source file and line. Hot-module reload and WebSockets keep working. Give the user the proxy URL (or open it in Playwright), then read the errors with get_errors, wait_for_log or verify_change like any other process output. action=list shows open proxies, action=stop closes one."},
		func(ctx context.Context, req *mcp.CallToolRequest, in browserConsoleIn) (*mcp.CallToolResult, *browserConsoleOut, error) {
			b, err := h.bridge(ctx, req, "")
			if err != nil {
				return nil, nil, err
			}
			switch strings.ToLower(in.Action) {
			case "list":
				ps, err := b.ListBrowserProxies(ctx)
				if err != nil {
					return nil, nil, err
				}
				return nil, &browserConsoleOut{Proxies: ps}, nil
			case "stop":
				if in.ID == "" {
					return nil, nil, fmt.Errorf("action=stop needs id")
				}
				if err := b.StopBrowserProxy(ctx, in.ID); err != nil {
					return nil, nil, err
				}
				return nil, &browserConsoleOut{Stopped: true}, nil
			case "", "start":
			default:
				return nil, nil, fmt.Errorf("unknown action %q (start, list or stop)", in.Action)
			}
			if in.ProcessID == "" {
				return nil, nil, fmt.Errorf("process_id is required")
			}
			ttl := in.TTLMinutes
			if ttl > 720 {
				ttl = 720
			}
			px, err := b.StartBrowserProxy(ctx, client.BrowserProxyRequest{ProcessID: in.ProcessID, URL: in.URL, Port: in.Port, TTLMinutes: ttl, Warnings: in.Warnings})
			if err != nil {
				return nil, nil, err
			}
			return nil, &browserConsoleOut{Proxy: px, Hint: "Open " + px.URL + " instead of the dev server URL. Browser errors then appear in get_errors(process_id) as kind=browser_error. If the app builds absolute links to its own port, those pages bypass the proxy."}, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "verify_change", Title: "Verify change", Annotations: mutating("Verify change", false, false), Description: "After editing code, confirm the change works in one call: restarts the process (or, with restart=false, waits for a hot-reloading dev server to rebuild), waits until it is live while watching for fail lines, runs the app's saved checks, and reports only the errors that appeared because of this change (new_errors, grouped with file and line). verdict is pass or fail with a one-line summary; on fail it includes the diagnosis. Use it instead of restart_process + wait_for_log + get_logs + run_checks."},
		func(ctx context.Context, req *mcp.CallToolRequest, in verifyChangeIn) (*mcp.CallToolResult, *verifyChangeOut, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			return nil, h.verifyChange(ctx, req, rt, in), nil
		})
}

func (h *handlers) verifyChange(ctx context.Context, req *mcp.CallToolRequest, rt runtime.Facade, in verifyChangeIn) *verifyChangeOut {
	out := &verifyChangeOut{ProcessID: in.ProcessID, Steps: []verifyStep{}, NewErrors: []api.ErrorGroup{}, Verdict: "fail"}
	fail := func(step, detail string) *verifyChangeOut {
		out.Steps = append(out.Steps, verifyStep{Step: step, OK: false, Detail: detail})
		out.Summary = step + " failed: " + detail
		h.verifyFinish(rt, out)
		return out
	}
	base, err := rt.GetLogs(api.GetLogsRequest{ProcessID: in.ProcessID, Lines: 1})
	if err != nil {
		out.Summary = err.Error()
		out.Steps = append(out.Steps, verifyStep{Step: "locate process", Detail: err.Error()})
		return out
	}
	out.Baseline = base.Cursor
	restart := boolOr(in.Restart, true)
	if !restart && in.WaitContains == "" && in.WaitPattern == "" {
		return fail("wait", "restart=false needs wait_contains or wait_pattern: the line the dev server prints after it rebuilds")
	}
	if restart {
		if err := rt.Restart(ctx, in.ProcessID); err != nil {
			return fail("restart", err.Error())
		}
		out.Steps = append(out.Steps, verifyStep{Step: "restart", OK: true})
	}
	eff, clamped := h.clampWait(max(in.TimeoutMS, 0))
	if in.TimeoutMS <= 0 {
		eff, clamped = h.clampWait(60000)
	}
	stop := h.progress(ctx, req, time.Duration(eff)*time.Millisecond, "waiting for "+in.ProcessID+" to pick up the change")
	wreq := api.WaitForLogRequest{ProcessID: in.ProcessID, Contains: in.WaitContains, Pattern: in.WaitPattern, FailPattern: in.FailPattern, TimeoutMS: eff, Since: out.Baseline}
	waited := false
	if wreq.Contains == "" && wreq.Pattern == "" {
		wreq.Ready = true
		wreq.Since = "instance"
	}
	w, werr := rt.WaitForLog(ctx, wreq)
	if werr != nil && wreq.Ready && strings.Contains(werr.Error(), "no readiness") {
		werr = nil
		w = nil
		settle := in.SettleMS
		if settle <= 0 {
			settle = 2000
		}
		ex, err := rt.WaitForExit(ctx, api.WaitForExitParams{ProcessID: in.ProcessID, TimeoutMS: settle})
		stop()
		if err != nil {
			return fail("start", err.Error())
		}
		if ex != nil && !ex.Timeout {
			return fail("start", "the process exited during startup")
		}
		out.Steps = append(out.Steps, verifyStep{Step: "start", OK: true, Detail: fmt.Sprintf("stayed up for %dms (no readiness line configured)", settle)})
		waited = true
	} else {
		stop()
	}
	if !waited {
		if werr != nil {
			return fail("wait", werr.Error())
		}
		out.Wait = w
		switch {
		case w.Failed:
			line := ""
			if w.Entry != nil {
				line = w.Entry.Line
			}
			return fail("wait", "fail line appeared: "+truncate(line, 200))
		case w.Exited:
			return fail("wait", "the process exited before the change went live")
		case w.Timeout:
			msg := fmt.Sprintf("no matching line within %dms", eff)
			if clamped {
				msg += " (per-call limit for this client; call wait_for_log with since=\"" + w.Cursor + "\" to keep waiting)"
			}
			return fail("wait", msg)
		}
		detail := ""
		if w.Entry != nil {
			detail = truncate(w.Entry.Line, 200)
		}
		out.Steps = append(out.Steps, verifyStep{Step: "wait", OK: true, Detail: detail})
	}
	if boolOr(in.Checks, true) {
		cres, err := rt.RunChecks(ctx, api.RunChecksRequest{ProcessID: in.ProcessID})
		switch {
		case err != nil:
			out.Steps = append(out.Steps, verifyStep{Step: "checks", OK: true, Detail: "skipped: " + err.Error()})
		case len(cres.Results) == 0:
			out.Steps = append(out.Steps, verifyStep{Step: "checks", OK: true, Detail: "no saved checks; probe the changed behaviour and save it with save_run_config(checks=[...])"})
		default:
			out.Checks = cres
			if cres.Failed > 0 {
				var names []string
				for _, r := range cres.Results {
					if !r.OK {
						n := r.Name
						if n == "" {
							n = r.URL
						}
						names = append(names, n)
					}
				}
				return fail("checks", fmt.Sprintf("%d of %d failed: %s", cres.Failed, cres.Passed+cres.Failed, strings.Join(names, ", ")))
			}
			out.Steps = append(out.Steps, verifyStep{Step: "checks", OK: true, Detail: fmt.Sprintf("%d passed", cres.Passed)})
		}
	}
	h.verifyFinish(rt, out)
	if len(out.NewErrors) > 0 {
		out.Verdict = "fail"
		g := out.NewErrors[0]
		where := ""
		if len(g.Locations) > 0 {
			where = fmt.Sprintf(" at %s:%d", g.Locations[0].File, g.Locations[0].Line)
		}
		out.Summary = fmt.Sprintf("%d new error group(s) after the change, first: %s%s", len(out.NewErrors), truncate(g.Message, 160), where)
		out.Steps = append(out.Steps, verifyStep{Step: "errors", OK: false, Detail: out.Summary})
		return out
	}
	out.Steps = append(out.Steps, verifyStep{Step: "errors", OK: true, Detail: "no new errors"})
	out.Verdict = "pass"
	out.Summary = "the change is live and no new errors appeared"
	if out.Checks != nil {
		out.Summary = fmt.Sprintf("the change is live, %d check(s) passed and no new errors appeared", out.Checks.Passed)
	}
	return out
}

func (h *handlers) verifyFinish(rt runtime.Facade, out *verifyChangeOut) {
	if st, err := rt.Status(out.ProcessID); err == nil {
		out.Status = st.Status
		out.InstanceID = st.InstanceID
		if out.Verdict != "pass" && failedStatus(st) {
			out.Diagnosis = h.diagnosis(rt, out.ProcessID)
		}
	}
	if out.Baseline == "" {
		return
	}
	errs, err := rt.Errors(api.GetErrorsRequest{ProcessID: out.ProcessID, Since: out.Baseline, Baseline: out.Baseline})
	if err != nil {
		return
	}
	out.Cursor = errs.Cursor
	for _, g := range errs.Groups {
		if g.New {
			out.NewErrors = append(out.NewErrors, g)
		} else {
			out.OldErrors++
		}
	}
	if out.Verdict != "pass" && len(out.NewErrors) > 0 && out.Hint == "" {
		out.Hint = "Fix new_errors first; each lists file and line. Then call verify_change again."
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
