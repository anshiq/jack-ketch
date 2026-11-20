package server

import (
	"fmt"
	"net/http"
	"time"
)

func (s *Server) routeTimeline(mux *http.ServeMux) {
	mux.HandleFunc("/jackketch.v1.LogService/ErrorRate", s.wrap(s.handleErrorRate))
}

func (s *Server) handleErrorRate(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		WindowMs    int64  `json:"windowMs"`
		Buckets     int    `json:"buckets"`
	}
	if err := decode(r, &req); err != nil {
		return nil, fmt.Errorf("invalid_argument: %v", err)
	}
	if req.WindowMs <= 0 {
		req.WindowMs = int64(time.Hour / time.Millisecond)
	}
	if req.WindowMs > int64(7*24*time.Hour/time.Millisecond) {
		req.WindowMs = int64(7 * 24 * time.Hour / time.Millisecond)
	}
	if req.Buckets <= 0 || req.Buckets > 240 {
		req.Buckets = 60
	}
	now := time.Now()
	window := time.Duration(req.WindowMs) * time.Millisecond
	from := now.Add(-window)
	width := window / time.Duration(req.Buckets)
	type series struct {
		ProcessID   string `json:"processId"`
		App         string `json:"app,omitempty"`
		Command     string `json:"command"`
		Status      string `json:"status"`
		WorkspaceID string `json:"workspaceId"`
		Counts      []int  `json:"counts"`
		Total       int    `json:"total"`
	}
	out := []series{}
	for _, pr := range s.engine.LoadedRuntimes() {
		if req.WorkspaceID != "" && pr.WorkspaceID() != req.WorkspaceID {
			continue
		}
		rt := pr.LoadedRuntime()
		if rt == nil {
			continue
		}
		list, err := rt.List()
		if err != nil {
			continue
		}
		for _, p := range list.Processes {
			counts, total, err := rt.ErrorBuckets(p.ProcessID, from, width, req.Buckets)
			if err != nil {
				continue
			}
			out = append(out, series{ProcessID: p.ProcessID, App: p.App, Command: p.Command, Status: p.Status, WorkspaceID: pr.WorkspaceID(), Counts: counts, Total: total})
		}
	}
	return map[string]any{
		"from":     from.UnixMilli(),
		"to":       now.UnixMilli(),
		"bucketMs": width.Milliseconds(),
		"series":   out,
	}, nil
}
