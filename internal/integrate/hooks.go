package integrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const claudeHookMarker = "hook claude-pretooluse"

func claudeSettingsPath(scope Scope, project string) (string, error) {
	switch scope {
	case ScopeUser:
		return filepath.Join(currentPathEnv("").home, ".claude", "settings.json"), nil
	case ScopeProject:
		if project == "" {
			return "", errors.New("project scope needs a project directory")
		}
		return filepath.Join(project, ".claude", "settings.json"), nil
	}
	return "", fmt.Errorf("the Claude Code hook supports user and project scope, not %s", scope)
}

func isOurHookGroup(g any) bool {
	b, err := json.Marshal(g)
	return err == nil && strings.Contains(string(b), claudeHookMarker)
}

func PlanClaudeHook(scope Scope, project string, remove bool) (Change, error) {
	path, err := claudeSettingsPath(scope, project)
	if err != nil {
		return Change{}, err
	}
	before, existed, err := readConfig(path)
	if err != nil {
		return Change{}, err
	}
	c := Change{Harness: "claude-code", Scope: scope, Path: path, Existed: existed, before: before, RestartHint: "Claude Code loads hook changes in new sessions; run /hooks to review them."}
	if remove && !existed {
		return c, nil
	}
	doc, err := parseJSONC(before)
	if err != nil {
		return Change{}, err
	}
	var groups []any
	raw, ok, err := doc.get([]string{"hooks"}, "PreToolUse")
	if err != nil {
		return Change{}, err
	}
	if ok {
		if err := json.Unmarshal(raw, &groups); err != nil {
			return Change{}, fmt.Errorf("hooks.PreToolUse is not an array: %w", err)
		}
	}
	kept := make([]any, 0, len(groups)+1)
	for _, g := range groups {
		if !isOurHookGroup(g) {
			kept = append(kept, g)
		}
	}
	if !remove {
		cmd := "jack-ketch"
		if scope == ScopeUser {
			if cmd, err = ResolveServerCommand(false); err != nil {
				return Change{}, err
			}
		}
		kept = append(kept, orderedObject{{"matcher", "Bash"}, {"hooks", []any{orderedObject{{"type", "command"}, {"command", shellQuote(cmd) + " " + claudeHookMarker}}}}})
	}
	if len(kept) == 0 {
		if ok {
			if _, err := doc.remove([]string{"hooks"}, "PreToolUse"); err != nil {
				return Change{}, err
			}
		}
	} else if _, err := doc.upsert([]string{"hooks"}, "PreToolUse", kept); err != nil {
		return Change{}, err
	}
	after, err := finishJSON(doc.bytes(), FormatJSON)
	if err != nil {
		return Change{}, err
	}
	c.after = after
	c.Changed = (!existed && !remove) || (existed && !bytes.Equal(before, after))
	if c.Changed {
		c.Diff = unifiedDiff(path, before, after)
	}
	return c, nil
}

func ApplyClaudeHook(scope Scope, project string, remove bool) (Change, error) {
	c, err := PlanClaudeHook(scope, project, remove)
	if err != nil || !c.Changed {
		return c, err
	}
	if c.Existed {
		b, err := writeBackup(c.Path, c.before)
		if err != nil {
			return c, err
		}
		c.Backup = b
	}
	return c, writeConfig(c.Path, c.after)
}
