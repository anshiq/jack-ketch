// LogService: GetLogs (MCP-compatible tail), SearchLogs (FTS/regex),
// TailLogs (stream: backlog + live), WaitForLog, ClearLogs, ExportLogs
// (stream chunks), GetLogStats.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"jack-ketch/internal/config"
	"jack-ketch/internal/core"
	"jack-ketch/internal/logpipe"
	"jack-ketch/internal/logs"
	"jack-ketch/pkg/api"
)

func (s *Server) routeLog(mux *http.ServeMux) {
	p := "/jackketch.v1.LogService/"
	mux.HandleFunc(p+"GetLogs", s.wrap(s.logGet))
	mux.HandleFunc(p+"SearchLogs", s.wrap(s.logSearch))
	mux.HandleFunc(p+"TailLogs", s.logTail)
	mux.HandleFunc(p+"WaitForLog", s.wrap(s.logWait))
	mux.HandleFunc(p+"ClearLogs", s.wrap(s.logClear))
	mux.HandleFunc(p+"ExportLogs", s.logExport)
	mux.HandleFunc(p+"GetLogStats", s.wrap(s.logStats))
}

// redactorFor builds the workspace log redactor (nil-safe).
func (s *Server) redactorForWorkspace(workspaceID string) *config.Redactor {
	if workspaceID == "" {
		return nil
	}
	pr, err := s.engine.GetOrCreateRuntime(workspaceID)
	if err != nil {
		return nil
	}
	if res := pr.ResolvedConfig(); res != nil && len(res.Redact) > 0 {
		return config.CompileRedactor(res.Redact)
	}
	return nil
}

func (s *Server) logGet(w http.ResponseWriter, r *http.Request) (any, error) {
	var req api.GetLogsRequest
	_ = decode(r, &req)
	if req.ProcessID == "" {
		return nil, fmt.Errorf("processId required")
	}
	_, rt, wsID, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	res, err := rt.GetLogs(req)
	if err != nil {
		return nil, err
	}
	if red := s.redactorForWorkspace(wsID); red != nil && !red.Empty() {
		for i := range res.Entries {
			res.Entries[i].Line = red.Redact(res.Entries[i].Line)
		}
	}
	return res, nil
}

func (s *Server) logSearch(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID   string   `json:"workspaceId"`
		ProcessIDs    []string `json:"processIds"`
		Query         string   `json:"query"`
		Regex         bool     `json:"regex"`
		CaseSensitive bool     `json:"caseSensitive"`
		Streams       []string `json:"streams"`
		MinLevel      string   `json:"minLevel"`
		Order         string   `json:"order"`
		MaxRows       int      `json:"maxRows"`
		ContextLines  int      `json:"contextLines"`
		TimeFrom      string   `json:"timeFrom"`
		TimeTo        string   `json:"timeTo"`
		Offset        int      `json:"offset"`
		RawFTS        bool     `json:"rawFts"`
	}
	_ = decode(r, &req)
	if req.Query == "" {
		return nil, fmt.Errorf("query required")
	}
	match, err := compileMatcher(req.Query, req.Regex, req.CaseSensitive)
	if err != nil {
		return nil, err
	}
	ids := req.ProcessIDs
	if len(ids) == 0 {
		if req.WorkspaceID != "" {
			if pr, err := s.engine.GetOrCreateRuntime(req.WorkspaceID); err == nil {
				if rt, err := pr.Runtime(); err == nil {
					if l, err := rt.List(); err == nil {
						for _, p := range l.Processes {
							ids = append(ids, p.ProcessID)
						}
					}
				}
			}
		} else {
			s.engine.RangeRuntimes(func(pr *core.ProjectRuntime) bool {
				if rt, err := pr.Runtime(); err == nil {
					if l, err := rt.List(); err == nil {
						for _, p := range l.Processes {
							ids = append(ids, p.ProcessID)
						}
					}
				}
				return true
			})
		}
	}
	maxRows := req.MaxRows
	if maxRows <= 0 || maxRows > 1000 {
		maxRows = 200
	}
	var fromMs, toMs int64
	if req.TimeFrom != "" {
		fromMs = parseTimeBound(req.TimeFrom)
	}
	if req.TimeTo != "" {
		toMs = parseTimeBound(req.TimeTo)
	}
	var matches []any
	truncated := false
	indexError := ""
	red := s.redactorForWorkspace(req.WorkspaceID)
	seen := map[string]bool{}
	if req.WorkspaceID != "" && !req.Regex {
		query := req.Query
		if !req.RawFTS {
			query = ftsQuery(req.Query)
		}
		if hits, trunc, err := s.searchIndex(req.WorkspaceID, ids, query, maxRows-len(matches)); err == nil {
			for _, h := range hits {
				line := h.Line
				if red != nil {
					line = red.Redact(line)
				}
				level := logs.ParseLevel(line)
				if req.MinLevel != "" && !levelAtLeast(line, req.MinLevel) {
					continue
				}
				stream := streamName(h.Stream)
				if len(req.Streams) > 0 && !streamAllowed(stream, req.Streams) {
					continue
				}
				tsMs := h.TS / int64(time.Millisecond)
				if fromMs > 0 && tsMs < fromMs {
					continue
				}
				if toMs > 0 && tsMs > toMs {
					continue
				}
				key := h.ProcessID + "\x00" + line + "\x00" + strconv.FormatInt(tsMs/1000, 10)
				if seen[key] {
					continue
				}
				seen[key] = true
				matches = append(matches, map[string]any{
					"processId": h.ProcessID, "line": line,
					"stream": stream, "timestamp": h.TS,
					"id": 0, "level": level, "instanceId": h.InstanceID,
				})
			}
			truncated = truncated || trunc
		} else {
			indexError = err.Error()
		}
	}
	for _, id := range ids {
		_, rt, _, err := s.findProcess(id)
		if err != nil {
			continue
		}
		res, err := rt.GetLogs(api.GetLogsRequest{ProcessID: id, Lines: 2000})
		if err != nil {
			continue
		}
		instID := ""
		if proc, ok := rt.Manager().Get(id); ok {
			instID = proc.InstanceID()
		}
		var ring []api.LogEntry
		for _, e := range res.Entries {
			ts := parseEntryTime(e.Timestamp)
			if fromMs > 0 && ts < fromMs {
				continue
			}
			if toMs > 0 && ts > toMs {
				continue
			}
			ring = append(ring, e)
		}
		for idx, e := range ring {
			if match(e.Line) {
				if req.MinLevel != "" && !levelAtLeast(e.Line, req.MinLevel) {
					continue
				}
				if len(req.Streams) > 0 && !streamAllowed(e.Stream, req.Streams) {
					continue
				}
				line := e.Line
				if red != nil {
					line = red.Redact(line)
				}
				key := id + "\x00" + line + "\x00" + strconv.FormatInt(parseEntryTime(e.Timestamp)/1000, 10)
				if seen[key] {
					continue
				}
				seen[key] = true
				m := map[string]any{
					"processId": id, "line": line, "stream": e.Stream,
					"timestamp": e.Timestamp, "id": e.ID,
					"level": logs.ParseLevel(line), "instanceId": instID,
				}
				if req.ContextLines > 0 {
					before := []any{}
					after := []any{}
					for b := idx - req.ContextLines; b < idx; b++ {
						if b >= 0 {
							before = append(before, map[string]any{"line": ring[b].Line, "id": ring[b].ID})
						}
					}
					for a := idx + 1; a <= idx+req.ContextLines && a < len(ring); a++ {
						after = append(after, map[string]any{"line": ring[a].Line, "id": ring[a].ID})
					}
					m["context"] = map[string]any{"before": before, "after": after}
				}
				matches = append(matches, m)
				if len(matches) >= maxRows+req.Offset {
					truncated = true
					break
				}
			}
		}
		if truncated {
			break
		}
	}
	if req.Offset > 0 {
		if req.Offset < len(matches) {
			matches = matches[req.Offset:]
		} else {
			matches = []any{}
		}
	}
	if matches == nil {
		matches = []any{}
	}
	if req.Order == "desc" {
		for i, j := 0, len(matches)-1; i < j; i, j = i+1, j-1 {
			matches[i], matches[j] = matches[j], matches[i]
		}
	}
	out := map[string]any{
		"matches": matches, "truncatedScan": truncated,
		"totalEstimate": len(matches),
	}
	if indexError != "" {
		out["indexError"] = indexError
	}
	return out, nil
}

func streamAllowed(got string, want []string) bool {
	for _, w := range want {
		if w == got || w == "all" {
			return true
		}
	}
	return false
}

func parseTimeBound(v string) int64 {
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		if n < 1e12 {
			return n * 1000
		}
		return n
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UnixMilli()
	}
	return 0
}

func parseEntryTime(v string) int64 {
	if v == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UnixMilli()
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UnixMilli()
	}
	return 0
}

// searchIndex queries the per-workspace FTS index (history + migrated logs).
func (s *Server) searchIndex(workspaceID string, processes []string, query string, maxRows int) ([]logpipe.SearchHit, bool, error) {
	if maxRows <= 0 {
		return nil, false, nil
	}
	idxPath := filepath.Join(s.engine.DataDir(), "logs", workspaceID, "index.db")
	idx, err := logpipe.OpenWorkspaceIndex(idxPath)
	if err != nil {
		return nil, false, err
	}
	defer idx.Close()
	return idx.Search(query, processes, nil, 0, 0, maxRows)
}

func streamName(st uint8) string {
	switch st {
	case 1:
		return "stdout"
	case 2:
		return "stderr"
	case 3:
		return "pty"
	case 4:
		return "system"
	}
	return "stdout"
}

func compileMatcher(query string, regex, caseSensitive bool) (func(string) bool, error) {
	if regex {
		q := query
		if !caseSensitive && !strings.HasPrefix(q, "(?i)") {
			q = "(?i)" + q
		}
		re, err := regexp.Compile(q)
		if err != nil {
			return nil, fmt.Errorf("invalid_argument: invalid regex: %v", err)
		}
		return re.MatchString, nil
	}
	if caseSensitive {
		return func(l string) bool { return strings.Contains(l, query) }, nil
	}
	lq := strings.ToLower(query)
	return func(l string) bool { return strings.Contains(strings.ToLower(l), lq) }, nil
}

func ftsQuery(q string) string {
	fields := strings.Fields(q)
	for i, f := range fields {
		fields[i] = `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
	}
	return strings.Join(fields, " ")
}

func (s *Server) logTail(w http.ResponseWriter, r *http.Request) {
	var raw map[string]json.RawMessage
	_ = decode(r, &raw)
	var req struct {
		WorkspaceID string   `json:"workspaceId"`
		ProcessIDs  []string `json:"processIds"`
		Backlog     int      `json:"backlog"`
		Streams     []string `json:"streams"`
		Contains    string   `json:"contains"`
	}
	b, _ := json.Marshal(raw)
	_ = json.Unmarshal(normalizeTopLevelCasing(b), &req)
	params := rtParams{
		"workspaceId": req.WorkspaceID, "processIds": req.ProcessIDs,
		"backlog": req.Backlog, "contains": req.Contains,
	}
	if v, ok := raw["resume"]; ok && len(v) > 0 {
		var m map[string]uint64
		if json.Unmarshal(v, &m) == nil {
			conv := make(map[string]any, len(m))
			for id, n := range m {
				conv[id] = float64(n)
			}
			params["resume"] = conv
		}
	}
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	defer sw.Close()
	if err := s.topicLogs(r.Context(), params, sw.sendMap); err != nil {
		return
	}
}

func (s *Server) logWait(w http.ResponseWriter, r *http.Request) (any, error) {
	var req api.WaitForLogRequest
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	d := 30 * time.Second
	if req.TimeoutMS > 0 {
		d = time.Duration(req.TimeoutMS)*time.Millisecond + 5*time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), d)
	defer cancel()
	return rt.WaitForLog(ctx, req)
}

func (s *Server) logClear(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
		Stream    string `json:"stream"`
	}
	_ = decode(r, &req)
	pr, rt, wsID, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	noteScope(r, wsID, pr.ProjectID())
	noteArgs(r, map[string]any{"processId": req.ProcessID, "stream": req.Stream})
	return rt.ClearLogs(req.ProcessID, req.Stream)
}

func (s *Server) logExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProcessID string `json:"processId"`
		Format    string `json:"format"`
	}
	_ = decode(r, &req)
	_, rt, wsID, err := s.findProcess(req.ProcessID)
	if err != nil {
		writeError(w, err)
		return
	}
	format := req.Format
	if format == "" {
		format = "ndjson"
	}
	proc, ok := rt.Manager().Get(req.ProcessID)
	if !ok {
		writeError(w, fmt.Errorf("unknown process %q", req.ProcessID))
		return
	}
	all := proc.Logs.Query(logs.Query{})
	red := s.redactorForWorkspace(wsID)
	sw, ok := newStream(w, r, nil)
	if !ok {
		return
	}
	defer sw.Close()
	const chunk = 100
	instID := proc.InstanceID()
	n := 0
	for i := 0; i < len(all.Entries); i += chunk {
		end := i + chunk
		if end > len(all.Entries) {
			end = len(all.Entries)
		}
		var data string
		for _, e := range all.Entries[i:end] {
			line := e.Line
			if red != nil {
				line = red.Redact(line)
			}
			ts := e.Timestamp.Format(time.RFC3339Nano)
			switch format {
			case "txt":
				data += line + "\n"
			case "json":
				b, _ := json.Marshal(map[string]any{"id": e.ID, "timestamp": ts, "stream": string(e.Stream), "line": line, "instanceId": instID})
				data += string(b) + "\n"
			default:
				b, _ := json.Marshal(map[string]any{"line": line, "stream": string(e.Stream), "timestamp": ts, "id": e.ID})
				data += string(b) + "\n"
			}
			n++
		}
		if err := sw.send(map[string]any{"kind": "chunk", "data": data, "format": format}); err != nil {
			return
		}
	}
	_ = sw.send(map[string]any{"kind": "done", "lines": n, "format": format})
}

func (s *Server) logStats(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ProcessID   string `json:"processId"`
	}
	_ = decode(r, &req)
	stats := map[string]any{"indexLagSeconds": 0, "source": "ring"}
	if req.ProcessID != "" {
		_, rt, _, err := s.findProcess(req.ProcessID)
		if err != nil {
			return nil, err
		}
		if proc, ok := rt.Manager().Get(req.ProcessID); ok {
			stdout := proc.Logs.Count(logs.StreamStdout)
			stderr := proc.Logs.Count(logs.StreamStderr)
			stats["stdoutLines"] = stdout
			stats["stderrLines"] = stderr
			stats["totalLines"] = stdout + stderr
			all := proc.Logs.Query(logs.Query{})
			var first, last string
			var disk int64
			for _, e := range all.Entries {
				if first == "" {
					first = e.Timestamp.Format(time.RFC3339Nano)
				}
				last = e.Timestamp.Format(time.RFC3339Nano)
				disk += int64(len(e.Line))
			}
			stats["firstTimestamp"] = first
			stats["lastTimestamp"] = last
			stats["diskBytes"] = disk
			stats["segmentCount"] = 0
			if req.WorkspaceID != "" {
				seg := filepath.Join(s.engine.DataDir(), "logs", req.WorkspaceID, "segments")
				if entries, err := os.ReadDir(seg); err == nil {
					stats["segmentCount"] = len(entries)
				}
			}
		}
	} else {
		stats["totalLines"] = 0
		stats["diskBytes"] = logDiskBytes(s.engine.DataDir())
		stats["segmentCount"] = 0
		stats["firstTimestamp"] = ""
		stats["lastTimestamp"] = ""
	}
	return stats, nil
}

// levelAtLeast applies the min_level filter using the shared detector.
func levelAtLeast(line, min string) bool {
	got := logs.ParseLevel(line)
	if got == "" {
		got = "info"
	}
	order := map[string]int{"trace": 0, "debug": 1, "info": 2, "warn": 3, "error": 4, "fatal": 5}
	g, ok1 := order[got]
	m, ok2 := order[min]
	if !ok1 || !ok2 {
		return true
	}
	return g >= m
}
