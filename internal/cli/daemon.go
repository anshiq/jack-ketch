package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"jack-ketch/internal/config"
	"jack-ketch/internal/daemon"
	"jack-ketch/internal/platform/paths"
	"jack-ketch/internal/platform/servicemgr"
	"jack-ketch/pkg/client"
)

// newDaemonCmd adds the `daemon start|stop|status|run` tree. The daemon owns
// the runtime's processes so they survive the MCP session; `serve` and `repl`
// become thin clients when runtime.daemon is set.
func newDaemonCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var startTCP string
	var stopYes, restartYes bool
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the long-lived daemon that owns managed processes",
		Long:  "Manage the daemon that keeps managed processes alive across MCP sessions (runtime.daemon: true).",
	}
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon in the background",
		RunE: func(c *cobra.Command, args []string) error {
			p := paths.User()
			d := daemon.New(p.SocketPath(), p.Data)
			if startTCP != "" {
				if err := d.WriteTCPWant(startTCP); err != nil {
					return err
				}
				d = d.WithTCP(startTCP)
			}
			if d.IsRunning() {
				if startTCP != "" {
					if err := d.Restart(); err != nil {
						return err
					}
					fmt.Printf("daemon restarted with TCP listener on %s\n", startTCP)
					return nil
				}
				fmt.Println("daemon is already running")
				return nil
			}
			fmt.Println("starting daemon...")
			if err := d.Start(); err != nil {
				return err
			}
			if err := d.WaitReady(10 * time.Second); err != nil {
				return err
			}
			fmt.Printf("daemon ready (socket %s)\n", p.SocketPath())
			return nil
		},
	}
	startCmd.Flags().StringVar(&startTCP, "tcp", "", "also serve authenticated loopback TCP for the web UI (e.g. 127.0.0.1:7350)")
	cmd.AddCommand(
		startCmd,
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the daemon and every managed process",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				d := daemon.New(p.SocketPath(), p.Data)
				if !d.IsRunning() {
					fmt.Println("daemon is not running")
					return nil
				}
				if ok, err := confirmStopsProcesses("Stopping the daemon", stopYes); err != nil || !ok {
					return err
				}
				if err := d.Stop(); err != nil {
					return err
				}
				deadline := time.Now().Add(20 * time.Second)
				for time.Now().Before(deadline) {
					if !d.IsRunning() {
						fmt.Println("daemon stopped")
						return nil
					}
					time.Sleep(20 * time.Millisecond)
				}
				return fmt.Errorf("daemon did not stop within 20s")
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Report whether the daemon is running",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				d := daemon.New(p.SocketPath(), p.Data)
				if d.IsRunning() {
					fmt.Printf("running (pid %d)\n", d.ReadPid())
					printUpdateNotice()
				} else {
					fmt.Println("not running")
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "restart",
			Short: "Restart the per-user daemon (stops every managed process)",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				d := daemon.New(p.SocketPath(), p.Data)
				if d.IsRunning() {
					if ok, err := confirmStopsProcesses("Restarting the daemon", restartYes); err != nil || !ok {
						return err
					}
				}
				if err := d.Restart(); err != nil {
					return err
				}
				fmt.Println("daemon restarted; managed processes were stopped and autostart apps start again when their project is next used")
				return nil
			},
		},
		&cobra.Command{
			Use:   "logs",
			Short: "Tail the per-user daemon log",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				data, err := os.ReadFile(p.LogPath())
				if err != nil {
					return err
				}
				fmt.Print(tailLines(string(data), 100))
				return nil
			},
		},
		&cobra.Command{
			Use:   "install",
			Short: "Install autostart (systemd user socket / LaunchAgent / logon task)",
			RunE: func(c *cobra.Command, args []string) error {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				argv := daemon.ResolveJackKetchdCommand(exe)
				p := paths.User()
				mgr, err := servicemgr.New(argv, p.SocketPath())
				if err != nil {
					return err
				}
				return mgr.Install()
			},
		},
		&cobra.Command{
			Use:   "uninstall",
			Short: "Remove autostart",
			RunE: func(c *cobra.Command, args []string) error {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				argv := daemon.ResolveJackKetchdCommand(exe)
				p := paths.User()
				mgr, err := servicemgr.New(argv, p.SocketPath())
				if err != nil {
					return err
				}
				return mgr.Uninstall()
			},
		},
	)
	for _, sc := range cmd.Commands() {
		switch sc.Name() {
		case "stop":
			sc.Flags().BoolVarP(&stopYes, "yes", "y", false, "do not ask before stopping running managed processes")
		case "restart":
			sc.Flags().BoolVarP(&restartYes, "yes", "y", false, "do not ask before stopping running managed processes")
		}
	}
	return cmd
}

func confirmStopsProcesses(action string, yes bool) (bool, error) {
	p := paths.User()
	cl, err := client.EnsureDaemonAs(p.SocketPath(), "cli/daemon")
	if err != nil {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := cl.SystemService().UpdateStatus(ctx)
	if err != nil || st.RunningProcesses == 0 {
		return true, nil
	}
	fmt.Fprintf(os.Stderr, "warning: %s stops %d running managed process(es):\n", action, st.RunningProcesses)
	for _, rp := range st.Processes {
		name := rp.App
		if name == "" {
			name = rp.Command
		}
		fmt.Fprintf(os.Stderr, "  %s\t%s\t%s\n", rp.ProcessID, name, rp.WorkspacePath)
	}
	if yes {
		return true, nil
	}
	if !isTTY(os.Stdin) {
		return false, fmt.Errorf("%s would stop running processes; re-run with --yes to confirm", strings.ToLower(action[:1])+action[1:])
	}
	ok, err := confirm("Continue? [y/N] ", false)
	if err != nil {
		return false, err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "aborted")
	}
	return ok, nil
}

func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func printUpdateNotice() {
	p := paths.User()
	d := daemon.New(p.SocketPath(), p.Data)
	if !d.IsRunning() {
		return
	}
	cl, err := client.EnsureDaemonAs(p.SocketPath(), "cli/daemon")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := cl.SystemService().UpdateStatus(ctx)
	if err != nil || !st.Pending {
		return
	}
	fmt.Fprintln(os.Stderr, "update: "+st.Notice)
}
