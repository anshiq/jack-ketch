package client

import (
	"context"
	"encoding/json"

	"jack-ketch/pkg/api"
)

type CredentialServiceClient struct {
	client *Client
}

func (c *Client) CredentialService() *CredentialServiceClient {
	return &CredentialServiceClient{c}
}

func (c *CredentialServiceClient) List(ctx context.Context, workspaceID string) (*api.ListCredentialsResult, error) {
	var out api.ListCredentialsResult
	err := c.client.call(ctx, "CredentialService", "List", map[string]any{"workspaceId": workspaceID}, &out)
	return &out, err
}

func (c *CredentialServiceClient) ListProxies(ctx context.Context, workspaceID string) ([]api.CredentialProxy, error) {
	var out struct {
		Proxies []api.CredentialProxy `json:"proxies"`
	}
	err := c.client.call(ctx, "CredentialService", "ListProxies", map[string]any{"workspaceId": workspaceID}, &out)
	return out.Proxies, err
}

func (c *CredentialServiceClient) StartProxy(ctx context.Context, workspaceID string, req api.StartCredentialProxyRequest) (*api.CredentialProxy, error) {
	body := map[string]any{
		"workspaceId": workspaceID, "profile": req.Profile, "upstream": req.Upstream, "process_id": req.ProcessID,
		"port": req.Port, "ttl_minutes": req.TTLMinutes, "label": req.Label, "confirmed": req.Confirmed,
	}
	var raw json.RawMessage
	if err := c.client.call(ctx, "CredentialService", "StartProxy", body, &raw); err != nil {
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
	var out api.CredentialProxy
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *CredentialServiceClient) StopProxy(ctx context.Context, workspaceID, id string) error {
	return c.client.call(ctx, "CredentialService", "StopProxy", map[string]any{"workspaceId": workspaceID, "id": id}, nil)
}
