package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"jack-ketch/internal/integrate"
	rtmcp "jack-ketch/internal/mcp"
	"jack-ketch/internal/project"
)

type integrateFlags struct {
	all      bool
	json     bool
	project  string
	scope    string
	dryRun   bool
	yes      bool
	skills   bool
	command  string
	detected bool
	agents   []string
}

func newIntegrateCmd() *cobra.Command {
	var write, remove, create, noVerify, yes bool
	var scope string
	cmd := &cobra.Command{
		Use:   "integrate [agent]",
		Short: "Inspect and install MCP client config and skills",
		Long: `Inspect, install and remove jack-ketch MCP client configuration and skills.

With no arguments this is the same as ` + "`integrate list`" + `; with agent names it keeps the
legacy behaviour: --write installs, --remove removes, neither prints the snippet.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				if write || remove {
					return errors.New("integrate --write/--remove needs an agent name (or use `jack-ketch integrate install <agent>`)")
				}
				return runIntegrateList(out, integrateFlags{})
			}
			return runIntegrateLegacy(out, args, integrateFlags{scope: scope, yes: yes}, write, remove)
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "install the jack-ketch entry (legacy)")
	cmd.Flags().BoolVar(&remove, "remove", false, "remove the jack-ketch entry (legacy)")
	cmd.Flags().StringVar(&scope, "scope", "", "user or project scope (legacy default: project for claude-code, user otherwise)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompts")
	cmd.Flags().BoolVar(&create, "create", false, "accepted for compatibility; ignored")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "accepted for compatibility; ignored")
	cmd.AddCommand(
		newIntegrateListCmd(),
		newIntegrateInstallCmd(),
		newIntegrateRemoveCmd(),
		newIntegrateShowCmd(),
		newIntegrateVerifyCmd(),
		newIntegrateSkillsCmd(),
		newIntegrateSkillCmd(),
		newIntegrateStatusCmd(),
		newIntegrateHooksCmd(),
	)
	return cmd
}

func newIntegrateListCmd() *cobra.Command {
	var fl integrateFlags
	cmd := &cobra.Command{
		Use:   "list [--all] [--json]",
		Short: "Table of harnesses, detection and connection status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIntegrateList(cmd.OutOrStdout(), fl)
		},
	}
	cmd.Flags().BoolVar(&fl.all, "all", false, "include undetected Tier-2 harnesses")
	cmd.Flags().BoolVar(&fl.json, "json", false, "print the full report as JSON")
	cmd.Flags().StringVar(&fl.project, "project", "", "project directory for project/local scopes")
	return cmd
}

func newIntegrateInstallCmd() *cobra.Command {
	var fl integrateFlags
	cmd := &cobra.Command{
		Use:   "install <agent>... | --detected",
		Short: "Install the jack-ketch MCP entry",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fl.agents = args
			return runIntegrateInstall(cmd.OutOrStdout(), fl)
		},
	}
	cmd.Flags().StringVar(&fl.scope, "scope", "", "user, project or local (default: user)")
	cmd.Flags().StringVar(&fl.project, "project", "", "project directory for project/local scopes")
	cmd.Flags().BoolVar(&fl.dryRun, "dry-run", false, "print the diff without writing")
	cmd.Flags().BoolVar(&fl.yes, "yes", false, "skip confirmation prompts")
	cmd.Flags().BoolVar(&fl.skills, "skills", false, "also install skills for harnesses that support them")
	cmd.Flags().StringVar(&fl.command, "command", "", "command to write into the config (default: this binary)")
	cmd.Flags().BoolVar(&fl.detected, "detected", false, "every detected Tier-1 harness")
	cmd.Flags().BoolVar(&fl.json, "json", false, "print per-agent results as JSON")
	return cmd
}

func newIntegrateRemoveCmd() *cobra.Command {
	var fl integrateFlags
	cmd := &cobra.Command{
		Use:   "remove <agent>...",
		Short: "Remove the jack-ketch MCP entry",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fl.agents = args
			return runIntegrateRemove(cmd.OutOrStdout(), fl)
		},
	}
	cmd.Flags().StringVar(&fl.scope, "scope", "", "user, project or local (default: user)")
	cmd.Flags().StringVar(&fl.project, "project", "", "project directory for project/local scopes")
	cmd.Flags().BoolVar(&fl.dryRun, "dry-run", false, "print the diff without writing")
	cmd.Flags().BoolVar(&fl.yes, "yes", false, "skip confirmation prompts")
	cmd.Flags().BoolVar(&fl.detected, "detected", false, "every detected Tier-1 harness")
	return cmd
}

func newIntegrateShowCmd() *cobra.Command {
	var fl integrateFlags
	cmd := &cobra.Command{
		Use:   "show <agent>",
		Short: "Print the config snippet for one harness",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fl.agents = args
			return runIntegrateShow(cmd.OutOrStdout(), fl)
		},
	}
	cmd.Flags().StringVar(&fl.scope, "scope", "", "user, project or local (default: user)")
	cmd.Flags().StringVar(&fl.project, "project", "", "project directory for project/local scopes")
	cmd.Flags().BoolVar(&fl.json, "json", false, "print the snippet as JSON")
	return cmd
}

func newIntegrateVerifyCmd() *cobra.Command {
	var fl integrateFlags
	cmd := &cobra.Command{
		Use:   "verify [<agent>... | --detected]",
		Short: "Verify that installed entries can start the MCP server",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fl.agents = args
			return runIntegrateVerify(cmd.OutOrStdout(), fl)
		},
	}
	cmd.Flags().StringVar(&fl.scope, "scope", "", "user, project or local (default: user)")
	cmd.Flags().StringVar(&fl.project, "project", "", "project directory for project/local scopes")
	cmd.Flags().BoolVar(&fl.detected, "detected", false, "every detected Tier-1 harness")
	return cmd
}

func newIntegrateSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage the embedded Agent Skills",
	}
	cmd.AddCommand(
		newIntegrateSkillsActionCmd("list", "List the embedded skills"),
		newIntegrateSkillsActionCmd("status", "Show per-harness skill state"),
		newIntegrateSkillsActionCmd("install", "Install skills for harnesses"),
		newIntegrateSkillsActionCmd("remove", "Remove installed skills"),
	)
	return cmd
}

func newIntegrateSkillsActionCmd(action, short string) *cobra.Command {
	var fl integrateFlags
	cmd := &cobra.Command{
		Use:   action + " [--agent ID]... [--detected]",
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIntegrateSkills(action, cmd.OutOrStdout(), fl)
		},
	}
	cmd.Flags().StringArrayVar(&fl.agents, "agent", nil, "harness id (repeatable)")
	cmd.Flags().BoolVar(&fl.detected, "detected", false, "every detected Tier-1 harness with skills support")
	cmd.Flags().StringVar(&fl.scope, "scope", "", "user or project (default: user)")
	cmd.Flags().StringVar(&fl.project, "project", "", "project directory for project scope")
	cmd.Flags().BoolVar(&fl.yes, "yes", false, "skip confirmation prompts")
	return cmd
}

func newIntegrateSkillCmd() *cobra.Command {
	var write, remove bool
	cmd := &cobra.Command{
		Use:   "skill [--write|--remove]",
		Short: "Legacy skill install/remove (maps to `integrate skills`)",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fl := integrateFlags{detected: true}
			switch {
			case remove:
				return runIntegrateSkills("remove", out, fl)
			case write:
				return runIntegrateSkills("install", out, fl)
			default:
				return runIntegrateSkills("list", out, fl)
			}
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "install skills (legacy)")
	cmd.Flags().BoolVar(&remove, "remove", false, "remove skills (legacy)")
	return cmd
}

func newIntegrateStatusCmd() *cobra.Command {
	var fl integrateFlags
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Legacy alias for `integrate list`",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIntegrateList(cmd.OutOrStdout(), fl)
		},
	}
	cmd.Flags().BoolVar(&fl.all, "all", false, "include undetected Tier-2 harnesses")
	cmd.Flags().StringVar(&fl.project, "project", "", "project directory for project/local scopes")
	return cmd
}

func runIntegrateLegacy(out io.Writer, args []string, fl integrateFlags, write, remove bool) error {
	failed := 0
	for _, a := range args {
		id := integrate.CanonicalID(a)
		if _, ok := integrate.Lookup(id); !ok {
			fmt.Fprintf(out, "unknown agent %q; run jack-ketch integrate list\n", a)
			failed++
			continue
		}
		sub := integrateFlags{agents: []string{id}, yes: fl.yes, project: fl.project}
		if fl.scope == "" {
			if id == "claude-code" {
				sub.scope = "project"
			} else {
				sub.scope = "user"
			}
		} else {
			sub.scope = fl.scope
		}
		var err error
		switch {
		case remove:
			err = runIntegrateRemove(out, sub)
		case write:
			err = runIntegrateInstall(out, sub)
		default:
			err = runIntegrateShow(out, sub)
		}
		if err != nil {
			fmt.Fprintf(out, "%v\n", err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d agents failed", failed, len(args))
	}
	return nil
}

func defaultProject(p string) string {
	if p != "" {
		return p
	}
	if ws := rtmcp.DefaultWorkspacePath(); ws != "" {
		return project.WorkspaceRoot(ws)
	}
	return ""
}

func canonicalAgents(names []string) ([]string, int) {
	seen := map[string]bool{}
	var out []string
	failed := 0
	for _, n := range names {
		id := integrate.CanonicalID(n)
		if _, ok := integrate.Lookup(id); !ok {
			fmt.Printf("unknown agent %q; run jack-ketch integrate list\n", n)
			failed++
			continue
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, failed
}

func detectedAgents() []string {
	var out []string
	for _, h := range integrate.Harnesses() {
		if h.Tier == 1 && integrate.Detect(h).Detected {
			out = append(out, h.ID)
		}
	}
	return out
}

func resolveAgents(fl integrateFlags) (integrate.Scope, string, []string, int, error) {
	scope, err := integrate.ParseScope(fl.scope)
	if err != nil {
		return "", "", nil, 0, err
	}
	project := defaultProject(fl.project)
	names := fl.agents
	if fl.detected {
		names = detectedAgents()
	}
	agents, failed := canonicalAgents(names)
	if scope != integrate.ScopeUser && project == "" && len(agents) > 0 {
		return scope, project, agents, failed, fmt.Errorf("--project is required for %s scope", scope)
	}
	return scope, project, agents, failed, nil
}

func changeSummary(changes []integrate.Change) (string, string, bool) {
	path, backup, changed := "", "", false
	for _, c := range changes {
		if path == "" {
			path = c.Path
		}
		if backup == "" {
			backup = c.Backup
		}
		changed = changed || c.Changed
	}
	return path, backup, changed
}

func printDiff(out io.Writer, diff string) {
	color := isTTY(os.Stdout)
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		switch {
		case !color:
			fmt.Fprintln(out, line)
		case strings.HasPrefix(line, "+"):
			fmt.Fprintf(out, "\x1b[32m%s\x1b[0m\n", line)
		case strings.HasPrefix(line, "-"):
			fmt.Fprintf(out, "\x1b[31m%s\x1b[0m\n", line)
		default:
			fmt.Fprintln(out, line)
		}
	}
}

func runIntegrateList(out io.Writer, fl integrateFlags) error {
	project := defaultProject(fl.project)
	reports := integrate.ReportAll(project)
	if fl.json {
		return json.NewEncoder(out).Encode(reports)
	}
	fmt.Fprintf(out, "%-14s %-22s %-16s %-11s %-9s %-9s %s\n", "AGENT", "NAME", "DETECTED", "USER", "PROJECT", "LOCAL", "SKILLS")
	for _, r := range reports {
		if !fl.all && r.Tier != 1 && !r.Detection.Detected {
			continue
		}
		det := "-"
		if r.Detection.Detected {
			det = "yes"
			if v := strings.TrimSpace(r.Detection.Version); v != "" {
				det += " " + v
			}
		}
		skills := "n/a"
		if r.SkillsAvail {
			if states, err := integrate.SkillStatus(r.ID, integrate.ScopeUser, ""); err == nil {
				visible := 0
				for _, st := range states {
					if st.Visible {
						visible++
					}
				}
				skills = fmt.Sprintf("%d/%d", visible, len(states))
			} else {
				skills = "-"
			}
		}
		fmt.Fprintf(out, "%-14s %-22s %-16s %-11s %-9s %-9s %s\n",
			r.ID, r.DisplayName, det,
			scopeWord(r, integrate.ScopeUser), scopeWord(r, integrate.ScopeProject), scopeWord(r, integrate.ScopeLocal),
			skills)
	}
	return nil
}

func scopeWord(r integrate.HarnessReport, scope integrate.Scope) string {
	for _, sc := range r.Scopes {
		if sc.Scope != scope {
			continue
		}
		switch sc.Status {
		case "installed":
			return "connected"
		case "outdated":
			return "update"
		case "broken":
			return "broken"
		case "absent":
			return "-"
		default:
			return "n/a"
		}
	}
	return "n/a"
}

func runIntegrateInstall(out io.Writer, fl integrateFlags) error {
	scope, project, agents, failed, err := resolveAgents(fl)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		if failed > 0 {
			return fmt.Errorf("%d of %d agents failed", failed, len(fl.agents))
		}
		return errors.New("no agents to install (pass agent names or --detected)")
	}
	opts := integrate.Options{ProjectDir: project, Command: fl.command}
	results := make([]map[string]any, 0, len(agents))
	total := len(agents) + failed
	for _, id := range agents {
		h, _ := integrate.Lookup(id)
		res := map[string]any{"agent": id}
		changes, err := integrate.PlanInstall(id, scope, opts)
		if err != nil {
			fmt.Fprintf(out, "[FAIL] %s: %v\n", id, err)
			res["error"] = err.Error()
			results = append(results, res)
			failed++
			continue
		}
		if !fl.json {
			for _, c := range changes {
				if c.Diff != "" {
					printDiff(out, c.Diff)
				}
			}
		}
		if fl.dryRun {
			results = append(results, res)
			continue
		}
		if isTTY(os.Stdin) && !fl.yes {
			ok, err := confirm("Apply these changes? [Y/n]", true)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintf(out, "skipped %s\n", id)
				results = append(results, res)
				continue
			}
		}
		changes, err = integrate.Install(id, scope, opts)
		if err != nil {
			fmt.Fprintf(out, "[FAIL] %s: %v\n", id, err)
			res["error"] = err.Error()
			results = append(results, res)
			failed++
			continue
		}
		path, backup, changed := changeSummary(changes)
		res["path"], res["backup"], res["changed"] = path, backup, changed
		if !fl.json {
			fmt.Fprintf(out, "connected %s → %s\n", h.DisplayName, path)
			if backup != "" {
				fmt.Fprintf(out, "  backup: %s\n", backup)
			}
			if h.RestartHint != "" {
				fmt.Fprintf(out, "  next: %s\n", h.RestartHint)
			}
		}
		if fl.skills && h.Skills != nil {
			states, serr := integrate.InstallSkills(id, scope, project)
			if serr != nil {
				fmt.Fprintf(out, "[FAIL] %s skills: %v\n", id, serr)
				res["error"] = serr.Error()
				results = append(results, res)
				failed++
				continue
			}
			visible := 0
			for _, st := range states {
				if st.Visible {
					visible++
				}
			}
			res["skills"] = map[string]any{"visible": visible, "total": len(states)}
			if !fl.json {
				fmt.Fprintf(out, "  skills: %d/%d\n", visible, len(states))
			}
		}
		results = append(results, res)
	}
	if fl.json {
		if err := json.NewEncoder(out).Encode(results); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d agents failed", failed, total)
	}
	return nil
}

func runIntegrateRemove(out io.Writer, fl integrateFlags) error {
	scope, project, agents, failed, err := resolveAgents(fl)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		if failed > 0 {
			return fmt.Errorf("%d of %d agents failed", failed, len(fl.agents))
		}
		return errors.New("no agents to remove (pass agent names or --detected)")
	}
	opts := integrate.Options{ProjectDir: project}
	total := len(agents) + failed
	for _, id := range agents {
		h, _ := integrate.Lookup(id)
		changes, err := integrate.PlanRemove(id, scope, opts)
		if err != nil {
			fmt.Fprintf(out, "[FAIL] %s: %v\n", id, err)
			failed++
			continue
		}
		for _, c := range changes {
			if c.Diff != "" {
				printDiff(out, c.Diff)
			}
		}
		if fl.dryRun {
			continue
		}
		if isTTY(os.Stdin) && !fl.yes {
			ok, err := confirm("Apply these changes? [Y/n]", true)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintf(out, "skipped %s\n", id)
				continue
			}
		}
		changes, err = integrate.Remove(id, scope, opts)
		if err != nil {
			fmt.Fprintf(out, "[FAIL] %s: %v\n", id, err)
			failed++
			continue
		}
		path, _, changed := changeSummary(changes)
		if changed {
			fmt.Fprintf(out, "removed %s from %s\n", h.DisplayName, path)
		} else {
			fmt.Fprintf(out, "not configured: %s\n", path)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d agents failed", failed, total)
	}
	return nil
}

func runIntegrateShow(out io.Writer, fl integrateFlags) error {
	scope, project, agents, failed, err := resolveAgents(fl)
	if err != nil {
		return err
	}
	if len(agents) != 1 {
		return fmt.Errorf("usage: jack-ketch integrate show <agent> (got %d agent(s), want exactly one)", len(agents)+failed)
	}
	sn, err := integrate.SnippetFor(agents[0], scope, integrate.Options{ProjectDir: project})
	if err != nil {
		return err
	}
	if fl.json {
		return json.NewEncoder(out).Encode(sn)
	}
	if sn.Path != "" {
		fmt.Fprintf(out, "Path: %s\n", sn.Path)
	}
	fmt.Fprintln(out, strings.TrimRight(sn.Text, "\n"))
	if sn.CLI != "" {
		fmt.Fprintf(out, "CLI: %s\n", sn.CLI)
	}
	if sn.Deeplink != "" {
		fmt.Fprintf(out, "Deeplink: %s\n", sn.Deeplink)
	}
	if sn.Steps != "" {
		fmt.Fprintf(out, "Steps: %s\n", sn.Steps)
	}
	return nil
}

func runIntegrateVerify(out io.Writer, fl integrateFlags) error {
	scope, project, agents, failed, err := resolveAgents(fl)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		agents, failed = canonicalAgents(detectedAgents())
	}
	if len(agents) == 0 {
		return errors.New("no agents to verify (pass agent names or --detected)")
	}
	opts := integrate.Options{ProjectDir: project}
	failedAgents := failed
	for _, id := range agents {
		checks, err := integrate.Verify(context.Background(), id, scope, opts)
		if err != nil {
			fmt.Fprintf(out, "[FAIL] %s: %v\n", id, err)
			failedAgents++
			continue
		}
		bad := false
		for _, c := range checks {
			mark := "ok"
			if !c.OK {
				mark = "FAIL"
				bad = true
			}
			fmt.Fprintf(out, "[%s] %s %s: %s\n", mark, id, c.Name, c.Detail)
		}
		if bad || len(checks) == 0 {
			failedAgents++
		}
	}
	if failedAgents > 0 {
		return fmt.Errorf("%d of %d agents failed", failedAgents, len(agents)+failed)
	}
	return nil
}

func skillsAgents(fl integrateFlags, fallback bool) ([]string, int) {
	seen := map[string]bool{}
	var out []string
	if fl.detected || len(fl.agents) == 0 {
		for _, h := range integrate.Harnesses() {
			if h.Tier != 1 || h.Skills == nil || !integrate.Detect(h).Detected {
				continue
			}
			if !seen[h.ID] {
				seen[h.ID] = true
				out = append(out, h.ID)
			}
		}
	}
	explicit, failed := canonicalAgents(fl.agents)
	for _, id := range explicit {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) == 0 && len(fl.agents) == 0 && fallback {
		out = []string{"claude-code", "opencode"}
	}
	return out, failed
}

func runIntegrateSkills(action string, out io.Writer, fl integrateFlags) error {
	if action == "list" {
		for _, sk := range integrate.EmbeddedSkills() {
			fmt.Fprintf(out, "%-30s %-10s %s\n", sk.Name, sk.Version, sk.Description)
		}
		return nil
	}
	scope, err := integrate.ParseScope(fl.scope)
	if err != nil {
		return err
	}
	if scope == integrate.ScopeLocal {
		return errors.New("skills support user or project scope")
	}
	project := defaultProject(fl.project)
	if scope == integrate.ScopeProject && project == "" {
		return errors.New("--project is required for project scope")
	}
	fallback := action == "install" || action == "remove"
	agents, failed := skillsAgents(fl, fallback)
	if len(agents) == 0 {
		fmt.Fprintln(out, "no harnesses detected and none given; pass --agent <id>")
		return nil
	}
	switch action {
	case "status":
		for _, id := range agents {
			states, err := integrate.SkillStatus(id, scope, project)
			if err != nil {
				fmt.Fprintf(out, "[FAIL] %s: %v\n", id, err)
				failed++
				continue
			}
			for _, st := range states {
				state := "-"
				if st.Visible {
					if st.Outdated {
						state = "update"
					} else {
						state = "ok"
					}
				}
				fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", id, st.Name, state, st.Have)
			}
		}
	case "install":
		if isTTY(os.Stdin) && !fl.yes {
			ok, err := confirm(fmt.Sprintf("Install skills for %s? [Y/n]", strings.Join(agents, ", ")), true)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}
		for _, id := range agents {
			states, err := integrate.InstallSkills(id, scope, project)
			if err != nil {
				fmt.Fprintf(out, "[FAIL] %s: %v\n", id, err)
				failed++
				continue
			}
			visible := 0
			for _, st := range states {
				if st.Visible {
					visible++
				}
			}
			fmt.Fprintf(out, "installed skills for %s: %d/%d\n", id, visible, len(states))
		}
	case "remove":
		if isTTY(os.Stdin) && !fl.yes {
			ok, err := confirm(fmt.Sprintf("Remove skills for %s? [y/N]", strings.Join(agents, ", ")), false)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}
		for _, id := range agents {
			dirs, err := integrate.RemoveSkills(id, scope, project)
			if err != nil {
				fmt.Fprintf(out, "[FAIL] %s: %v\n", id, err)
				failed++
				continue
			}
			if len(dirs) == 0 {
				fmt.Fprintf(out, "no skills installed for %s\n", id)
				continue
			}
			for _, d := range dirs {
				fmt.Fprintf(out, "removed %s\n", d)
			}
		}
	default:
		return fmt.Errorf("unknown skills action %q", action)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d agents failed", failed, len(agents))
	}
	return nil
}

func newIntegrateHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Manage the Claude Code guardrail hook",
	}
	cmd.AddCommand(
		newIntegrateHooksActionCmd("install", "Install the Claude Code PreToolUse guardrail hook"),
		newIntegrateHooksActionCmd("remove", "Remove the Claude Code PreToolUse guardrail hook"),
	)
	return cmd
}

func newIntegrateHooksActionCmd(action, short string) *cobra.Command {
	var fl integrateFlags
	cmd := &cobra.Command{
		Use:   action + " claude-code [--scope user|project] [--project PATH]",
		Short: short,
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fl.agents = args
			return runIntegrateHooks(action, cmd.OutOrStdout(), fl)
		},
	}
	cmd.Flags().StringVar(&fl.scope, "scope", "", "user or project (default: user)")
	cmd.Flags().StringVar(&fl.project, "project", "", "project directory for project scope")
	cmd.Flags().BoolVar(&fl.dryRun, "dry-run", false, "print the diff without writing")
	cmd.Flags().BoolVar(&fl.yes, "yes", false, "skip confirmation prompts")
	return cmd
}

func runIntegrateHooks(action string, out io.Writer, fl integrateFlags) error {
	if len(fl.agents) != 1 {
		return fmt.Errorf("usage: jack-ketch integrate hooks %s claude-code [--scope user|project]", action)
	}
	if id := integrate.CanonicalID(fl.agents[0]); id != "claude-code" {
		return fmt.Errorf("the guardrail hook is available for claude-code only, not %q", fl.agents[0])
	}
	scope, err := integrate.ParseScope(fl.scope)
	if err != nil {
		return err
	}
	project := defaultProject(fl.project)
	remove := action == "remove"
	c, err := integrate.PlanClaudeHook(scope, project, remove)
	if err != nil {
		return err
	}
	if c.Diff != "" {
		printDiff(out, c.Diff)
	}
	if fl.dryRun {
		return nil
	}
	if !c.Changed {
		if remove {
			fmt.Fprintf(out, "hook not installed in %s\n", c.Path)
		} else {
			fmt.Fprintf(out, "hook already installed in %s\n", c.Path)
		}
		return nil
	}
	if isTTY(os.Stdin) && !fl.yes {
		ok, err := confirm("Apply these changes? [Y/n]", true)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
	c, err = integrate.ApplyClaudeHook(scope, project, remove)
	if err != nil {
		return err
	}
	if remove {
		fmt.Fprintf(out, "removed the Claude Code guardrail hook from %s\n", c.Path)
	} else {
		fmt.Fprintf(out, "installed the Claude Code guardrail hook in %s\n", c.Path)
	}
	if c.Backup != "" {
		fmt.Fprintf(out, "  backup: %s\n", c.Backup)
	}
	if c.RestartHint != "" {
		fmt.Fprintf(out, "  next: %s\n", c.RestartHint)
	}
	return nil
}

func prompt(msg string, args ...any) (string, error) {
	fmt.Printf(msg, args...)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		fmt.Println()
		return "", errors.New("no input")
	}
	fmt.Println()
	return strings.TrimSpace(sc.Text()), nil
}

func confirm(q string, def bool) (bool, error) {
	answer, err := prompt("%s", q)
	if err != nil {
		return false, err
	}
	if answer == "" {
		return def, nil
	}
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	}
	return false, fmt.Errorf("invalid answer %q (y/n)", answer)
}
