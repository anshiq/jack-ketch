package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/runtime"
	"jack-ketch/pkg/api"
)

type listCredentialsIn struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type credentialProxyIn struct {
	Action     string `json:"action" jsonschema:"start, stop or list."`
	Profile    string `json:"profile,omitempty" jsonschema:"start: credential profile name from list_credentials."`
	ProcessID  string `json:"process_id,omitempty" jsonschema:"start: running process whose port is the upstream (or pass upstream)."`
	Upstream   string `json:"upstream,omitempty" jsonschema:"start: upstream base URL, for example http://localhost:3000."`
	Port       int    `json:"port,omitempty" jsonschema:"start: port to use when the process listens on several."`
	TTLMinutes int    `json:"ttl_minutes,omitempty" jsonschema:"start: lifetime in minutes (default 30, max 480)."`
	Label      string `json:"label,omitempty" jsonschema:"start: short note shown next to the proxy in the web UI."`
	ID         string `json:"id,omitempty" jsonschema:"stop: proxy id."`
	Confirmed  bool   `json:"confirmed,omitempty" jsonschema:"Set only after the user approved opening the proxy."`
	Workspace  string `json:"workspace,omitempty" jsonschema:"Absolute project path to act on. Omit to use this session's project."`
}

type credentialProxyOut struct {
	Proxy   *api.CredentialProxy  `json:"proxy,omitempty"`
	Proxies []api.CredentialProxy `json:"proxies"`
	Stopped bool                  `json:"stopped,omitempty"`
	Usage   string                `json:"usage,omitempty"`
}

func registerCredentialTools(server *mcp.Server, h *handlers) {
	addTool(server, h,
		&mcp.Tool{Name: "list_credentials", Title: "List credentials", Annotations: readOnly("List credentials"), Description: "Credential profiles the user configured in the web UI for this project (JWT, cookies, API keys, tenant id...). Shows each profile's name, environment, default flag, fields (name, kind, whether it holds a value, the value only when it is not secret), token status (ok, expiring, expired, rejected) and whether a login recipe can refresh it. Secret values are never returned. Use them by passing credential=<name> to probe (the default profile is applied automatically) or with ${cred:<field>} placeholders in the url, headers or body."},
		func(ctx context.Context, req *mcp.CallToolRequest, in listCredentialsIn) (*mcp.CallToolResult, *api.ListCredentialsResult, error) {
			b, err := h.bridge(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			res, err := b.ListCredentials(ctx)
			if err != nil {
				return nil, nil, err
			}
			if res.Profiles == nil {
				res.Profiles = []api.CredentialProfile{}
			}
			return nil, res, nil
		})

	addTool(server, h,
		&mcp.Tool{Name: "credential_proxy", Title: "Credential proxy", Annotations: mutating("Credential proxy", false, false), Description: "Open a short-lived local proxy (127.0.0.1:<port>) that forwards to the app and attaches a credential profile to every request, so curl, Playwright, e2e suites or a browser can run as an authenticated user without you ever seeing the secret. Secret values are scrubbed from responses and Set-Cookie headers are kept inside jack-ketch. Starting a proxy needs the user's approval; it expires after ttl_minutes (default 30) and can be stopped with action=stop. WebSocket upgrades are not supported."},
		func(ctx context.Context, req *mcp.CallToolRequest, in credentialProxyIn) (*mcp.CallToolResult, *credentialProxyOut, error) {
			b, err := h.bridge(ctx, req, in.Workspace)
			if err != nil {
				return nil, nil, err
			}
			switch in.Action {
			case "list", "":
				list, err := b.ListCredentialProxies(ctx)
				if err != nil {
					return nil, nil, err
				}
				return nil, &credentialProxyOut{Proxies: list}, nil
			case "stop":
				if in.ID == "" {
					return nil, nil, errors.New("stop needs the proxy id")
				}
				if err := b.StopCredentialProxy(ctx, in.ID); err != nil {
					return nil, nil, err
				}
				list, _ := b.ListCredentialProxies(ctx)
				return nil, &credentialProxyOut{Proxies: list, Stopped: true}, nil
			case "start":
			default:
				return nil, nil, fmt.Errorf("unknown action %q (start, stop or list)", in.Action)
			}
			if in.Profile == "" {
				return nil, nil, errors.New("start needs profile (see list_credentials)")
			}
			preq := api.StartCredentialProxyRequest{Profile: in.Profile, Upstream: in.Upstream, ProcessID: in.ProcessID, Port: in.Port, TTLMinutes: in.TTLMinutes, Label: in.Label, Confirmed: in.Confirmed}
			got, err := b.StartCredentialProxy(ctx, preq)
			var ce *runtime.ConfirmError
			if errors.As(err, &ce) {
				target := in.Upstream
				if target == "" {
					target = "process " + in.ProcessID
				}
				pending, approved, supported := elicitApproval(req, "credential_proxy", fmt.Sprintf("jack-ketch: open a local proxy to %s that attaches credential profile %s? (%s)", target, in.Profile, ce.Reason))
				switch {
				case pending != nil:
					return pending, nil, nil
				case supported && !approved:
					return nil, nil, errors.New("the user declined to open the credential proxy")
				case supported && approved:
					preq.Confirmed = true
					got, err = b.StartCredentialProxy(ctx, preq)
				}
			}
			if err != nil {
				return nil, nil, err
			}
			list, _ := b.ListCredentialProxies(ctx)
			usage := fmt.Sprintf("Send requests to %s instead of the app URL; credential profile %s is attached automatically. It expires at %s. Stop it with credential_proxy(action=\"stop\", id=%q) when finished.", got.URL, got.Profile, time.Unix(got.ExpiresAt, 0).Format(time.RFC3339), got.ID)
			return nil, &credentialProxyOut{Proxy: got, Proxies: list, Usage: usage}, nil
		})
}
