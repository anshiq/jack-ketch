// Cobra command tree for jack-ketch. The MCP server ("serve") is an
// explicit subcommand; running with no arguments prints the command help.
// run/shell keep their hand-rolled flag parsers (DisableFlagParsing) so flags
// may appear after positional arguments.
package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"jack-ketch/internal/config"
	"jack-ketch/internal/integrate"
	"jack-ketch/internal/jackketchd"
	rtmcp "jack-ketch/internal/mcp"
)

// Run is the CLI entry point invoked from main with os.Args[1:].
//
// No arguments -> the command help. The process manager lives under "repl";
// "serve" and the other subcommands run as one-shots. The REPL commands
// (start/logs/wait/...) only exist inside a session because they manage the
// session's processes.
func Run(args []string, logger *slog.Logger) error {
	cwd, _ := os.Getwd()
	loaded := config.EmptyLoaded(cwd)
	if len(args) == 0 {
		return runBare(loaded, logger)
	}
	for _, a := range args {
		if a == "--no-gui" {
			return newRootCmd(nil, loaded, logger).Help()
		}
		if a == "--tui" || a == "-t" {
			return errors.New("the TUI has been removed; use `jack-ketch web` for the web interface")
		}
		if a == "--gui" {
			return errors.New("the desktop GUI has been removed; use `jack-ketch web` for the web interface")
		}
	}
	root := newRootCmd(nil, loaded, logger)
	root.SetArgs(args)
	return root.Execute()
}

func runBare(loaded *config.Loaded, logger *slog.Logger) error {
	return newRootCmd(nil, loaded, logger).Help()
}

// newRootCmd builds the full command tree. s is nil for one-shot use; when
// non-nil, the session-scoped process commands are added (interactive REPL).
func newRootCmd(s *session, loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:   "jack-ketch",
		Short: "local async process supervisor for AI coding agents",
		Long: `jack-ketch supervises development processes and exposes them over MCP.

Run with no arguments to print the command help.

One-shot commands:
   web                Open the web UI (token URL + browser).
   serve              Run the MCP server (stdio, or --http ADDR).
   integrate          Inspect and install MCP client config and skills.
   doctor             Run diagnostics for daemon, socket, perms and integrations.
   daemon             Manage the daemon, autostart and its TCP listener.
   repl               Open the interactive process manager (start/logs/wait/stop).
   run <cmd> [args...]  Start a process in the foreground and tail its output.
   shell <app|proc>     Start an interactive shell in a process's environment.
   ps, logs, config, sessions  Inspect the daemon from the CLI.
   migrate            Import legacy v2 state / print migration report.
   version            Print the version.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				fmt.Printf("jack-ketch %s\n", rtmcp.Version)
				return nil
			}
			return cmd.Help()
		},
	}
	root.PersistentFlags().BoolVarP(&showVersion, "version", "v", false, "print version and exit")
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newServeCmd(loaded, logger),
		newReplCmd(loaded, logger),
		newRunCmd(loaded, logger),
		newShellCmd(loaded, logger),
		newIntegrateCmd(),
		newJackKetchdCmd(),
		newDaemonCmd(loaded, logger),
		newProjectCmd(loaded, logger),
		newDaemonPsCmd(loaded, logger),
		newDaemonLogsCmd(loaded, logger),
		newDaemonStartCmd(loaded, logger),
		newDaemonStopCmd(loaded, logger),
		newDaemonRestartCmd(loaded, logger),
		newDaemonConfigCmd(loaded, logger),
		newDaemonSessionsCmd(loaded, logger),
		newMigrateCmd(loaded, logger),
		newDoctorCmd(loaded, logger),
		newWebCmd(loaded, logger),
		newVersionCmd(),
		newHookCmd(),
	)
	if s != nil {
		root.AddCommand(newServeHintCmd())
		root.AddCommand(s.sessionCommands()...)
	}
	return root
}

// newReplCmd opens the interactive process manager. Its commands map 1:1 to
// the MCP tools (start/ps/logs/wait/signal/stop/env/shell/exit, ...).
func newReplCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "repl",
		Short: "Open the interactive process manager",
		Long:  "Open the interactive process manager: start/stop/signal processes, read and wait on logs, inspect env, and shell into a process's environment.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return replMain(loaded, logger)
		},
	}
}

func newServeCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var opts serveOptions
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server over stdio (or --http)",
		Long:  "Run the MCP server as a thin client of the per-user daemon (started on demand). With --embedded, run the legacy session-scoped runtime instead. With --http ADDR, serve over the streamable-HTTP transport instead.",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts = fillServeEnv(opts)
			if opts.harness != "" {
				if _, ok := integrate.Lookup(opts.harness); !ok {
					fmt.Fprintf(os.Stderr, "warning: unknown harness %q\n", opts.harness)
				}
			}
			if opts.check {
				return serveCheck(logger, opts)
			}
			if opts.httpAddr != "" {
				return ServeHTTP(logger, opts)
			}
			return serveStdio(loaded, logger, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.embedded, "embedded", false, "legacy session-scoped runtime instead of the daemon bridge")
	cmd.Flags().StringVar(&opts.project, "project", "", "workspace path or id to serve (default: current project)")
	cmd.Flags().StringVar(&opts.harness, "harness", "", "harness id this client identifies as")
	cmd.Flags().IntVar(&opts.maxWaitMS, "max-wait-ms", 0, "cap for the wait tools in milliseconds (0 = per-harness detected limit)")
	cmd.Flags().StringVar(&opts.toolset, "toolset", "", "tool set to expose: full or core")
	cmd.Flags().BoolVar(&opts.check, "check", false, "verify daemon connectivity and the tool list, print one JSON line, exit")
	cmd.Flags().StringVar(&opts.httpAddr, "http", "", "serve over streamable-HTTP on this address (e.g. :7341) instead of stdio")
	cmd.Flags().BoolVar(&opts.allowRemote, "allow-remote", false, "with --http, allow binding non-loopback interfaces")
	cmd.Flags().StringVar(&opts.token, "token", "", "bearer token for --http")
	cmd.Flags().StringVar(&opts.tokenFile, "token-file", "", "file containing the bearer token for --http")
	return cmd
}

func fillServeEnv(opts serveOptions) serveOptions {
	if opts.harness == "" {
		opts.harness = os.Getenv("JACK_KETCH_HARNESS")
	}
	if opts.maxWaitMS == 0 {
		if v := os.Getenv("JACK_KETCH_MAX_WAIT_MS"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				opts.maxWaitMS = n
			}
		}
	}
	if opts.toolset == "" {
		opts.toolset = os.Getenv("JACK_KETCH_TOOLSET")
	}
	if opts.token == "" {
		opts.token = os.Getenv("JACK_KETCH_HTTP_TOKEN")
	}
	return opts
}

func newRunCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:                "run <command> [args...]",
		Short:              "Start a process in the foreground and tail its output",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunCommand(args, loaded, logger)
		},
	}
}

func newShellCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:                "shell <app|process_id> [--shell path]",
		Short:              "Start an interactive shell in a process's environment",
		Long:               "Start an interactive shell inside the resolved workdir+env of an app from the project config (or a process started by this session). Process ids of daemon-managed processes are not visible here; use the web UI terminal or the MCP open_shell tool for those.",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return ShellCommand(args, loaded, logger)
		},
	}
}

func newJackKetchdCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "jackketchd",
		Short:              "Run the per-user daemon in the foreground",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return jackketchd.Main(args, "v"+rtmcp.Version)
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("jack-ketch %s\n", rtmcp.Version)
			return nil
		},
	}
}

// newServeHintCmd is the REPL-only placeholder for `serve`: the stdio server
// must own stdin, so it cannot run inside the session.
func newServeHintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP stdio server (not available inside the REPL)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("serve owns stdin and cannot run inside this session; exit the REPL and run: jack-ketch serve")
		},
	}
}
