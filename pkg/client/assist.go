package client

import (
	"context"
	"encoding/json"
	"errors"

	"jack-ketch/pkg/api"
)

type ConfirmationNeeded struct {
	Reason  string
	Message string
}

func (e *ConfirmationNeeded) Error() string { return e.Message }

func (c *ProcessServiceClient) Diagnose(ctx context.Context, processID string) (*api.Diagnosis, error) {
	var out api.Diagnosis
	err := c.client.call(ctx, "ProcessService", "Diagnose", map[string]any{"processId": processID}, &out)
	return &out, err
}

func (c *ProcessServiceClient) Outcome(ctx context.Context, processID string) (*api.RunOutcome, error) {
	var out api.RunOutcome
	err := c.client.call(ctx, "ProcessService", "Outcome", map[string]any{"processId": processID}, &out)
	return &out, err
}

func (c *ProcessServiceClient) Probe(ctx context.Context, req api.ProbeRequest) (*api.ProbeResult, error) {
	var raw json.RawMessage
	if err := c.client.call(ctx, "ProcessService", "Probe", req, &raw); err != nil {
		return nil, err
	}
	var gate struct {
		NeedsConfirmation bool   `json:"needsConfirmation"`
		Reason            string `json:"reason"`
		Error             string `json:"error"`
	}
	if json.Unmarshal(raw, &gate) == nil && gate.NeedsConfirmation {
		return nil, &ConfirmationNeeded{Reason: gate.Reason, Message: gate.Error}
	}
	var out api.ProbeResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ProcessServiceClient) RunChecks(ctx context.Context, workspaceID string, req api.RunChecksRequest) (*api.RunChecksResult, error) {
	body := map[string]any{"workspaceId": workspaceID, "process_id": req.ProcessID, "app": req.App, "mode": req.Mode, "names": req.Names}
	var out api.RunChecksResult
	err := c.client.call(ctx, "ProcessService", "RunChecks", body, &out)
	return &out, err
}

func (c *ProcessServiceClient) SaveSuggestion(ctx context.Context, processID string) (*api.SaveSuggestion, error) {
	var out struct {
		Suggestion *api.SaveSuggestion `json:"suggestion"`
	}
	err := c.client.call(ctx, "ProcessService", "SaveSuggestion", map[string]any{"processId": processID}, &out)
	return out.Suggestion, err
}

type SaveRunResponse struct {
	api.SaveRunResult
	Gitignore map[string]any `json:"gitignore,omitempty"`
}

func (c *ConfigServiceClient) SaveRun(ctx context.Context, sessionID string, req api.SaveRunRequest) (*SaveRunResponse, error) {
	if req.ProcessID == "" {
		return nil, errors.New("process_id required")
	}
	body := map[string]any{
		"process_id": req.ProcessID, "action": req.Action, "app": req.App, "mode": req.Mode, "scope": req.Scope,
		"description": req.Description, "set_default": req.SetDefault, "overwrite": req.Overwrite,
		"env_sources": req.EnvSources, "checks": req.Checks, "sessionId": sessionID,
	}
	var out SaveRunResponse
	err := c.client.call(ctx, "ConfigService", "SaveRun", body, &out)
	return &out, err
}

func (c *ConfigServiceClient) GitignoreStatus(ctx context.Context, workspaceID string) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ConfigService", "GitignoreStatus", map[string]any{"workspaceId": workspaceID}, &out)
	return out, err
}

func (c *ConfigServiceClient) UpdateGitignore(ctx context.Context, workspaceID string, entries []string, action string) (map[string]any, error) {
	var out map[string]any
	err := c.client.call(ctx, "ConfigService", "UpdateGitignore", map[string]any{"workspaceId": workspaceID, "entries": entries, "action": action}, &out)
	return out, err
}
