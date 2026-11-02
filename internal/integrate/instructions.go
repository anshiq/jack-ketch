package integrate

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	instructionsStart = "<!-- JACK-KETCH_START -->"
	instructionsEnd   = "<!-- JACK-KETCH_END -->"
	instructionsBody  = `## jack-ketch

jack-ketch runs and supervises this project's development processes. Use its MCP tools for dev servers, watchers, workers, and app-tied builds, tests and migrations; a process started in your shell has no captured logs, readiness detection, health checks or restart supervision.

1. Call list_processes first; re-attach to a running instance instead of starting a duplicate.
2. Call list_apps, then start_process(app=..., mode=...); keep the returned process_id.
3. Confirm readiness with wait_for_log(process_id, ready=true) or start_process(..., timeout_ms=...).
4. On failure use process_status, get_logs, search_logs and diagnose_process; restart with restart_process.
5. After a change use probe or run_checks to verify the running behaviour.
6. When a result carries save_suggestion, ask the user before saving it as an app mode.
7. For authenticated endpoints call list_credentials and use probe (the default profile is attached automatically, or credential=<name>); never ask for or look for secret values. If a result carries credential_hint, tell the user to update the profile on the web UI's Credentials page.

Do not use jack-ketch for one-off shell commands (ls, cat, grep, git, docker, kubectl, package managers); use your own tools for those.`
)

func instructionBlock() string {
	return instructionsStart + "\n\n" + instructionsBody + "\n\n" + instructionsEnd + "\n"
}

func findInstructionBlock(text string, from int) (int, int, bool, error) {
	startRel := strings.Index(text[from:], instructionsStart)
	endRel := strings.Index(text[from:], instructionsEnd)
	if startRel < 0 {
		if endRel >= 0 {
			return 0, 0, false, fmt.Errorf("found %s without %s", instructionsEnd, instructionsStart)
		}
		return 0, 0, false, nil
	}
	if endRel >= 0 && endRel < startRel {
		return 0, 0, false, fmt.Errorf("found %s without %s", instructionsEnd, instructionsStart)
	}
	start := from + startRel
	after := start + len(instructionsStart)
	rel := strings.Index(text[after:], instructionsEnd)
	if rel < 0 {
		return 0, 0, false, fmt.Errorf("found %s without %s", instructionsStart, instructionsEnd)
	}
	return start, after + rel + len(instructionsEnd), true, nil
}

func joinInstructionText(left, right string) string {
	trimmedLeft := strings.TrimRight(left, "\n")
	trimmedRight := strings.TrimLeft(right, "\n")
	switch {
	case trimmedLeft == "":
		return trimmedRight
	case trimmedRight == "":
		return trimmedLeft + "\n"
	}
	gap := max(len(left)-len(trimmedLeft), len(right)-len(trimmedRight), 1)
	return trimmedLeft + strings.Repeat("\n", gap) + trimmedRight
}

func upsertInstructionBlock(text string) (string, error) {
	start, end, found, err := findInstructionBlock(text, 0)
	if err != nil {
		return "", err
	}
	block := instructionBlock()
	if !found {
		text = strings.TrimRight(text, "\n")
		if text == "" {
			return block, nil
		}
		return text + "\n\n" + block, nil
	}
	tail := text[end:]
	rest, _, err := stripInstructionBlocks(tail)
	if err != nil {
		return "", err
	}
	text = text[:start] + strings.TrimSuffix(block, "\n") + rest
	if tail != "" && rest == "" {
		text += "\n"
	}
	return text, nil
}

func stripInstructionBlocks(text string) (string, bool, error) {
	var ranges [][2]int
	pos := 0
	for {
		start, end, found, err := findInstructionBlock(text, pos)
		if err != nil {
			return "", false, err
		}
		if !found {
			break
		}
		ranges = append(ranges, [2]int{start, end})
		pos = end
	}
	if len(ranges) == 0 {
		return text, false, nil
	}
	for i := len(ranges) - 1; i >= 0; i-- {
		text = joinInstructionText(text[:ranges[i][0]], text[ranges[i][1]:])
	}
	return text, true, nil
}

func expandPaths(specs []PathSpec, env pathEnv) []string {
	var out []string
	for _, ps := range specs {
		if tmpl := ps.template(); tmpl != "" {
			out = append(out, env.expand(tmpl))
		}
	}
	return out
}

func instructionPath(spec *InstructionSpec, env pathEnv, remove bool) (string, bool) {
	primary := expandPaths(spec.Paths, env)
	for _, c := range primary {
		if fileExists(c) {
			return c, false
		}
	}
	if !remove {
		for _, c := range expandPaths(spec.Fallback, env) {
			if fileExists(c) {
				return c, true
			}
		}
	}
	for _, c := range primary {
		if dirExists(filepath.Dir(c)) {
			return c, false
		}
	}
	if len(primary) > 0 {
		return primary[0], false
	}
	return "", false
}

type InstructionReport struct {
	Path       string `json:"path"`
	FileExists bool   `json:"fileExists"`
	Installed  bool   `json:"installed"`
	UpToDate   bool   `json:"upToDate"`
	Shared     bool   `json:"shared,omitempty"`
}

func InstructionStatus(h Harness, env pathEnv) *InstructionReport {
	if h.Instructions == nil {
		return nil
	}
	path, shared := instructionPath(h.Instructions, env, false)
	if path == "" {
		return nil
	}
	r := &InstructionReport{Path: path, Shared: shared}
	data, existed, err := readConfig(path)
	if err != nil || !existed {
		return r
	}
	r.FileExists = true
	text := string(data)
	start, end, found, err := findInstructionBlock(text, 0)
	if err != nil || !found {
		return r
	}
	r.Installed = true
	r.UpToDate = text[start:end] == strings.TrimSuffix(instructionBlock(), "\n")
	return r
}

func planInstructions(h Harness, env pathEnv, remove bool) ([]Change, error) {
	if h.Instructions == nil {
		return nil, nil
	}
	path, _ := instructionPath(h.Instructions, env, remove)
	if path == "" {
		return nil, nil
	}
	before, existed, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	var after []byte
	if remove {
		text, removed, err := stripInstructionBlocks(string(before))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if !removed {
			return nil, nil
		}
		after = []byte(text)
	} else {
		text, err := upsertInstructionBlock(string(before))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		after = []byte(text)
	}
	if !existed || !bytes.Equal(before, after) {
		c := Change{Harness: h.ID, Scope: ScopeUser, Path: path, Existed: existed, Changed: true, before: before, after: after}
		c.Diff = unifiedDiff(path, before, after)
		return []Change{c}, nil
	}
	return nil, nil
}
