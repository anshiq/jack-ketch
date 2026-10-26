package mcp

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func boolPtr(b bool) *bool { return &b }

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPtr(false)}
}

func mutating(title string, destructive, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: boolPtr(destructive), IdempotentHint: idempotent, OpenWorldHint: boolPtr(false)}
}

var coreTools = map[string]bool{
	"start_process": true, "stop_process": true, "restart_process": true, "process_status": true,
	"list_processes": true, "get_logs": true, "wait_for_log": true, "wait_for_exit": true,
	"list_apps": true, "search_logs": true, "send_stdin": true, "signal_process": true,
	"get_project_info": true, "set_workspace": true,
	"diagnose_process": true, "probe": true, "save_run_config": true,
}

func toolsetFor(name string) map[string]bool {
	if strings.EqualFold(strings.TrimSpace(name), "core") {
		return coreTools
	}
	return nil
}

func addTool[In, Out any](s *mcp.Server, h *handlers, t *mcp.Tool, fn mcp.ToolHandlerFor[In, Out]) {
	if h.toolset != nil && !h.toolset[t.Name] {
		return
	}
	if t.InputSchema == nil {
		t.InputSchema = inputSchema[In]()
	}
	name := t.Name
	mcp.AddTool(s, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		h.observe(ctx, req)
		if name == "send_stdin" {
			h.log(name, "redacted")
		} else {
			h.log(name, in)
		}
		return fn(ctx, req, in)
	})
}
