//go:build linux
// +build linux

package servicemgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type systemdManager struct {
	argv       []string
	socketPath string
}

func newPlatform(argv []string, socketPath string) (Manager, error) {
	return &systemdManager{argv: argv, socketPath: socketPath}, nil
}

func systemdJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		a = strings.ReplaceAll(a, "%", "%%")
		a = strings.ReplaceAll(a, "$", "$$")
		if strings.ContainsAny(a, " \t\"\\'") {
			a = `"` + strings.ReplaceAll(strings.ReplaceAll(a, `\`, `\\`), `"`, `\"`) + `"`
		}
		parts[i] = a
	}
	return strings.Join(parts, " ")
}

func unitDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "systemd", "user"), nil
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

func (m *systemdManager) socketUnit() string {
	return `[Unit]
Description=jack-ketch daemon socket (per-user)
Documentation=https://github.com/anshiq/jack-ketch

[Socket]
ListenStream=` + m.socketPath + `
SocketMode=0600
DirectoryMode=0700

[Install]
WantedBy=sockets.target
`
}

func (m *systemdManager) serviceUnit() string {
	return `[Unit]
Description=jack-ketch daemon (1 per OS user)
Requires=jackketchd.socket
After=jackketchd.socket

[Service]
Type=notify
KillMode=process
Delegate=yes
Restart=on-failure
RestartSec=1s
ExecStart=` + systemdJoin(m.argv) + `
NotifyAccess=main

[Install]
WantedBy=default.target
`
}

// Install writes the user units and enables the socket. It never enables
// linger silently; linger guidance is printed instead.
func (m *systemdManager) Install() error {
	dir, err := unitDir()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, "jackketchd.socket"), []byte(m.socketUnit()), 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, "jackketchd.service"), []byte(m.serviceUnit()), 0o644); err != nil {
		return err
	}
	if _, err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if _, err := run("systemctl", "--user", "enable", "--now", "jackketchd.socket"); err != nil {
		return err
	}
	fmt.Println("installed jackketchd.socket. For survive-logout: loginctl enable-linger $USER (opt-in, never automatic)")
	return nil
}

func (m *systemdManager) Uninstall() error {
	_, _ = run("systemctl", "--user", "disable", "--now", "jackketchd.socket")
	_, _ = run("systemctl", "--user", "disable", "--now", "jackketchd.service")
	dir, err := unitDir()
	if err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, "jackketchd.socket"))
	_ = os.Remove(filepath.Join(dir, "jackketchd.service"))
	_, _ = run("systemctl", "--user", "daemon-reload")
	return nil
}

func (m *systemdManager) Status() (ServiceStatus, error) {
	out, err := run("systemctl", "--user", "is-active", "jackketchd.service", "jackketchd.socket")
	_ = out
	return ServiceStatus{Running: err == nil}, nil
}

func (m *systemdManager) Start() error {
	_, err := run("systemctl", "--user", "start", "jackketchd.socket")
	return err
}

func (m *systemdManager) Stop() error {
	_, err := run("systemctl", "--user", "stop", "jackketchd.service", "jackketchd.socket")
	return err
}
