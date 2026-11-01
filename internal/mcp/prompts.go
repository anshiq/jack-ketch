package mcp

import (
	"context"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func promptResult(desc, text string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{Description: desc, Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}}}
}

func registerPrompts(s *mcp.Server, h *handlers) {
	s.AddPrompt(&mcp.Prompt{
		Name: "start_app", Title: "Start an app with jack-ketch",
		Description: "Start a configured app (or the main dev server) and confirm it is ready.",
		Arguments:   []*mcp.PromptArgument{{Name: "app", Title: "App", Description: "App name from list_apps; empty means pick the main dev server"}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		app := strings.TrimSpace(req.Params.Arguments["app"])
		target := "the project's main development server (pick it from list_apps)"
		if app != "" {
			target = "the app \"" + app + "\""
		}
		return promptResult("Start an app", "Use the jack-ketch tools to start "+target+". First call list_processes and reuse a running instance instead of starting a duplicate. Otherwise call start_process(app=..., timeout_ms=60000). If the result says wait.clamped=true, call wait_for_log(process_id, ready=true) again. If it fails, read get_logs(process_id, stream=\"stderr\", lines=100), explain the cause, fix it, then restart_process and wait again. Report the process_id, the URL or port, and the readiness line."), nil
	})
	s.AddPrompt(&mcp.Prompt{
		Name: "diagnose", Title: "Diagnose a failing process",
		Description: "Collect status, errors and recent logs for a process and explain the failure.",
		Arguments:   []*mcp.PromptArgument{{Name: "process_id", Title: "Process", Description: "process_id from list_processes; empty means the most recently failed process"}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		pid := strings.TrimSpace(req.Params.Arguments["process_id"])
		target := "the most recently crashed or exited process from list_processes"
		if pid != "" {
			target = "process " + pid
		}
		return promptResult("Diagnose a process", "Diagnose "+target+" with jack-ketch: call process_status, then get_logs(stream=\"stderr\", lines=100), then search_logs for error or exception across the project. Summarize the root cause with the exact log lines, propose a fix, and do not restart anything until the user agrees."), nil
	})
	s.AddPrompt(&mcp.Prompt{
		Name: "verify", Title: "Verify a change at runtime",
		Description: "Run the app and exercise a just-implemented fix or feature live, showing responses and the logs they produced.",
		Arguments:   []*mcp.PromptArgument{{Name: "change", Title: "Change", Description: "What was changed, e.g. \"POST /api/orders validates quantity\""}, {Name: "app", Title: "App", Description: "App to run; empty means pick from list_apps"}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		change := strings.TrimSpace(req.Params.Arguments["change"])
		if change == "" {
			change = "the change you just made"
		}
		app := strings.TrimSpace(req.Params.Arguments["app"])
		target := "the app that serves it (list_apps; reuse a running process from list_processes)"
		if app != "" {
			target = "the app \"" + app + "\" (reuse a running process from list_processes)"
		}
		return promptResult("Verify at runtime", "Show "+change+" working at runtime with jack-ketch. Start or restart_process "+target+" and wait_for_log(ready=true). Plan 2-5 concrete interactions that prove the change, including one edge case. For HTTP use probe (path, method, body, expect_status); the result includes the logs the request produced. Ask the user before any non-GET request or a non-local target, and never against production data. For CLIs or scripts use start_process(command=..., timeout_ms=...) and check exit code and output; for workers trigger the job and wait_for_log for its outcome. Report each interaction as expected vs actual with the key log lines. If something fails, call diagnose_process. Finally offer to save the useful probes as checks with save_run_config(checks=[...]) so run_checks can repeat them."), nil
	})
	s.AddPrompt(&mcp.Prompt{
		Name: "status", Title: "Project process status",
		Description: "Summarize every process jack-ketch supervises for this project.",
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return promptResult("Process status", "Call list_processes and, for each running or failed process, process_status. Present a table with process_id, app or command, status, health, pid, ports if known, and the last error line from get_logs(stream=\"stderr\", lines=5) for failed ones."), nil
	})
}

func (h *handlers) complete(ctx context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	var values []string
	if req.Params != nil && req.Params.Ref != nil && req.Params.Ref.Type == "ref/prompt" {
		prefix := strings.ToLower(req.Params.Argument.Value)
		if rt, err := h.facade(ctx); err == nil {
			switch req.Params.Argument.Name {
			case "app":
				if apps, err := rt.Apps(); err == nil {
					for _, a := range apps.Apps {
						if strings.HasPrefix(strings.ToLower(a.Name), prefix) {
							values = append(values, a.Name)
						}
					}
				}
			case "process_id":
				if list, err := rt.List(); err == nil {
					for _, p := range list.Processes {
						if strings.HasPrefix(strings.ToLower(p.ProcessID), prefix) {
							values = append(values, p.ProcessID)
						}
					}
				}
			}
		}
	}
	sort.Strings(values)
	total := len(values)
	if len(values) > 100 {
		values = values[:100]
	}
	if values == nil {
		values = []string{}
	}
	return &mcp.CompleteResult{Completion: mcp.CompletionResultDetails{Values: values, Total: total, HasMore: total > len(values)}}, nil
}
