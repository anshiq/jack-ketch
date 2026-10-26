package cli

import (
	"context"
	"time"

	"jack-ketch/internal/config"
	"jack-ketch/internal/platform/paths"
	"jack-ketch/internal/project"
	"jack-ketch/pkg/client"
)

func loadedWithDaemonConfig(base *config.Loaded) *config.Loaded {
	p := paths.User()
	cl, err := client.EnsureDaemonAs(p.SocketPath(), "cli/config")
	if err != nil {
		return base
	}
	root := project.WorkspaceRoot(base.ProjectDir)
	projectID, wsID, err := resolveWorkspace(cl, root)
	if err != nil {
		return base
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := cl.ConfigService().Get(ctx, wsID)
	if err != nil {
		return base
	}
	raw, _ := res["raw"].(map[string]any)
	src := &config.MemorySource{Overlays: map[string][]byte{}}
	if s, ok := raw["project"].(string); ok {
		src.ProjectYAML = []byte(s)
	}
	if s, ok := raw["workspace"].(string); ok {
		src.Overlays[wsID] = []byte(s)
	}
	resolved, errs := config.Resolve(src, projectID, wsID, root, nil)
	if errs != nil {
		return base
	}
	return resolved.ToLoaded(root)
}
