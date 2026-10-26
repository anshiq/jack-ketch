package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"jack-ketch/internal/config"
	"jack-ketch/internal/runtime"
	"jack-ketch/pkg/api"
	"jack-ketch/pkg/client"
)

func (b *Bridge) Diagnose(id string) (*api.Diagnosis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return b.client.ProcessService().Diagnose(ctx, id)
}

func (b *Bridge) Outcome(ctx context.Context, id string) (*api.RunOutcome, error) {
	return b.client.ProcessService().Outcome(ctx, id)
}

func (b *Bridge) Probe(ctx context.Context, req api.ProbeRequest) (*api.ProbeResult, error) {
	budget := time.Duration(req.TimeoutMS)*time.Millisecond + 20*time.Second
	if req.TimeoutMS <= 0 {
		budget = 40 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	res, err := b.client.ProcessService().Probe(ctx, req)
	var cn *client.ConfirmationNeeded
	if errors.As(err, &cn) {
		return nil, &runtime.ConfirmError{Reason: cn.Reason}
	}
	return res, err
}

func (b *Bridge) RunChecks(ctx context.Context, req api.RunChecksRequest) (*api.RunChecksResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return b.client.ProcessService().RunChecks(ctx, b.ws(), req)
}

func (b *Bridge) SaveSuggestion(ctx context.Context, id string) (*api.SaveSuggestion, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return b.client.ProcessService().SaveSuggestion(ctx, id)
}

func (b *Bridge) SaveRun(ctx context.Context, req api.SaveRunRequest) (*client.SaveRunResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return b.client.ConfigService().SaveRun(ctx, b.SessionID(), req)
}

func (b *Bridge) GitignoreStatus(ctx context.Context) (map[string]any, error) {
	if !b.Bound() {
		return nil, errors.New(unboundHint)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return b.client.ConfigService().GitignoreStatus(ctx, b.ws())
}

func (b *Bridge) UpdateGitignore(ctx context.Context, entries []string, action string) (map[string]any, error) {
	if !b.Bound() {
		return nil, errors.New(unboundHint)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return b.client.ConfigService().UpdateGitignore(ctx, b.ws(), entries, action)
}

func describeRawApp(raw any, info *api.AppInfo) {
	data, err := json.Marshal(raw)
	if err != nil {
		return
	}
	var app config.AppConfig
	if json.Unmarshal(data, &app) != nil {
		return
	}
	runtime.DescribeApp(app, info)
}
