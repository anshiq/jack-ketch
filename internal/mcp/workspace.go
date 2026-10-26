package mcp

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/runtime"
)

const unboundHint = "no project is bound to this session; pass workspace=\"/absolute/path/to/project\" or call set_workspace first"

func DefaultWorkspacePath() string {
	for _, k := range []string{"JACK_KETCH_PROJECT", "CLAUDE_PROJECT_DIR"} {
		if v := os.Getenv(k); v != "" && filepath.IsAbs(v) {
			return v
		}
	}
	cwd, err := os.Getwd()
	if err != nil || unsuitableWorkspace(cwd) {
		return ""
	}
	return cwd
}

func unsuitableWorkspace(p string) bool {
	clean := filepath.Clean(p)
	if clean == string(filepath.Separator) || clean == "." {
		return true
	}
	if vol := filepath.VolumeName(clean); vol != "" && (clean == vol || clean == vol+string(filepath.Separator)) {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return true
	}
	return false
}

func (h *handlers) scoped(ctx context.Context, req *mcp.CallToolRequest, workspace string) (runtime.Facade, error) {
	rt, err := h.facade(ctx)
	if err != nil {
		return nil, err
	}
	b, ok := rt.(*Bridge)
	if !ok {
		if workspace != "" {
			return nil, errors.New("workspace selection needs the jack-ketch daemon; this session runs in embedded mode")
		}
		return rt, nil
	}
	if workspace != "" {
		return b.ForWorkspace(ctx, workspace)
	}
	if b.Bound() {
		return b, nil
	}
	if path := rootsWorkspace(ctx, req); path != "" {
		if _, err := b.SetWorkspace(ctx, path); err == nil {
			return b, nil
		}
	}
	return nil, errors.New(unboundHint)
}

func rootsWorkspace(ctx context.Context, req *mcp.CallToolRequest) string {
	if req == nil || req.Session == nil {
		return ""
	}
	caps := req.ClientCapabilities()
	if caps == nil || caps.RootsV2 == nil {
		return ""
	}
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	res, err := req.Session.ListRoots(rctx, &mcp.ListRootsParams{})
	if err != nil || res == nil {
		return ""
	}
	for _, r := range res.Roots {
		u, err := url.Parse(r.URI)
		if err != nil || u.Scheme != "file" {
			continue
		}
		p := u.Path
		if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
		p = filepath.FromSlash(p)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return ""
}
