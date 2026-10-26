package integrate

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed all:skill
var skillFS embed.FS

const (
	skillRoot   = "skill"
	skillMarker = ".jack-ketch-skill.json"
)

type Skill struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type skillFrontmatter struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Version     any               `yaml:"version"`
	Metadata    map[string]string `yaml:"metadata"`
}

func parseFrontmatter(data []byte) (skillFrontmatter, bool) {
	var fm skillFrontmatter
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return fm, false
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return fm, false
	}
	if yaml.Unmarshal([]byte(rest[:end]), &fm) != nil {
		return fm, false
	}
	return fm, true
}

func (fm skillFrontmatter) version() string {
	if v := fm.Metadata["version"]; v != "" {
		return v
	}
	if fm.Version != nil {
		return fmt.Sprint(fm.Version)
	}
	return ""
}

func EmbeddedSkills() []Skill {
	entries, err := fs.ReadDir(skillFS, skillRoot)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := skillFS.ReadFile(path.Join(skillRoot, e.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		fm, _ := parseFrontmatter(data)
		out = append(out, Skill{Name: e.Name(), Version: fm.version(), Description: fm.Description})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func embeddedFiles(name string) (map[string][]byte, error) {
	root := path.Join(skillRoot, name)
	files := map[string][]byte{}
	err := fs.WalkDir(skillFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := skillFS.ReadFile(p)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(p, root+"/")] = data
		return nil
	})
	return files, err
}

func installSkillTree(dest string, sk Skill) error {
	files, err := embeddedFiles(sk.Name)
	if err != nil {
		return err
	}
	for rel, data := range files {
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if cur, err := os.ReadFile(target); err == nil && bytes.Equal(cur, data) {
			continue
		}
		if err := writeAtomic(target, data); err != nil {
			return err
		}
	}
	_ = filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dest, p)
		rel = filepath.ToSlash(rel)
		if rel == skillMarker {
			return nil
		}
		if _, ok := files[rel]; !ok {
			_ = os.Remove(p)
		}
		return nil
	})
	marker, _ := json.Marshal(map[string]any{"name": sk.Name, "version": sk.Version, "installedAt": time.Now().Unix()})
	return writeAtomic(filepath.Join(dest, skillMarker), append(marker, '\n'))
}

func installedSkillVersion(dir string) (string, bool) {
	if data, err := os.ReadFile(filepath.Join(dir, skillMarker)); err == nil {
		var m struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(data, &m) == nil {
			return m.Version, true
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return "", false
	}
	fm, _ := parseFrontmatter(data)
	return fm.version(), true
}

type skillLocation struct {
	install string
	reads   []string
}

func locateSkills(h Harness, scope Scope, project string) (skillLocation, error) {
	if h.Skills == nil {
		return skillLocation{}, fmt.Errorf("%s does not support Agent Skills", h.DisplayName)
	}
	switch scope {
	case ScopeProject:
		if project == "" {
			return skillLocation{}, errors.New("invalid_argument: project scope requires a project directory")
		}
		dir := filepath.Join(project, filepath.FromSlash(h.Skills.ProjectDir))
		return skillLocation{install: dir, reads: []string{dir}}, nil
	case ScopeLocal:
		return skillLocation{}, errors.New("invalid_argument: skills support user or project scope")
	}
	env := currentPathEnv("")
	loc := skillLocation{install: env.expand(h.Skills.Install.template())}
	for _, r := range h.Skills.Reads {
		if t := r.template(); t != "" {
			loc.reads = append(loc.reads, env.expand(t))
		}
	}
	return loc, nil
}

type SkillState struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Want        string `json:"want"`
	Have        string `json:"have,omitempty"`
	Dir         string `json:"dir,omitempty"`
	Target      string `json:"target"`
	Visible     bool   `json:"visible"`
	Own         bool   `json:"own"`
	Shared      bool   `json:"shared"`
	Outdated    bool   `json:"outdated"`
}

func samePath(a, b string) bool {
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func SkillStatus(id string, scope Scope, project string) ([]SkillState, error) {
	h, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", id)
	}
	loc, err := locateSkills(h, scope, project)
	if err != nil {
		return nil, err
	}
	var out []SkillState
	for _, sk := range EmbeddedSkills() {
		own := filepath.Join(loc.install, sk.Name)
		st := SkillState{Name: sk.Name, Description: sk.Description, Want: sk.Version, Target: own}
		for _, dir := range loc.reads {
			d := filepath.Join(dir, sk.Name)
			if v, ok := installedSkillVersion(d); ok {
				st.Visible, st.Dir, st.Have = true, d, v
				st.Target = d
				st.Own = samePath(d, own)
				st.Shared = !st.Own
				break
			}
		}
		st.Outdated = st.Visible && st.Have != sk.Version
		out = append(out, st)
	}
	return out, nil
}

func InstallSkills(id string, scope Scope, project string) ([]SkillState, error) {
	h, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", id)
	}
	loc, err := locateSkills(h, scope, project)
	if err != nil {
		return nil, err
	}
	states, err := SkillStatus(id, scope, project)
	if err != nil {
		return nil, err
	}
	byName := map[string]Skill{}
	for _, sk := range EmbeddedSkills() {
		byName[sk.Name] = sk
	}
	for i, st := range states {
		if st.Visible && !st.Outdated {
			continue
		}
		dest := filepath.Join(loc.install, st.Name)
		if st.Visible {
			dest = st.Dir
		}
		if err := installSkillTree(dest, byName[st.Name]); err != nil {
			return nil, err
		}
		states[i].Visible, states[i].Dir, states[i].Have, states[i].Outdated = true, dest, st.Want, false
		states[i].Target = dest
		states[i].Own = samePath(dest, filepath.Join(loc.install, st.Name))
		states[i].Shared = !states[i].Own
	}
	return states, nil
}

func RemoveSkills(id string, scope Scope, project string) ([]string, error) {
	states, err := SkillStatus(id, scope, project)
	if err != nil {
		return nil, err
	}
	var removed []string
	seen := map[string]bool{}
	for _, st := range states {
		if !st.Visible || !st.Own || seen[st.Dir] {
			continue
		}
		if err := os.RemoveAll(st.Dir); err != nil {
			return removed, err
		}
		seen[st.Dir] = true
		removed = append(removed, st.Dir)
	}
	return removed, nil
}

type SkillUpdate struct {
	Harness string `json:"harness"`
	Name    string `json:"name"`
	Have    string `json:"have"`
	Want    string `json:"want"`
	Dir     string `json:"dir"`
}

func CheckSkillUpdates(scope Scope, project string) []SkillUpdate {
	seen := map[string]bool{}
	var out []SkillUpdate
	for _, h := range Harnesses() {
		if h.Skills == nil {
			continue
		}
		states, err := SkillStatus(h.ID, scope, project)
		if err != nil {
			continue
		}
		for _, st := range states {
			if st.Outdated && !seen[st.Dir] {
				seen[st.Dir] = true
				out = append(out, SkillUpdate{Harness: h.ID, Name: st.Name, Have: st.Have, Want: st.Want, Dir: st.Dir})
			}
		}
	}
	return out
}
