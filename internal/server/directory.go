package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultDirEntryLimit = 100
	maxDirEntryLimit     = 500
)

type directoryEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

func (s *Server) sysListDirectory(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Dir      string `json:"dir"`
		Prefix   string `json:"prefix"`
		BaseDir  string `json:"baseDir"`
		DirsOnly bool   `json:"dirsOnly"`
		Limit    int    `json:"limit"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultDirEntryLimit
	}
	if limit > maxDirEntryLimit {
		limit = maxDirEntryLimit
	}
	dir := resolveListDir(req.Dir, req.BaseDir)
	out := make([]directoryEntry, 0, limit)
	res, err := os.ReadDir(dir)
	if err != nil {
		return map[string]any{
			"dir": dir, "entries": out, "truncated": false, "error": err.Error(),
		}, nil
	}
	prefix := strings.ToLower(req.Prefix)
	for _, entry := range res {
		name := entry.Name()
		if prefix != "" && !strings.HasPrefix(strings.ToLower(name), prefix) {
			continue
		}
		isDir := entry.IsDir()
		if !isDir && entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.IsDir() {
				isDir = true
			}
		}
		if req.DirsOnly && !isDir {
			continue
		}
		out = append(out, directoryEntry{Name: name, IsDir: isDir})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	truncated := len(out) > limit
	if truncated {
		out = out[:limit]
	}
	return map[string]any{
		"dir": dir, "entries": out, "truncated": truncated, "error": "",
	}, nil
}

func resolveListDir(dir, base string) string {
	if dir == "" {
		dir = "."
	}
	dir = expandHomePath(dir)
	if !filepath.IsAbs(dir) {
		base = expandHomePath(base)
		if base == "" {
			base = homeDir()
		}
		if !filepath.IsAbs(base) {
			base = filepath.Join(homeDir(), base)
		}
		dir = filepath.Join(base, dir)
	}
	dir = filepath.Clean(dir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return dir
}

func expandHomePath(p string) string {
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home := homeDir(); home != "" {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
