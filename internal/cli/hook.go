package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

const hookDenyReason = "jack-ketch supervises long-running dev processes in this project. Use the jack-ketch MCP tool start_process (command=..., args=[...] or app=...) and then wait_for_log(process_id, ready=true) instead of running it in Bash."

var devServerPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(npm|pnpm|yarn|bun)( run)? (dev|start|serve|watch)\b`),
	regexp.MustCompile(`^npx (next|vite|nuxt|astro|remix) dev\b`),
	regexp.MustCompile(`^(next|vite|nuxt) dev\b`),
	regexp.MustCompile(`^nodemon\b`),
	regexp.MustCompile(`^python3? (-m )?manage\.py runserver\b`),
	regexp.MustCompile(`^uvicorn\b.*--reload`),
	regexp.MustCompile(`^flask run\b`),
	regexp.MustCompile(`^rails (s|server)\b`),
	regexp.MustCompile(`^(\./)?mvnw? spring-boot:run\b`),
	regexp.MustCompile(`^\./gradlew bootRun\b`),
	regexp.MustCompile(`^php artisan serve\b`),
	regexp.MustCompile(`^air\b`),
	regexp.MustCompile(`^cargo watch\b`),
}

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Agent hook entry points",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:    "claude-pretooluse",
		Short:  "Claude Code PreToolUse guardrail",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookPretooluse(cmd.InOrStdin(), cmd.OutOrStdout())
		},
	})
	return cmd
}

func runHookPretooluse(in io.Reader, out io.Writer) error {
	data, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return nil
	}
	var payload struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	if payload.ToolName != "Bash" || !isDevServerCommand(payload.ToolInput.Command) {
		return nil
	}
	_, err = fmt.Fprintln(out, `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"`+hookDenyReason+`"}}`)
	return err
}

func isDevServerCommand(command string) bool {
	segments := strings.NewReplacer("&&", "\n", ";", "\n", "|", "\n").Replace(command)
	for _, segment := range strings.Split(segments, "\n") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		for _, re := range devServerPatterns {
			if re.MatchString(segment) {
				return true
			}
		}
	}
	return false
}
