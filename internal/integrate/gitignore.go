package integrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type IgnoreEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
	Exists bool   `json:"exists"`
}

type GitignoreStatus struct {
	Repo       bool          `json:"repo"`
	RepoRoot   string        `json:"repo_root,omitempty"`
	Gitignore  string        `json:"gitignore,omitempty"`
	Missing    []IgnoreEntry `json:"missing"`
	Tracked    []string      `json:"tracked,omitempty"`
	Dismissed  []string      `json:"dismissed,omitempty"`
	Suggestion string        `json:"suggestion,omitempty"`
}

const gitignoreHeader = "# jack-ketch"

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		return strings.TrimSpace(out.String()), fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func gitRoot(dir string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := gitOut(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || out == "" {
		return "", false
	}
	return out, true
}

func ignored(root, rel string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "check-ignore", "-q", "--no-index", rel)
	cmd.Dir = root
	return cmd.Run() == nil
}

func tracked(root, rel string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := gitOut(ctx, root, "ls-files", "--", rel)
	return err == nil && out != ""
}

var exampleEnvSuffixes = []string{".example", ".sample", ".template", ".dist", ".defaults"}

func isExampleEnv(name string) bool {
	low := strings.ToLower(filepath.Base(name))
	for _, s := range exampleEnvSuffixes {
		if strings.HasSuffix(low, s) {
			return true
		}
	}
	return strings.HasPrefix(low, "example.") || strings.Contains(low, ".example.")
}

func jackKetchArtifacts(workspace string) []IgnoreEntry {
	out := []IgnoreEntry{{Path: ".jack-ketch/", Kind: "state", Reason: "jack-ketch local state (audit log, log archive)"}}
	seen := map[string]bool{".jack-ketch/": true}
	for _, h := range Harnesses() {
		if t, ok := h.Scopes[ScopeProject]; ok {
			for _, ps := range t.Paths {
				tmpl := ps.template()
				if !strings.HasPrefix(tmpl, "{project}/") {
					continue
				}
				rel := strings.TrimPrefix(tmpl, "{project}/")
				if seen[rel] {
					continue
				}
				data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(rel)))
				if err != nil || !bytes.Contains(data, []byte("jack-ketch")) {
					continue
				}
				seen[rel] = true
				reason := "MCP client config written by jack-ketch integrate"
				if !strings.Contains(rel, "jack-ketch") {
					reason += " (may also hold other tools' settings)"
				}
				out = append(out, IgnoreEntry{Path: rel, Kind: "integration", Reason: reason})
			}
		}
		if h.Skills != nil && h.Skills.ProjectDir != "" {
			dir := filepath.Join(workspace, filepath.FromSlash(h.Skills.ProjectDir))
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() || !strings.HasPrefix(e.Name(), "jack-ketch") {
					continue
				}
				rel := filepath.ToSlash(filepath.Join(h.Skills.ProjectDir, e.Name())) + "/"
				if seen[rel] {
					continue
				}
				seen[rel] = true
				out = append(out, IgnoreEntry{Path: rel, Kind: "skill", Reason: "agent skill installed by jack-ketch integrate skills"})
			}
		}
	}
	return out
}

func CheckGitignore(workspace string, envFiles []string, dismissed []string) GitignoreStatus {
	st := GitignoreStatus{Missing: []IgnoreEntry{}}
	root, ok := gitRoot(workspace)
	if !ok {
		return st
	}
	st.Repo = true
	st.RepoRoot = root
	st.Gitignore = filepath.Join(root, ".gitignore")
	skip := map[string]bool{}
	for _, d := range dismissed {
		skip[d] = true
	}
	st.Dismissed = append([]string(nil), dismissed...)
	candidates := jackKetchArtifacts(workspace)
	for _, f := range envFiles {
		f = filepath.ToSlash(filepath.Clean(f))
		if f == "" || f == "." || isExampleEnv(f) || strings.HasPrefix(f, "../") || filepath.IsAbs(f) {
			continue
		}
		candidates = append(candidates, IgnoreEntry{Path: f, Kind: "env", Reason: "env file loaded by a saved app; usually holds secrets"})
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		abs := filepath.Join(workspace, filepath.FromSlash(strings.TrimSuffix(c.Path, "/")))
		rel, err := filepath.Rel(root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(c.Path, "/") {
			rel += "/"
		}
		if seen[rel] || skip[rel] {
			continue
		}
		seen[rel] = true
		_, statErr := os.Stat(abs)
		c.Exists = statErr == nil
		if c.Kind != "state" && !c.Exists {
			continue
		}
		if ignored(root, strings.TrimSuffix(rel, "/")) {
			continue
		}
		c.Path = rel
		if c.Exists && tracked(root, strings.TrimSuffix(rel, "/")) {
			st.Tracked = append(st.Tracked, rel)
		}
		st.Missing = append(st.Missing, c)
	}
	sort.SliceStable(st.Missing, func(i, j int) bool { return kindRank(st.Missing[i].Kind) < kindRank(st.Missing[j].Kind) })
	if len(st.Missing) > 0 {
		paths := make([]string, 0, len(st.Missing))
		for _, m := range st.Missing {
			paths = append(paths, m.Path)
		}
		st.Suggestion = "Ask the user whether to add these to .gitignore so git does not track jack-ketch files: " + strings.Join(paths, ", ") + ". On yes call update_gitignore; on no call update_gitignore(action=\"dismiss\") so they are not suggested again."
		if len(st.Tracked) > 0 {
			st.Suggestion += " Already committed (ignoring alone will not untrack them; the user can run git rm --cached on them): " + strings.Join(st.Tracked, ", ") + "."
		}
	}
	return st
}

func kindRank(k string) int {
	switch k {
	case "env":
		return 0
	case "state":
		return 1
	case "skill":
		return 2
	}
	return 3
}

func AppendGitignore(repoRoot string, entries []string) ([]string, error) {
	if repoRoot == "" {
		return nil, errors.New("not a git repository")
	}
	path := filepath.Join(repoRoot, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var added []string
	for _, e := range entries {
		e = strings.TrimSpace(filepath.ToSlash(e))
		if e == "" || strings.HasPrefix(e, "#") || strings.Contains(e, "\n") || strings.HasPrefix(e, "../") {
			continue
		}
		line := e
		if !strings.HasPrefix(line, "/") && !strings.HasPrefix(line, "*") {
			line = "/" + line
		}
		if have[line] || have[e] || have[strings.TrimSuffix(e, "/")] || have["/"+strings.TrimSuffix(e, "/")] {
			continue
		}
		have[line] = true
		added = append(added, line)
	}
	if len(added) == 0 {
		return nil, nil
	}
	var b strings.Builder
	b.Write(data)
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		b.WriteString("\n")
	}
	if !have[gitignoreHeader] {
		if len(data) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(gitignoreHeader + "\n")
	}
	for _, l := range added {
		b.WriteString(l + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	return added, nil
}
