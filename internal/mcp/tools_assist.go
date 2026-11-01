package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/diagnose"
	"jack-ketch/internal/runtime"
	"jack-ketch/pkg/api"
)

type suggester interface {
	SaveSuggestion(ctx context.Context, id string) (*api.SaveSuggestion, error)
}

func (h *handlers) suggestion(ctx context.Context, rt runtime.Facade, id string) *api.SaveSuggestion {
	s, ok := rt.(suggester)
	if !ok {
		return nil
	}
	sug, err := s.SaveSuggestion(ctx, id)
	if err != nil {
		return nil
	}
	return sug
}

func (h *handlers) diagnosis(rt runtime.Facade, id string) *api.Diagnosis {
	d, err := rt.Diagnose(id)
	if err != nil || d == nil {
		return nil
	}
	if !d.Failing && d.Category == diagnose.CatNone {
		return nil
	}
	return d
}

func failedStatus(st *api.StatusResult) bool {
	switch st.Status {
	case "failed", "crashed":
		return true
	case "exited", "stopped":
		return st.ExitCode != nil && *st.ExitCode != 0
	}
	return st.Health == "unhealthy"
}

func (h *handlers) annotateStart(ctx context.Context, rt runtime.Facade, res *api.StartResult) {
	if res.Wait == nil {
		return
	}
	w := res.Wait
	switch {
	case w.Exited && w.ExitCode != nil && *w.ExitCode != 0, res.Status == "failed" || res.Status == "crashed":
		res.Diagnosis = h.diagnosis(rt, res.ProcessID)
	case w.Ready, w.Exited && w.ExitCode != nil && *w.ExitCode == 0, w.Timeout && !w.Clamped:
		res.SaveSuggestion = h.suggestion(ctx, rt, res.ProcessID)
	}
}

func startErrorHint(err error, in startIn) error {
	d := diagnose.Analyze(diagnose.Input{Status: "failed", StartError: err.Error(), Command: in.Command, Args: in.Args, WorkDir: in.WorkDir})
	if len(d.Issues) == 0 {
		return err
	}
	var b strings.Builder
	b.WriteString(err.Error())
	for _, iss := range d.Issues {
		if iss.Category == diagnose.CatStartFailed && len(d.Issues) > 1 {
			continue
		}
		b.WriteString("\n" + iss.Summary)
		for i, s := range iss.Steps {
			if i >= 3 {
				break
			}
			b.WriteString("\n- " + s)
		}
		break
	}
	return errors.New(b.String())
}

type processIDOnlyIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id to diagnose (any state; for a running process it inspects recent errors)."`
}

type probeIn struct {
	ProcessID      string            `json:"process_id" jsonschema:"Running process to exercise; its logs during the request are returned."`
	URL            string            `json:"url,omitempty" jsonschema:"Path like /api/users (resolved against the port the process listens on, or PORT from its env) or a full http(s) URL. ${VAR} expands from the process env."`
	Method         string            `json:"method,omitempty" jsonschema:"HTTP method, default GET. Non-GET/HEAD/OPTIONS needs the user's approval (confirmed=true)."`
	Headers        map[string]string `json:"headers,omitempty" jsonschema:"Request headers; values expand ${VAR} from the process env (for example Authorization: Bearer ${API_TOKEN})."`
	Body           string            `json:"body,omitempty" jsonschema:"Request body; JSON bodies get Content-Type application/json automatically."`
	Port           int               `json:"port,omitempty" jsonschema:"Port to use for a path url when the process listens on several."`
	TCP            string            `json:"tcp,omitempty" jsonschema:"host:port to connect to instead of an HTTP request."`
	ExpectStatus   int               `json:"expect_status,omitempty" jsonschema:"Expected status code; default treats 4xx/5xx as failure."`
	ExpectContains string            `json:"expect_contains,omitempty" jsonschema:"Substring the response body must contain."`
	TimeoutMS      int               `json:"timeout_ms,omitempty" jsonschema:"Request timeout (default 10000, max 120000)."`
	SettleMS       int               `json:"settle_ms,omitempty" jsonschema:"How long to keep collecting logs after the response (default 300)."`
	Confirmed      bool              `json:"confirmed,omitempty" jsonschema:"Set only after the user approved a data-changing method or a non-local target."`
}

type runChecksIn struct {
	ProcessID string   `json:"process_id,omitempty" jsonschema:"Running process whose app/mode checks to run."`
	App       string   `json:"app,omitempty" jsonschema:"App name (when process_id is omitted, its running process is used)."`
	Mode      string   `json:"mode,omitempty" jsonschema:"Mode of the app; default mode when omitted."`
	Names     []string `json:"names,omitempty" jsonschema:"Only run these checks."`
	Workspace string   `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type saveRunIn struct {
	ProcessID   string              `json:"process_id" jsonschema:"The process whose start setup (command, workdir, env, env file) is saved."`
	Action      string              `json:"action,omitempty" jsonschema:"save (default) or dismiss (the user said no: never suggest this run again)."`
	App         string              `json:"app,omitempty" jsonschema:"App name to save under; defaults to save_suggestion.app. An existing app gets a new mode."`
	Mode        string              `json:"mode,omitempty" jsonschema:"Mode name (dev, prod, headless, test, staging...); defaults to save_suggestion.mode."`
	Scope       string              `json:"scope,omitempty" jsonschema:"project (default, shared by every checkout of this repo) or workspace (only this checkout)."`
	Description string              `json:"description,omitempty" jsonschema:"One line saying what this mode is for."`
	SetDefault  bool                `json:"set_default,omitempty" jsonschema:"Make this mode the app's default_mode."`
	Overwrite   bool                `json:"overwrite,omitempty" jsonschema:"Replace an existing mode of the same name that has different settings."`
	EnvSources  []api.EnvSourceSpec `json:"env_sources,omitempty" jsonschema:"Override what env is saved, in priority order (later wins): [{file: .env}, {file: .env.local, optional: true}, {vars: [KEY=VALUE]}]. Default: the run's env_file then its env vars."`
	Checks      []api.CheckSpec     `json:"checks,omitempty" jsonschema:"Runtime checks to save on this mode (use the check object returned by probe)."`
}

type updateGitignoreIn struct {
	Entries   []string `json:"entries,omitempty" jsonschema:"Paths to add (repo-relative); default: every path listed in gitignore.missing."`
	Action    string   `json:"action,omitempty" jsonschema:"add (default) or dismiss (the user said no: stop suggesting these)."`
	Workspace string   `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type gitignorer interface {
	GitignoreStatus(ctx context.Context) (map[string]any, error)
	UpdateGitignore(ctx context.Context, entries []string, action string) (map[string]any, error)
}

func elicitApproval(req *mcp.CallToolRequest, key, message string) (pending *mcp.CallToolResult, approved, supported bool) {
	caps := req.ClientCapabilities()
	if caps == nil || caps.Elicitation == nil || req.Params == nil {
		return nil, false, false
	}
	resp, answered := req.Params.InputResponses[key].(*mcp.ElicitResult)
	if !answered {
		return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{
			key: &mcp.ElicitParams{
				Message: message,
				RequestedSchema: &jsonschema.Schema{
					Type:     "object",
					Required: []string{"approve"},
					Properties: map[string]*jsonschema.Schema{
						"approve": {Type: "boolean", Description: "Allow this request"},
					},
				},
			},
		}}, false, true
	}
	if resp.Action != "accept" {
		return nil, false, true
	}
	ok, _ := resp.Content["approve"].(bool)
	return nil, ok, true
}

func registerAssistTools(server *mcp.Server, h *handlers) {
	addTool(server, h,
		&mcp.Tool{Name: "diagnose_process", Title: "Diagnose process", Annotations: readOnly("Diagnose process"), Description: "Explain why a process failed or is erroring: category (missing_env, invalid_credentials, missing_dependency, missing_tool, service_unreachable, port_in_use, migration_pending, code_error, ...), evidence lines, who must act (user, agent, or agent_with_approval), plain steps, suggested fix commands flagged needs_approval, and for env problems the exact missing keys with hints from .env.example and the file to add them to. Follow its guidance field."},
		func(ctx context.Context, req *mcp.CallToolRequest, in processIDOnlyIn) (*mcp.CallToolResult, *api.Diagnosis, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			d, err := rt.Diagnose(in.ProcessID)
			if err != nil {
				return nil, nil, err
			}
			return nil, d, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "probe", Title: "Probe process", Annotations: mutating("Probe process", false, false), Description: "Exercise a running process at runtime: send an HTTP request (or TCP connect) and get status, headers, body and the exact log lines the process printed while handling it, in one call. Use it to show the user a fix or feature working live. GET/HEAD/OPTIONS to localhost run directly; other methods or non-local targets need the user's approval (confirmed=true). The result's check object can be saved with save_run_config(checks=[...]) for one-call re-verification via run_checks."},
		func(ctx context.Context, req *mcp.CallToolRequest, in probeIn) (*mcp.CallToolResult, *api.ProbeResult, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			preq := api.ProbeRequest{ProcessID: in.ProcessID, Method: in.Method, URL: in.URL, TCP: in.TCP, Port: in.Port, Headers: in.Headers, Body: in.Body,
				ExpectStatus: in.ExpectStatus, ExpectContains: in.ExpectContains, TimeoutMS: in.TimeoutMS, SettleMS: in.SettleMS, Confirmed: in.Confirmed}
			res, err := rt.Probe(ctx, preq)
			var ce *runtime.ConfirmError
			if errors.As(err, &ce) {
				target := in.URL
				if in.TCP != "" {
					target = in.TCP
				}
				method := strings.ToUpper(in.Method)
				if method == "" {
					method = "GET"
				}
				pending, approved, supported := elicitApproval(req, "probe", fmt.Sprintf("jack-ketch: allow %s %s against process %s? (%s)", method, target, in.ProcessID, ce.Reason))
				switch {
				case pending != nil:
					return pending, nil, nil
				case supported && !approved:
					return nil, &api.ProbeResult{ProcessID: in.ProcessID, Method: method, URL: in.URL, Error: "the user declined this request", Failures: []string{"declined by the user"}, Logs: []api.LogEntry{}}, nil
				case supported && approved:
					preq.Confirmed = true
					res, err = rt.Probe(ctx, preq)
				}
			}
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "run_checks", Title: "Run checks", Annotations: mutating("Run checks", false, false), Description: "Run the saved runtime checks (checks: in the app or mode config) against a running process and report pass/fail per check with the logs each request produced. Use after a change to re-verify behaviour in one call."},
		func(ctx context.Context, req *mcp.CallToolRequest, in runChecksIn) (*mcp.CallToolResult, *api.RunChecksResult, error) {
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
			res, err := rt.RunChecks(ctx, api.RunChecksRequest{ProcessID: in.ProcessID, App: in.App, Mode: in.Mode, Names: in.Names})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "save_run_config", Title: "Save run config", Annotations: mutating("Save run config", false, true), Description: "Save how a process was started (command, workdir, env vars and env files in priority order) as a named mode of an app in the project config, so next time it starts with start_process(app=..., mode=...). Call only after the user agreed to the save_suggestion question; with action=dismiss when they declined. Each way of running an app (dev, prod, headless, test, staging...) is its own mode."},
		func(ctx context.Context, req *mcp.CallToolRequest, in saveRunIn) (*mcp.CallToolResult, map[string]any, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			b, ok := rt.(*Bridge)
			if !ok {
				return nil, nil, errNeedsDaemon()
			}
			res, err := b.SaveRun(ctx, api.SaveRunRequest{ProcessID: in.ProcessID, Action: in.Action, App: in.App, Mode: in.Mode, Scope: in.Scope,
				Description: in.Description, SetDefault: in.SetDefault, Overwrite: in.Overwrite, EnvSources: in.EnvSources, Checks: in.Checks})
			if err != nil {
				return nil, nil, err
			}
			out := map[string]any{}
			if err := convert(res.SaveRunResult, &out); err != nil {
				return nil, nil, err
			}
			if len(res.Gitignore) > 0 {
				out["gitignore"] = res.Gitignore
			}
			return nil, out, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "update_gitignore", Title: "Update .gitignore", Annotations: mutating("Update .gitignore", false, true), Description: "Add jack-ketch's files (its .jack-ketch/ state dir, env files that saved apps load, project-scope integration files) to the repository .gitignore so git does not track them. Call only after the user agreed to the gitignore suggestion; action=dismiss when they declined so it is not asked again. Without entries it adds everything listed in gitignore.missing (see get_project_info)."},
		func(ctx context.Context, req *mcp.CallToolRequest, in updateGitignoreIn) (*mcp.CallToolResult, map[string]any, error) {
			rt, err := h.scoped(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			g, ok := rt.(gitignorer)
			if !ok {
				return nil, nil, errNeedsDaemon()
			}
			res, err := g.UpdateGitignore(ctx, in.Entries, in.Action)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})
}
