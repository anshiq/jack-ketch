package integrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Detection struct {
	Detected bool     `json:"detected"`
	Version  string   `json:"version,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

type cachedVersion struct {
	v  string
	at time.Time
}

var versionCache sync.Map

var versionRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?(?:[-+][0-9A-Za-z.]+)?`)

func binaryVersion(path string) string {
	if v, ok := versionCache.Load(path); ok {
		if cv := v.(cachedVersion); time.Since(cv.at) < 10*time.Minute {
			return cv.v
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "CI=1")
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	v := ""
	if err == nil {
		v = versionRe.FindString(string(out))
	}
	versionCache.Store(path, cachedVersion{v: v, at: time.Now()})
	return v
}

func extensionDirs(env pathEnv) []string {
	return []string{
		filepath.Join(env.home, ".vscode", "extensions"),
		filepath.Join(env.home, ".vscode-insiders", "extensions"),
		filepath.Join(env.home, ".vscode-oss", "extensions"),
		filepath.Join(env.home, ".cursor", "extensions"),
		filepath.Join(env.home, ".windsurf", "extensions"),
	}
}

func findExtension(env pathEnv, id string) (string, string) {
	prefix := strings.ToLower(id) + "-"
	for _, base := range extensionDirs(env) {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if e.IsDir() && strings.HasPrefix(name, prefix) {
				return filepath.Join(base, e.Name()), versionRe.FindString(name[len(prefix):])
			}
		}
	}
	return "", ""
}

func Detect(h Harness) Detection {
	var d Detection
	env := currentPathEnv("")
	for _, bin := range h.Binaries {
		if path, err := exec.LookPath(bin); err == nil {
			d.Detected = true
			d.Evidence = append(d.Evidence, "binary "+path)
			if d.Version == "" {
				d.Version = binaryVersion(path)
			}
		}
	}
	for _, ps := range h.DetectPaths {
		if tmpl := ps.template(); tmpl != "" {
			path := env.expand(tmpl)
			if _, err := os.Stat(path); err == nil {
				d.Detected = true
				d.Evidence = append(d.Evidence, "path "+path)
			}
		}
	}
	for _, ext := range h.Extensions {
		if dir, ver := findExtension(env, ext); dir != "" {
			d.Detected = true
			d.Evidence = append(d.Evidence, "extension "+dir)
			if d.Version == "" {
				d.Version = ver
			}
		}
	}
	return d
}
