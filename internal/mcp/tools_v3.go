package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/runtime"
	"jack-ketch/pkg/api"
)

type listProcessesIn struct {
	Scope     string `json:"scope,omitempty" jsonschema:"workspace (default), project (all checkouts of this project) or all (every project on this machine)."`
	Workspace string `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type yamlIn struct {
	YAML      string `json:"yaml" jsonschema:"Full project config YAML."`
	Workspace string `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type applyConfigIn struct {
	YAML         string `json:"yaml" jsonschema:"Full project config YAML (project layer). Read the current YAML from the jack-ketch://project/config resource or the web Config page first."`
	BaseRevision int64  `json:"base_revision,omitempty" jsonschema:"Revision the edit is based on (configRevision from get_project_info). 0 disables the check."`
	Message      string `json:"message,omitempty" jsonschema:"Revision message recorded in config history."`
	Workspace    string `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type searchLogsIn struct {
	Query         string   `json:"query" jsonschema:"Text to find (all words must match), or a regular expression when regex=true."`
	ProcessIDs    []string `json:"process_ids,omitempty" jsonschema:"Limit to these processes; empty searches the whole project."`
	Regex         bool     `json:"regex,omitempty" jsonschema:"Treat query as a regular expression."`
	CaseSensitive bool     `json:"case_sensitive,omitempty" jsonschema:"Match case exactly."`
	Stream        string   `json:"stream,omitempty" jsonschema:"stdout or stderr; empty searches both."`
	Level         string   `json:"level,omitempty" jsonschema:"Minimum level: debug, info, warn or error."`
	ContextLines  int      `json:"context_lines,omitempty" jsonschema:"Lines of context before and after each match (ring buffer only)."`
	MaxRows       int      `json:"max_rows,omitempty" jsonschema:"Maximum matches (default 200, cap 1000)."`
	Workspace     string   `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type resolveProposalIn struct {
	ProposalID string `json:"proposal_id,omitempty" jsonschema:"Proposal id from get_project_info; empty uses the current pending proposal."`
	Action     string `json:"action" jsonschema:"approve or dismiss."`
	Workspace  string `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type setWorkspaceIn struct {
	Path string `json:"path" jsonschema:"Absolute path of the project directory (or a ws_... workspace id)."`
}

type listSessionsOut struct {
	Sessions []map[string]any `json:"sessions"`
}

const descSetWorkspace = "Bind this session to a project directory (absolute path) when the client has no project folder, for example Claude Desktop. Other tools also accept workspace=... per call."

const descListProjects = "Projects and workspace paths known to jack-ketch on this machine."

func (h *handlers) bridge(ctx context.Context, req *mcp.CallToolRequest, workspace string) (*Bridge, error) {
	rt, err := h.scoped(ctx, req, workspace)
	if err != nil {
		return nil, err
	}
	b, ok := rt.(*Bridge)
	if !ok {
		return nil, errNeedsDaemon()
	}
	return b, nil
}

func errNeedsDaemon() error {
	return &daemonRequiredError{}
}

type daemonRequiredError struct{}

func (e *daemonRequiredError) Error() string {
	return "this tool needs the jack-ketch daemon; it is unavailable in embedded mode (start it with: jack-ketch daemon start)"
}

func registerV3Tools(server *mcp.Server, h *handlers) {
	addTool(server, h,
		&mcp.Tool{Name: "get_project_info", Title: "Project info", Annotations: readOnly("Project info"), Description: "Where this session's project lives (ids, path, config revision, pending auto-detect proposal, attached sessions) and how this client is handled (harness, per-call wait limit). Call before editing config."},
		func(ctx context.Context, req *mcp.CallToolRequest, in workspaceIn) (*mcp.CallToolResult, map[string]any, error) {
			base := map[string]any{}
			for k, v := range h.limits.snapshot() {
				base[k] = v
			}
			base["mode"] = h.mode()
			base["serverVersion"] = Version
			if h.toolset != nil {
				base["toolset"] = "core"
			} else {
				base["toolset"] = "full"
			}
			rt, err := h.scoped(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			b, ok := rt.(*Bridge)
			if !ok {
				base["projectDir"] = DefaultWorkspacePath()
				return nil, base, nil
			}
			res, err := b.ProjectInfo(ctx)
			if err != nil {
				return nil, nil, err
			}
			for k, v := range res {
				base[k] = v
			}
			return nil, base, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "validate_config", Title: "Validate config", Annotations: readOnly("Validate config"), Description: "Validate project config YAML without applying it; returns line/column errors and warnings."},
		func(ctx context.Context, req *mcp.CallToolRequest, in yamlIn) (*mcp.CallToolResult, map[string]any, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			b, ok := rt.(*Bridge)
			if !ok {
				return nil, nil, errNeedsDaemon()
			}
			res, err := b.ValidateConfig(ctx, in.YAML)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "plan_config", Title: "Plan config", Annotations: readOnly("Plan config"), Description: "Diff proposed project config YAML against the active revision: apps added, removed, needing restart or applied live."},
		func(ctx context.Context, req *mcp.CallToolRequest, in yamlIn) (*mcp.CallToolResult, map[string]any, error) {
			b, err := h.bridge(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.PlanConfig(ctx, in.YAML)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "apply_config", Title: "Apply config", Annotations: mutating("Apply config", true, true), Description: "Apply project config YAML as a new revision with hot reload. Pass base_revision (configRevision from get_project_info) for optimistic concurrency."},
		func(ctx context.Context, req *mcp.CallToolRequest, in applyConfigIn) (*mcp.CallToolResult, map[string]any, error) {
			b, err := h.bridge(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.ApplyConfig(ctx, in.YAML, in.BaseRevision, in.Message)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "resolve_config_proposal", Title: "Resolve proposal", Annotations: mutating("Resolve proposal", true, false), Description: "Approve or dismiss a pending auto-detected config proposal. Only act on the user's explicit decision; when the client supports it, the user is asked to confirm."},
		func(ctx context.Context, req *mcp.CallToolRequest, in resolveProposalIn) (*mcp.CallToolResult, map[string]any, error) {
			b, err := h.bridge(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			if in.Action == "approve" {
				if caps := req.ClientCapabilities(); caps != nil && caps.Elicitation != nil && req.Params != nil {
					resp, answered := req.Params.InputResponses["confirm"].(*mcp.ElicitResult)
					if !answered {
						summary := ""
						if info, err := b.ProjectInfo(ctx); err == nil {
							if pp, ok := info["pendingProposal"].(map[string]any); ok {
								if s, err := json.Marshal(pp["summary"]); err == nil {
									summary = string(s)
								}
							}
						}
						return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{
							"confirm": &mcp.ElicitParams{
								Message: "jack-ketch: approve the pending config proposal? " + summary,
								RequestedSchema: &jsonschema.Schema{
									Type:     "object",
									Required: []string{"decision"},
									Properties: map[string]*jsonschema.Schema{
										"decision": {Type: "string", Enum: []any{"approve", "dismiss"}, Description: "approve applies the proposal; dismiss discards it"},
									},
								},
							},
						}}, nil, nil
					}
					if resp.Action != "accept" {
						return nil, map[string]any{"resolved": false, "reason": "user " + resp.Action}, nil
					}
					if d, _ := resp.Content["decision"].(string); d == "dismiss" {
						in.Action = "dismiss"
					}
				}
			}
			res, err := b.ResolveProposal(ctx, in.ProposalID, in.Action)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "search_logs", Title: "Search logs", Annotations: readOnly("Search logs"), Description: "Search logs across the project's processes (history and live buffers) by text or regular expression, with optional stream, level and context lines."},
		func(ctx context.Context, req *mcp.CallToolRequest, in searchLogsIn) (*mcp.CallToolResult, map[string]any, error) {
			var b *Bridge
			var err error
			if len(in.ProcessIDs) == 0 {
				b, err = h.bridge(ctx, req, in.Workspace)
			} else {
				var rt runtime.Facade
				rt, err = h.facade(ctx)
				if err == nil {
					if bb, ok := rt.(*Bridge); ok {
						b = bb
					} else {
						err = errNeedsDaemon()
					}
				}
			}
			if err != nil {
				return nil, nil, err
			}
			res, err := b.SearchLogs(ctx, in.Query, in.ProcessIDs, in.Regex, in.CaseSensitive, in.Stream, in.Level, in.ContextLines, in.MaxRows)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "list_sessions", Title: "List sessions", Annotations: readOnly("List sessions"), Description: "Agents and UIs currently attached to this project, with their client names."},
		func(ctx context.Context, req *mcp.CallToolRequest, in workspaceIn) (*mcp.CallToolResult, *listSessionsOut, error) {
			b, err := h.bridge(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.ListSessions(ctx)
			if err != nil {
				return nil, nil, err
			}
			return nil, &listSessionsOut{Sessions: res}, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "set_workspace", Title: "Set workspace", Annotations: readOnly("Set workspace"), Description: descSetWorkspace},
		func(ctx context.Context, req *mcp.CallToolRequest, in setWorkspaceIn) (*mcp.CallToolResult, map[string]any, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			b, ok := rt.(*Bridge)
			if !ok {
				return nil, nil, errNeedsDaemon()
			}
			ref, err := b.SetWorkspace(ctx, in.Path)
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"workspace_id": ref.ws, "project_id": ref.proj, "path": ref.path}, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "list_projects", Title: "List projects", Annotations: readOnly("List projects"), Description: descListProjects},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, nil, err
			}
			b, ok := rt.(*Bridge)
			if !ok {
				return nil, nil, errNeedsDaemon()
			}
			projects, err := b.Client().ProjectService().ListProjects(ctx)
			if err != nil {
				return nil, nil, err
			}
			out := make([]map[string]any, 0, len(projects))
			for _, p := range projects {
				var paths []string
				if wss, err := b.Client().ProjectService().ListWorkspaces(ctx, p.ID); err == nil {
					for _, w := range wss {
						paths = append(paths, w.Path)
					}
				}
				out = append(out, map[string]any{"project_id": p.ID, "name": p.Name, "workspaces": paths})
			}
			return nil, map[string]any{"projects": out, "current_workspace": b.WorkspacePath()}, nil
		})
}

func listScoped(ctx context.Context, h *handlers, req *mcp.CallToolRequest, in listProcessesIn) (*api.ListResult, error) {
	if in.Scope == "all" {
		rt, err := h.facade(ctx)
		if err != nil {
			return nil, err
		}
		if b, ok := rt.(*Bridge); ok {
			return b.ListScoped("all")
		}
		return rt.List()
	}
	rt, err := h.scoped(ctx, req, in.Workspace)
	if err != nil {
		return nil, err
	}
	if b, ok := rt.(*Bridge); ok {
		return b.ListScoped(in.Scope)
	}
	return rt.List()
}

// ListScoped lists processes at workspace/project/all scope.
func (b *Bridge) ListScoped(scope string) (*api.ListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var items []map[string]any
	switch scope {
	case "", "workspace":
		got, err := b.client.ProcessService().List(ctx, b.ws(), false)
		if err != nil {
			return nil, err
		}
		items = got
	case "all":
		got, err := b.client.ProcessService().List(ctx, "", true)
		if err != nil {
			return nil, err
		}
		items = got
	case "project":
		wss, err := b.client.ProjectService().ListWorkspaces(ctx, b.proj())
		if err != nil {
			return nil, err
		}
		for _, w := range wss {
			if got, err := b.client.ProcessService().List(ctx, w.ID, false); err == nil {
				items = append(items, got...)
			}
		}
	default:
		return nil, fmt.Errorf("unknown scope %q (want workspace, project or all)", scope)
	}
	out := &api.ListResult{Processes: []api.ProcessSummary{}}
	for _, it := range items {
		var s api.ProcessSummary
		if err := convert(it, &s); err == nil && s.ProcessID != "" {
			out.Processes = append(out.Processes, s)
		}
	}
	return out, nil
}
