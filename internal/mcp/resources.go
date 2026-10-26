package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/pkg/api"
)

const resourcePrefix = "jack-ketch://processes/"

func jsonResource(uri string, v any) (*mcp.ReadResourceResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(b)}}}, nil
}

func registerResources(s *mcp.Server, h *handlers) {
	s.AddResource(&mcp.Resource{URI: "jack-ketch://processes", Name: "processes", Title: "Managed processes", MIMEType: "application/json", Description: "Processes jack-ketch supervises for this session's project."},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			rt, err := h.scoped(ctx, nil, "")
			if err != nil {
				return nil, err
			}
			list, err := rt.List()
			if err != nil {
				return nil, err
			}
			return jsonResource(req.Params.URI, list)
		})
	s.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "jack-ketch://processes/{process_id}", Name: "process-status", Title: "Process status", MIMEType: "application/json", Description: "Status of one managed process."},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			id := strings.TrimPrefix(req.Params.URI, resourcePrefix)
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, err
			}
			st, err := rt.Status(id)
			if err != nil {
				return nil, mcp.ResourceNotFoundError(req.Params.URI)
			}
			return jsonResource(req.Params.URI, st)
		})
	s.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "jack-ketch://processes/{process_id}/logs", Name: "process-logs", Title: "Process logs", MIMEType: "text/plain", Description: "The last 200 log lines of one managed process."},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			id := strings.TrimSuffix(strings.TrimPrefix(req.Params.URI, resourcePrefix), "/logs")
			rt, err := h.facade(ctx)
			if err != nil {
				return nil, err
			}
			logs, err := rt.GetLogs(api.GetLogsRequest{ProcessID: id, Lines: 200})
			if err != nil {
				return nil, mcp.ResourceNotFoundError(req.Params.URI)
			}
			var sb strings.Builder
			for _, e := range logs.Entries {
				sb.WriteString(e.Timestamp + " [" + e.Stream + "] " + e.Line + "\n")
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "text/plain", Text: sb.String()}}}, nil
		})
	s.AddResource(&mcp.Resource{URI: "jack-ketch://project/config", Name: "project-config", Title: "Project config", MIMEType: "application/yaml", Description: "The project's jack-ketch config YAML (edit with validate_config, plan_config and apply_config)."},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			rt, err := h.scoped(ctx, nil, "")
			if err != nil {
				return nil, err
			}
			b, ok := rt.(*Bridge)
			if !ok {
				return nil, errNeedsDaemon()
			}
			cfg, err := b.Client().ConfigService().Get(ctx, b.WorkspaceID())
			if err != nil {
				return nil, err
			}
			yml := ""
			if raw, ok := cfg["raw"].(map[string]any); ok {
				yml, _ = raw["project"].(string)
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "application/yaml", Text: yml}}}, nil
		})
}
