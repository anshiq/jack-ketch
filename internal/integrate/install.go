package integrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"
)

type Options struct {
	ProjectDir string
	Command    string
}

type Change struct {
	Harness     string `json:"harness"`
	Scope       Scope  `json:"scope"`
	Path        string `json:"path"`
	Existed     bool   `json:"existed"`
	Changed     bool   `json:"changed"`
	Diff        string `json:"diff,omitempty"`
	Backup      string `json:"backup,omitempty"`
	RestartHint string `json:"restartHint,omitempty"`
	Note        string `json:"note,omitempty"`
	before      []byte
	after       []byte
	deleteFile  bool
}

var errMissingConfig = errors.New("config file not found")

func targetFor(id string, scope Scope, opts Options) (Harness, Target, string, error) {
	h, ok := Lookup(id)
	if !ok {
		return Harness{}, Target{}, "", fmt.Errorf("unknown agent %q (run `jack-ketch integrate list`)", id)
	}
	if h.Tier != 1 {
		return h, Target{}, "", fmt.Errorf("%s needs manual setup: %s", h.DisplayName, h.ManualSteps)
	}
	t, ok := h.Scopes[scope]
	if !ok {
		names := make([]string, 0, len(h.Scopes))
		for _, s := range h.ScopeList() {
			names = append(names, string(s))
		}
		return h, Target{}, "", fmt.Errorf("%s does not support %s scope (supported: %s)", h.DisplayName, scope, strings.Join(names, ", "))
	}
	project := ""
	if scope != ScopeUser {
		if opts.ProjectDir == "" {
			return h, t, "", fmt.Errorf("%s scope needs a project directory", scope)
		}
		abs, err := filepath.Abs(opts.ProjectDir)
		if err != nil {
			return h, t, "", err
		}
		project = abs
	}
	return h, t, project, nil
}

func candidatePaths(t Target, env pathEnv) []string {
	var out []string
	for _, ps := range t.Paths {
		if tmpl := ps.template(); tmpl != "" {
			out = append(out, env.expand(tmpl))
		}
	}
	return out
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func resolveTargetPaths(t Target, env pathEnv, remove bool) ([]string, error) {
	cands := candidatePaths(t, env)
	if len(cands) == 0 {
		return nil, errors.New("no config location for this operating system")
	}
	var existing []string
	for _, c := range cands {
		if fileExists(c) {
			existing = append(existing, c)
		}
	}
	if remove {
		if len(existing) == 0 {
			return cands[:1], nil
		}
		if t.Fanout {
			return existing, nil
		}
		return existing[:1], nil
	}
	if t.Fanout {
		var out []string
		for _, c := range cands {
			if fileExists(c) || (!t.RequireExist && dirExists(filepath.Dir(c))) {
				out = append(out, c)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
		if t.RequireExist {
			return nil, errMissingConfig
		}
		return cands[:1], nil
	}
	if len(existing) > 0 {
		return existing[:1], nil
	}
	if t.RequireExist {
		return nil, errMissingConfig
	}
	for _, c := range cands {
		if dirExists(filepath.Dir(c)) {
			return []string{c}, nil
		}
	}
	return cands[:1], nil
}

func expandKeyPath(path []string, project string) []string {
	out := make([]string, len(path))
	for i, k := range path {
		if k == "{project}" {
			k = project
		}
		out[i] = k
	}
	return out
}

func serverSpec(h Harness, t Target, opts Options) (ServerSpec, error) {
	cmd := opts.Command
	if cmd == "" {
		var err error
		if cmd, err = ResolveServerCommand(t.Portable); err != nil {
			return ServerSpec{}, err
		}
	}
	return ServerSpec{Command: cmd, Args: []string{"serve", "--harness", h.ID}}, nil
}

func finishJSON(out []byte, f Format) ([]byte, error) {
	if f == FormatJSON && !json.Valid(out) {
		return hujson.Standardize(out)
	}
	return out, nil
}

func upsertEntry(h Harness, t Target, base []byte, keyPath []string, spec ServerSpec) ([]byte, error) {
	switch t.Format {
	case FormatJSON, FormatJSONC:
		doc, err := parseJSONC(base)
		if err != nil {
			return nil, err
		}
		if _, err := doc.upsert(keyPath, ServerName, jsonEntry(h.Dialect, spec)); err != nil {
			return nil, err
		}
		return finishJSON(doc.bytes(), t.Format)
	case FormatTOML:
		return tomlUpsert(base, codexBlock(spec))
	case FormatGooseYAML:
		return yamlUpsertMap(base, "extensions", ServerName, gooseEntry(spec))
	case FormatContinueYAML:
		return yamlUpsertList(base, "mcpServers", ServerName, continueEntry(spec))
	case FormatContinueBlock:
		return continueBlockFile(spec)
	}
	return nil, fmt.Errorf("unsupported config format %s", t.Format)
}

func removeEntry(t Target, before []byte, keyPath []string) ([]byte, bool, error) {
	switch t.Format {
	case FormatJSON, FormatJSONC:
		doc, err := parseJSONC(before)
		if err != nil {
			return nil, false, err
		}
		changed, err := doc.remove(keyPath, ServerName)
		if err != nil || !changed {
			return before, false, err
		}
		out, err := finishJSON(doc.bytes(), t.Format)
		return out, false, err
	case FormatTOML:
		out, err := tomlRemove(before)
		return out, false, err
	case FormatGooseYAML:
		out, err := yamlRemoveMap(before, "extensions", ServerName)
		return out, false, err
	case FormatContinueYAML:
		out, err := yamlRemoveList(before, "mcpServers", ServerName)
		return out, false, err
	case FormatContinueBlock:
		return nil, true, nil
	}
	return nil, false, fmt.Errorf("unsupported config format %s", t.Format)
}

func plan(id string, scope Scope, opts Options, remove bool) ([]Change, error) {
	h, t, project, err := targetFor(id, scope, opts)
	if err != nil {
		return nil, err
	}
	env := currentPathEnv(project)
	paths, err := resolveTargetPaths(t, env, remove)
	if errors.Is(err, errMissingConfig) {
		return nil, fmt.Errorf("%s: %s", h.DisplayName, h.ManualSteps)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", h.DisplayName, err)
	}
	var spec ServerSpec
	if !remove {
		if spec, err = serverSpec(h, t, opts); err != nil {
			return nil, err
		}
	}
	keyPath := expandKeyPath(t.KeyPath, project)
	out := make([]Change, 0, len(paths))
	for _, path := range paths {
		before, existed, err := readConfig(path)
		if err != nil {
			return nil, err
		}
		c := Change{Harness: h.ID, Scope: scope, Path: path, Existed: existed, before: before, RestartHint: h.RestartHint, Note: t.Note}
		if remove {
			if !existed {
				out = append(out, c)
				continue
			}
			after, del, err := removeEntry(t, before, keyPath)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			c.after, c.deleteFile = after, del
			c.Changed = del || !bytes.Equal(before, after)
		} else {
			base := before
			if !existed && t.NewFile != "" {
				base = []byte(t.NewFile)
			}
			after, err := upsertEntry(h, t, base, keyPath, spec)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			c.after = after
			c.Changed = !existed || !bytes.Equal(before, after)
		}
		if c.Changed {
			c.Diff = unifiedDiff(path, before, c.after)
		}
		out = append(out, c)
	}
	return out, nil
}

func PlanInstall(id string, scope Scope, opts Options) ([]Change, error) {
	return plan(id, scope, opts, false)
}

func PlanRemove(id string, scope Scope, opts Options) ([]Change, error) {
	return plan(id, scope, opts, true)
}

func Install(id string, scope Scope, opts Options) ([]Change, error) {
	return apply(id, scope, opts, false)
}

func Remove(id string, scope Scope, opts Options) ([]Change, error) {
	return apply(id, scope, opts, true)
}

func apply(id string, scope Scope, opts Options, remove bool) ([]Change, error) {
	for attempt := 0; attempt < 3; attempt++ {
		changes, err := plan(id, scope, opts, remove)
		if err != nil {
			return nil, err
		}
		conflict := false
		for i := range changes {
			c := &changes[i]
			if !c.Changed {
				continue
			}
			cur, _, err := readConfig(c.Path)
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(cur, c.before) {
				conflict = true
				break
			}
			if c.Existed {
				b, err := writeBackup(c.Path, c.before)
				if err != nil {
					return nil, err
				}
				c.Backup = b
			}
			if c.deleteFile {
				if err := os.Remove(c.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return nil, err
				}
				continue
			}
			if err := writeConfig(c.Path, c.after); err != nil {
				return nil, err
			}
		}
		if !conflict {
			return changes, nil
		}
	}
	return nil, errors.New("the config file kept changing while jack-ketch was writing it; close the agent and retry")
}
