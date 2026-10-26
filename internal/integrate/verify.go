package integrate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type VerifyCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Path   string `json:"path,omitempty"`
}

func lastJSONLine(out []byte) []byte {
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		l := bytes.TrimSpace(lines[i])
		if len(l) > 0 && l[0] == '{' {
			return l
		}
	}
	return nil
}

func Verify(ctx context.Context, id string, scope Scope, opts Options) ([]VerifyCheck, error) {
	entries, err := Inspect(id, scope, opts)
	if err != nil {
		return nil, err
	}
	var checks []VerifyCheck
	for _, e := range entries {
		checks = append(checks, VerifyCheck{Name: "config", OK: e.Installed, Detail: map[bool]string{true: "jack-ketch entry present", false: "jack-ketch entry missing"}[e.Installed], Path: e.Path})
		if !e.Installed {
			continue
		}
		checks = append(checks, VerifyCheck{Name: "command", OK: e.CommandFound, Detail: e.Command, Path: e.Path})
		if len(e.Problems) > 0 {
			checks = append(checks, VerifyCheck{Name: "entry", OK: false, Detail: strings.Join(e.Problems, "; "), Path: e.Path})
		}
		if !e.CommandFound {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		args := append(append([]string{}, e.Args...), "--check")
		cmd := exec.CommandContext(cctx, e.Command, args...)
		cmd.Env = os.Environ()
		cmd.WaitDelay = time.Second
		out, runErr := cmd.Output()
		cancel()
		var res struct {
			OK            bool   `json:"ok"`
			Error         string `json:"error"`
			Tools         int    `json:"tools"`
			DaemonVersion string `json:"daemonVersion"`
		}
		if line := lastJSONLine(out); line == nil || json.Unmarshal(line, &res) != nil {
			detail := strings.TrimSpace(string(out))
			if runErr != nil {
				detail = strings.TrimSpace(runErr.Error() + " " + detail)
			}
			checks = append(checks, VerifyCheck{Name: "handshake", OK: false, Detail: detail, Path: e.Path})
			continue
		}
		detail := fmt.Sprintf("%d tools, daemon %s", res.Tools, res.DaemonVersion)
		if !res.OK {
			detail = res.Error
		}
		checks = append(checks, VerifyCheck{Name: "handshake", OK: res.OK, Detail: detail, Path: e.Path})
	}
	return checks, nil
}
