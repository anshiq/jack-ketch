//go:build darwin
// +build darwin

package servicemgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type launchdManager struct {
	argv       []string
	socketPath string
}

func newPlatform(argv []string, socketPath string) (Manager, error) {
	return &launchdManager{argv: argv, socketPath: socketPath}, nil
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func (m *launchdManager) plist() string {
	var args strings.Builder
	args.WriteString("  <key>ProgramArguments</key>\n  <array>")
	for _, a := range m.argv {
		args.WriteString("<string>" + xmlEscaper.Replace(a) + "</string>")
	}
	args.WriteString("</array>")
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>dev.jack-ketch.jackketchd</string>
` + args.String() + `
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key>
  <dict><key>SuccessfulExit</key><false/></dict>
</dict>
</plist>
`
}

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", "dev.jack-ketch.jackketchd.plist"), nil
}

func (m *launchdManager) Install() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, []byte(m.plist()), 0o644); err != nil {
		return err
	}
	_, err = run("launchctl", "load", path)
	return err
}

func (m *launchdManager) Uninstall() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	_, _ = run("launchctl", "unload", path)
	return os.Remove(path)
}

func (m *launchdManager) Status() (ServiceStatus, error) {
	out, err := run("launchctl", "list", "dev.jack-ketch.jackketchd")
	if err != nil {
		return ServiceStatus{}, nil
	}
	st := ServiceStatus{Running: true}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "\"PID\"") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				if pid, err := strconv.Atoi(strings.Trim(fields[len(fields)-1], ";")); err == nil {
					st.Pid = pid
				}
			}
		}
	}
	fmt.Println(out)
	return st, nil
}

func (m *launchdManager) Start() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	_, err = run("launchctl", "load", path)
	return err
}

func (m *launchdManager) Stop() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	_, err = run("launchctl", "unload", path)
	return err
}
