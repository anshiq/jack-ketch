package integrate

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func frontmatterMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("missing frontmatter")
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		t.Fatal("unterminated frontmatter")
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end]), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestEmbeddedSkills(t *testing.T) {
	setTestEnv(t)
	skills := EmbeddedSkills()
	if len(skills) != 3 {
		t.Fatalf("got %d skills, want 3", len(skills))
	}
	nameRe := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	entries, err := skillFS.ReadDir(skillRoot)
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			dirs[e.Name()] = true
		}
	}
	for _, sk := range skills {
		if sk.Version == "" || sk.Description == "" {
			t.Errorf("%s: version=%q description=%q", sk.Name, sk.Version, sk.Description)
		}
		if !nameRe.MatchString(sk.Name) || !dirs[sk.Name] {
			t.Errorf("bad skill name %q", sk.Name)
		}
		data, err := skillFS.ReadFile(path.Join(skillRoot, sk.Name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		fm := frontmatterMap(t, data)
		if _, ok := fm["version"]; ok {
			t.Errorf("%s: top-level version key still present", sk.Name)
		}
		if fm["name"] != sk.Name {
			t.Errorf("%s: frontmatter name = %v", sk.Name, fm["name"])
		}
	}
}

func TestSkillsInstallFlow(t *testing.T) {
	base := setTestEnv(t)
	home := filepath.Join(base, "home")
	states, err := InstallSkills("claude-code", ScopeUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 {
		t.Fatalf("got %d states", len(states))
	}
	for _, st := range states {
		if !st.Visible || st.Outdated || !st.Own || st.Shared {
			t.Errorf("%s: visible=%v outdated=%v own=%v shared=%v", st.Name, st.Visible, st.Outdated, st.Own, st.Shared)
		}
		if st.Description == "" {
			t.Errorf("%s: empty description", st.Name)
		}
		dir := filepath.Join(home, ".claude", "skills", st.Name)
		if st.Dir != dir || st.Target != dir {
			t.Errorf("%s: dir=%q target=%q want %q", st.Name, st.Dir, st.Target, dir)
		}
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
			t.Errorf("%s: SKILL.md missing: %v", st.Name, err)
		}
		if _, err := os.Stat(filepath.Join(dir, skillMarker)); err != nil {
			t.Errorf("%s: marker missing: %v", st.Name, err)
		}
	}
	if _, err := InstallSkills("cursor", ScopeUser, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor")); err == nil {
		t.Error("cursor install wrote files despite skills being visible via ~/.claude/skills")
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); err == nil {
		t.Error("cursor install wrote to ~/.agents")
	}
	if _, err := InstallSkills("codex", ScopeUser, ""); err != nil {
		t.Fatal(err)
	}
	for _, sk := range EmbeddedSkills() {
		if _, err := os.Stat(filepath.Join(home, ".agents", "skills", sk.Name, "SKILL.md")); err != nil {
			t.Errorf("codex %s: %v", sk.Name, err)
		}
	}
	marker := filepath.Join(home, ".claude", "skills", states[0].Name, skillMarker)
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["version"] = "1"
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	updates := CheckSkillUpdates(ScopeUser, "")
	found := false
	for _, u := range updates {
		if u.Name == states[0].Name && u.Have == "1" {
			found = true
		}
	}
	if !found {
		t.Errorf("CheckSkillUpdates did not report the edited marker: %+v", updates)
	}
	removed, err := RemoveSkills("codex", ScopeUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 3 {
		t.Errorf("removed %d dirs, want 3", len(removed))
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", states[0].Name, "SKILL.md")); err == nil {
		t.Error("codex skills not removed")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", states[0].Name, "SKILL.md")); err != nil {
		t.Errorf("claude-code skills were removed too: %v", err)
	}
}

func TestSkillsSharedCopyAndRemove(t *testing.T) {
	base := setTestEnv(t)
	home := filepath.Join(base, "home")
	if _, err := InstallSkills("claude-code", ScopeUser, ""); err != nil {
		t.Fatal(err)
	}
	states, err := SkillStatus("opencode", ScopeUser, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range states {
		if !st.Visible || st.Own || !st.Shared {
			t.Errorf("%s: visible=%v own=%v shared=%v", st.Name, st.Visible, st.Own, st.Shared)
		}
		want := filepath.Join(home, ".claude", "skills", st.Name)
		if st.Dir != want || st.Target != want {
			t.Errorf("%s: dir=%q target=%q want %q", st.Name, st.Dir, st.Target, want)
		}
	}
	removed, err := RemoveSkills("opencode", ScopeUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Errorf("removed shared dirs: %v", removed)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", states[0].Name, "SKILL.md")); err != nil {
		t.Errorf("shared copy removed: %v", err)
	}
	marker := filepath.Join(home, ".claude", "skills", states[0].Name, skillMarker)
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["version"] = "1"
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if updates := CheckSkillUpdates(ScopeUser, ""); len(updates) == 0 {
		t.Error("CheckSkillUpdates did not report the shared copy")
	}
	states, err = InstallSkills("opencode", ScopeUser, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range states {
		if st.Outdated {
			t.Errorf("%s still outdated after shared update", st.Name)
		}
	}
	removed, err = RemoveSkills("claude-code", ScopeUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 3 {
		t.Errorf("removed %d owned dirs, want 3", len(removed))
	}
}

func TestSkillUpdatesProjectScope(t *testing.T) {
	base := setTestEnv(t)
	project := filepath.Join(base, "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	states, err := InstallSkills("claude-code", ScopeProject, project)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(project, ".claude", "skills", states[0].Name, skillMarker)
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["version"] = "1"
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if updates := CheckSkillUpdates(ScopeProject, project); len(updates) == 0 {
		t.Error("project updates missing")
	}
	if updates := CheckSkillUpdates(ScopeUser, ""); len(updates) != 0 {
		t.Errorf("user updates leaked project state: %+v", updates)
	}
}

func TestSkillsLocalScopeRejected(t *testing.T) {
	setTestEnv(t)
	if _, err := SkillStatus("claude-code", ScopeLocal, ""); err == nil {
		t.Fatal("expected local scope error")
	}
	if _, err := InstallSkills("claude-code", ScopeLocal, ""); err == nil {
		t.Fatal("expected local scope install error")
	}
	if _, err := SkillStatus("claude-code", ScopeProject, ""); err == nil {
		t.Fatal("expected missing project error")
	}
}
