// SystemService: GetVersion, Health, GetStats, GetSettings,
// UpdateSettings, Shutdown{keep_processes}.
package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"jack-ketch/internal/core"
	"jack-ketch/internal/platform/paths"
)

func (s *Server) routeSystem(mux *http.ServeMux) {
	p := "/jackketch.v1.SystemService/"
	mux.HandleFunc(p+"GetVersion", s.wrap(func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{
			"daemonVersion": s.version, "apiVersion": "v1",
			"minClientVersion": "v1", "shimProtocol": 1,
		}, nil
	}))
	mux.HandleFunc(p+"Health", s.wrap(func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{
			"healthy": true, "status": "ready",
			"uptimeMs":      time.Since(s.started).Milliseconds(),
			"uptimeSeconds": int64(time.Since(s.started).Seconds()),
			"version":       s.version, "socketPath": s.Addr(),
			"dataDir": s.engine.DataDir(), "tcpAddr": s.TCPAddr(),
		}, nil
	}))
	mux.HandleFunc(p+"GetStats", s.wrap(s.sysStats))
	mux.HandleFunc(p+"GetRuntimeStats", s.wrap(s.sysRuntimeStats))
	mux.HandleFunc(p+"EnableTCP", s.wrap(s.sysEnableTCP))
	mux.HandleFunc(p+"GetSettings", s.wrap(s.sysGetSettings))
	mux.HandleFunc(p+"UpdateSettings", s.wrap(s.sysUpdateSettings))
	mux.HandleFunc(p+"Shutdown", s.wrap(s.sysShutdown))
}

func (s *Server) sysStats(w http.ResponseWriter, r *http.Request) (any, error) {
	projects := 0
	if ps, err := s.engine.Store().ListProjects(); err == nil {
		projects = len(ps)
	}
	processes := 0
	running := 0
	failed := 0
	for _, pr := range s.engine.LoadedRuntimes() {
		if rt, err := pr.Runtime(); err == nil {
			if l, err := rt.List(); err == nil {
				processes += len(l.Processes)
				for _, p := range l.Processes {
					if st, err := rt.Status(p.ProcessID); err == nil {
						switch st.Status {
						case "running", "ready":
							running++
						case "failed", "crashed":
							failed++
						}
					}
				}
			}
		}
	}
	return map[string]any{
		"projects": projects, "processes": processes,
		"sessions":         s.engine.Sessions().Count(),
		"processesRunning": running, "processesFailed": failed,
		"logDiskBytes":    logDiskBytes(s.engine.DataDir()),
		"indexLagSeconds": 0,
		"uptimeSeconds":   int64(time.Since(s.started).Seconds()),
		"streams":         s.rtStreamCounts(),
	}, nil
}

func sysSettingDefs() map[string]any {
	return map[string]any{
		"log_level":        "info",
		"idle_exit":        "never",
		"retention":        "30d",
		"learn_raw_starts": true,
	}
}

func sysSettingSchema() map[string]any {
	return map[string]any{
		"log_level":        map[string]any{"type": "string", "enum": []string{"debug", "info", "warn", "error"}, "description": "Daemon log verbosity"},
		"idle_exit":        map[string]any{"type": "string", "description": "Stop the daemon after this long with no sessions and no running processes (never, 30m, 2h, 1d)"},
		"retention":        map[string]any{"type": "string", "description": "Event history retention (7d, 30d, 90d). Audit history is kept 90 days."},
		"learn_raw_starts": map[string]any{"type": "boolean", "description": "Add raw start_process commands that are still running after 10 seconds to the project config as auto apps"},
	}
}

func (s *Server) sysGetSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	defs := sysSettingDefs()
	out := map[string]any{}
	for k, dv := range defs {
		if v, ok, _ := s.engine.Store().GetSetting(k); ok {
			val := settingJSONValue(v, dv)
			if k == "learn_raw_starts" {
				if str, ok := val.(string); ok {
					val = str == "true"
				}
			}
			out[k] = val
		} else {
			out[k] = dv
		}
	}
	return map[string]any{"settings": out, "schema": sysSettingSchema()}, nil
}

func (s *Server) sysUpdateSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	var raw map[string]any
	if err := decode(r, &raw); err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	defs := sysSettingDefs()
	updates := map[string]any{}
	for k, v := range raw {
		key := toSnakeCase(k)
		if _, ok := defs[key]; !ok {
			return nil, fmt.Errorf("invalid_argument: unknown setting %q", k)
		}
		if err := validateSetting(key, v); err != nil {
			return nil, err
		}
		updates[key] = v
	}
	for k, v := range updates {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if err := s.engine.Store().SetSetting(k, string(b)); err != nil {
			return nil, err
		}
	}
	s.engine.ApplySettings()
	return s.sysGetSettings(w, r)
}

func validateSetting(key string, v any) error {
	switch key {
	case "log_level":
		str, ok := v.(string)
		if !ok || (str != "debug" && str != "info" && str != "warn" && str != "error") {
			return fmt.Errorf("invalid_argument: log_level must be debug|info|warn|error")
		}
	case "idle_exit":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("invalid_argument: idle_exit must be a string")
		}
		if str != "never" {
			if _, err := core.ParseRetention(str); err != nil {
				return fmt.Errorf("invalid_argument: idle_exit must be never or a duration such as 30m, 2h, 1d")
			}
		}
	case "retention":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("invalid_argument: retention must be a string")
		}
		d, err := core.ParseRetention(str)
		if err != nil || d < time.Hour {
			return fmt.Errorf("invalid_argument: retention must be a duration of at least 1h such as 7d or 30d")
		}
	case "learn_raw_starts":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("invalid_argument: learn_raw_starts must be true or false")
		}
	}
	return nil
}

func settingJSONValue(stored string, def any) any {
	var v any
	if json.Unmarshal([]byte(stored), &v) == nil {
		return v
	}
	return def
}

func logDiskBytes(dataDir string) int64 {
	var total int64
	root := filepath.Join(dataDir, "logs")
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func (s *Server) sysRuntimeStats(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
	}
	_ = decode(r, &req)
	if req.WorkspaceID == "" {
		return nil, fmt.Errorf("invalid_argument: workspaceId required")
	}
	_, rt, err := s.engine.RuntimeFor(req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return rt.RuntimeStats()
}

func (s *Server) sysEnableTCP(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Addr string `json:"addr"`
	}
	_ = decode(r, &req)
	if cur := s.TCPAddr(); cur != "" {
		return map[string]any{"addr": cur, "started": false}, nil
	}
	addr := req.Addr
	if addr == "" {
		addr = "127.0.0.1:7350"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		return nil, fmt.Errorf("invalid_argument: the web listener must bind a loopback address")
	}
	tokenPath := s.tokenPath
	if tokenPath == "" {
		tokenPath = paths.User().TokenPath()
	}
	token, err := EnsureToken(tokenPath)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	go func() { _ = s.serveTCPListener(ln, token) }()
	deadline := time.Now().Add(2 * time.Second)
	for s.TCPAddr() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return map[string]any{"addr": s.TCPAddr(), "started": true}, nil
}

func (s *Server) sysShutdown(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		KeepProcesses *bool `json:"keepProcesses"`
	}
	_ = decode(r, &req)
	keep := true
	if req.KeepProcesses != nil {
		keep = *req.KeepProcesses
	}
	if s.onShutdown != nil {
		go s.onShutdown(keep)
	}
	return map[string]any{"draining": true, "keepProcesses": keep}, nil
}
