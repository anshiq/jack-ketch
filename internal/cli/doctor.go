package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"jack-ketch/internal/config"
	"jack-ketch/internal/daemon"
	"jack-ketch/internal/integrate"
	rtmcp "jack-ketch/internal/mcp"
	"jack-ketch/internal/platform/paths"
	"jack-ketch/pkg/client"
)

type doctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

func newDoctorCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var jsonOut, fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostics for daemon, socket, perms and integrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := runDoctor(fix)
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(checks)
			}
			failed := 0
			for _, c := range checks {
				mark := "ok"
				if !c.OK {
					mark = "FAIL"
					failed++
				}
				fmt.Printf("[%s] %s %s\n", mark, c.Name, c.Detail)
				if !c.OK && c.Fix != "" {
					fmt.Printf("      fix: %s\n", c.Fix)
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d check(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable output")
	cmd.Flags().BoolVar(&fix, "fix", false, "repair permissions where safe")
	return cmd
}

func runDoctor(fix bool) []doctorCheck {
	p := paths.User()
	var out []doctorCheck
	add := func(name string, ok bool, detail, fixMsg string) {
		out = append(out, doctorCheck{Name: name, OK: ok, Detail: detail, Fix: fixMsg})
	}

	st, err := os.Stat(p.SocketPath())
	if err != nil {
		add("daemon-socket", false, "not present (daemon not running?)", "jack-ketch daemon start")
	} else {
		mode := st.Mode().Perm()
		ok := mode == 0o600
		detail := fmt.Sprintf("mode %o", mode)
		if !ok && fix {
			if err := os.Chmod(p.SocketPath(), 0o600); err == nil {
				ok = true
				detail += " (fixed)"
			}
		}
		add("daemon-socket", ok, detail, "chmod 600 "+p.SocketPath())
	}

	for _, d := range []struct{ name, path string }{
		{"runtime-dir", p.Runtime}, {"data-dir", p.Data}, {"config-dir", p.Config},
	} {
		st, err := os.Stat(d.path)
		if err != nil {
			add(d.name, false, "missing", "start the daemon once to create dirs")
			continue
		}
		ok := st.Mode().Perm() == 0o700
		detail := fmt.Sprintf("mode %o", st.Mode().Perm())
		if !ok && fix {
			if err := os.Chmod(d.path, 0o700); err == nil {
				ok = true
				detail += " (fixed)"
			}
		}
		add(d.name, ok, detail, "chmod 700 "+d.path)
	}

	if st, err := os.Stat(p.TokenPath()); err == nil {
		mode := st.Mode().Perm()
		ok := mode == 0o600
		detail := fmt.Sprintf("mode %o", mode)
		if !ok && fix {
			if err := os.Chmod(p.TokenPath(), 0o600); err == nil {
				ok = true
				detail += " (fixed)"
			}
		}
		add("token-file", ok, detail, "chmod 600 "+p.TokenPath())
	}

	if cl, err := client.EnsureDaemonAs(p.SocketPath(), "cli/doctor"); err != nil {
		add("daemon-reachable", false, err.Error(), "jack-ketch daemon start")
	} else {
		add("daemon-reachable", true, p.SocketPath(), "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		v, verr := cl.GetVersion(ctx)
		cancel()
		want := "v" + rtmcp.Version
		switch {
		case verr != nil:
			add("daemon-version", false, verr.Error(), "jack-ketch daemon restart")
		case v.DaemonVersion == want:
			add("daemon-version", true, v.DaemonVersion, "")
		default:
			add("daemon-version", false, fmt.Sprintf("%s (want %s)", v.DaemonVersion, want), "jack-ketch daemon restart")
		}
	}

	exe, err := os.Executable()
	if err != nil {
		add("daemon-binary", false, err.Error(), "reinstall jack-ketch")
	} else {
		argv := daemon.ResolveJackKetchdCommand(exe)
		ok := false
		if _, err := os.Stat(argv[0]); err == nil {
			ok = true
		}
		add("daemon-binary", ok, strings.Join(argv, " "), "install jackketchd next to the jack-ketch binary")
	}

	if addr := tcpAddrFromFile(); addr == "" {
		add("web-listener", true, "off (jack-ketch web turns it on)", "")
	} else if tcpReachable(addr, 2*time.Second) {
		add("web-listener", true, addr, "")
	} else {
		add("web-listener", false, addr+" (not reachable)", "jack-ketch web")
	}

	projectDir := defaultProject("")
	for _, h := range integrate.Harnesses() {
		if h.Tier != 1 {
			continue
		}
		rep := integrate.Report(h, projectDir)
		if !rep.Detection.Detected {
			continue
		}
		status := ""
		var problems []string
		for _, sc := range rep.Scopes {
			if sc.Scope != integrate.ScopeUser {
				continue
			}
			status = sc.Status
			for _, e := range sc.Entries {
				problems = append(problems, e.Problems...)
			}
		}
		switch status {
		case "installed":
			add("integration:"+h.ID, true, "connected", "")
		case "outdated":
			add("integration:"+h.ID, false, "outdated", "jack-ketch integrate install "+h.ID)
		case "broken":
			add("integration:"+h.ID, false, strings.Join(problems, "; "), "jack-ketch integrate install "+h.ID)
		case "absent":
			add("integration:"+h.ID, true, fmt.Sprintf("not connected (jack-ketch integrate install %s)", h.ID), "")
		default:
			add("integration:"+h.ID, true, "n/a", "")
		}
	}

	updates := integrate.CheckSkillUpdates(integrate.ScopeUser, "")
	if len(updates) == 0 {
		add("skills", true, "embedded skills up to date", "")
	}
	for _, u := range updates {
		add("skills", false, fmt.Sprintf("%s: %s have %s want %s", u.Harness, u.Name, u.Have, u.Want),
			"jack-ketch integrate skills install --agent "+u.Harness)
	}

	if wd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(wd, ".jack-ketch")); err == nil {
			add("legacy-dir", true, "legacy v2 directory; safe to delete after jack-ketch migrate", "")
		}
	}

	if runtime.GOOS == "linux" {
		if home, err := os.UserHomeDir(); err == nil {
			sock := filepath.Join(home, ".config", "systemd", "user", "jackketchd.socket")
			if _, err := os.Stat(sock); err == nil {
				raw, err := exec.Command("systemctl", "--user", "is-active", "jackketchd.socket").Output()
				state := strings.TrimSpace(string(raw))
				if state == "" && err != nil {
					state = err.Error()
				}
				add("systemd", state == "active", state, "systemctl --user enable --now jackketchd.socket")
			}
		}
	}
	return out
}
