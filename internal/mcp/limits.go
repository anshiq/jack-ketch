package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/integrate"
)

const clampHint = "Waited %dms, the per-call limit for this client. The process is still running: call %s again to keep waiting."

type limitState struct {
	mu            sync.RWMutex
	harness       string
	source        string
	explicit      bool
	override      int
	clientName    string
	clientVersion string
}

func newLimitState(harness string, explicit bool, override int) *limitState {
	l := &limitState{harness: harness, explicit: explicit, override: override}
	switch {
	case harness != "" && explicit:
		l.source = "flag"
	case harness != "":
		l.source = "env"
	default:
		if id := integrate.HarnessFromEnv(); id != "" {
			l.harness, l.source = id, "env"
		} else {
			l.harness, l.source = "unknown", "unknown"
		}
	}
	return l
}

func (l *limitState) maxWaitLocked() int {
	if l.override > 0 {
		return l.override
	}
	return integrate.WaitLimit(l.harness, l.explicit)
}

func (l *limitState) maxWait() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.maxWaitLocked()
}

func (l *limitState) observe(name, title, version string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clientName, l.clientVersion = name, version
	if l.explicit {
		return l.harness, false
	}
	id := integrate.HarnessFromClient(name, title)
	if id == "" || id == l.harness {
		return l.harness, false
	}
	l.harness, l.source = id, "client"
	return id, true
}

func (l *limitState) snapshot() map[string]any {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return map[string]any{
		"harness": l.harness, "harnessSource": l.source,
		"clientName": l.clientName, "clientVersion": l.clientVersion,
		"maxWaitMs": l.maxWaitLocked(),
	}
}

func (h *handlers) observe(ctx context.Context, req *mcp.CallToolRequest) {
	if req == nil {
		return
	}
	info := req.ClientInfo()
	if info == nil {
		return
	}
	h.clientMu.Lock()
	if h.clientSeen {
		h.clientMu.Unlock()
		return
	}
	h.clientSeen = true
	h.clientMu.Unlock()
	id, changed := h.limits.observe(info.Name, info.Title, info.Version)
	harness := ""
	if changed {
		harness = id
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if rt, err := h.facade(ctx); err == nil {
			if b, ok := rt.(*Bridge); ok {
				b.UpdateClient(ctx, harness, info.Name, info.Version)
			}
		}
	}()
}

func (h *handlers) clampWait(ms int) (int, bool) {
	if ms <= 0 {
		ms = 30000
	}
	if limit := h.limits.maxWait(); limit > 0 && ms > limit {
		return limit, true
	}
	return ms, false
}

func (h *handlers) progress(ctx context.Context, req *mcp.CallToolRequest, budget time.Duration, label string) func() {
	if req == nil || req.Session == nil || req.Params == nil || h.progEvery <= 0 {
		return func() {}
	}
	tok := req.Params.GetProgressToken()
	if tok == nil {
		return func() {}
	}
	done := make(chan struct{})
	start := time.Now()
	go func() {
		t := time.NewTicker(h.progEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				el := time.Since(start)
				_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: tok, Progress: el.Seconds(), Total: budget.Seconds(),
					Message: fmt.Sprintf("%s (%ds of %ds)", label, int(el.Seconds()), int(budget.Seconds())),
				})
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
