package integrate

import (
	"fmt"
	"strings"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeLocal   Scope = "local"
)

func ParseScope(s string) (Scope, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "user", "global":
		return ScopeUser, nil
	case "project", "workspace":
		return ScopeProject, nil
	case "local":
		return ScopeLocal, nil
	}
	return "", fmt.Errorf("unknown scope %q (want user, project or local)", s)
}

type Format string

const (
	FormatJSON          Format = "json"
	FormatJSONC         Format = "jsonc"
	FormatTOML          Format = "toml"
	FormatGooseYAML     Format = "goose-yaml"
	FormatContinueYAML  Format = "continue-yaml"
	FormatContinueBlock Format = "continue-block"
)

type Dialect string

const (
	DialectClaude     Dialect = "claude"
	DialectPlain      Dialect = "plain"
	DialectTypedStdio Dialect = "typed-stdio"
	DialectVSCode     Dialect = "vscode"
	DialectCopilot    Dialect = "copilot"
	DialectGemini     Dialect = "gemini"
	DialectCodex      Dialect = "codex"
	DialectOpenCode   Dialect = "opencode"
	DialectKilo       Dialect = "kilo"
	DialectZed        Dialect = "zed"
	DialectCline      Dialect = "cline"
	DialectRoo        Dialect = "roo"
	DialectKiro       Dialect = "kiro"
	DialectGoose      Dialect = "goose"
	DialectContinue   Dialect = "continue"
)

type Target struct {
	Paths        []PathSpec
	Format       Format
	KeyPath      []string
	Fanout       bool
	RequireExist bool
	Portable     bool
	NewFile      string
	Note         string
}

type SkillSpec struct {
	Install    PathSpec
	Reads      []PathSpec
	ProjectDir string
}

type Limits struct {
	InstalledMaxWaitMS int
	DetectedMaxWaitMS  int
}

type Harness struct {
	ID          string
	DisplayName string
	Tier        int
	DocsURL     string
	Binaries    []string
	DetectPaths []PathSpec
	Extensions  []string
	Scopes      map[Scope]Target
	Dialect     Dialect
	Limits      Limits
	Skills      *SkillSpec
	RestartHint string
	ManualSteps string
	SnippetKey  []string
	CLIAdd      func(ServerSpec) string
	Deeplink    func(ServerSpec) string
}

func (h Harness) ScopeList() []Scope {
	var out []Scope
	for _, s := range []Scope{ScopeUser, ScopeProject, ScopeLocal} {
		if _, ok := h.Scopes[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

const DefaultMaxWaitMS = 50000

func WaitLimit(id string, explicit bool) int {
	h, ok := Lookup(id)
	if !ok {
		return DefaultMaxWaitMS
	}
	v := h.Limits.DetectedMaxWaitMS
	if explicit {
		v = h.Limits.InstalledMaxWaitMS
	}
	if v <= 0 {
		return DefaultMaxWaitMS
	}
	return v
}
