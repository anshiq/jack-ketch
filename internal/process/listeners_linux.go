//go:build linux

package process

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Listener struct {
	Port    int    `json:"port"`
	PID     int    `json:"pid"`
	Command string `json:"command"`
	Address string `json:"address"`
}

func ListenersSupported() bool { return true }

func Listeners() []Listener {
	type sock struct {
		port int
		addr string
	}
	socks := map[int]sock{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[min(1, len(lines)):] {
			f := strings.Fields(line)
			if len(f) < 10 || f[3] != "0A" {
				continue
			}
			inode, err := strconv.Atoi(f[9])
			if err != nil || inode == 0 {
				continue
			}
			host, portHex, ok := strings.Cut(f[1], ":")
			if !ok {
				continue
			}
			port, err := strconv.ParseUint(portHex, 16, 32)
			if err != nil {
				continue
			}
			socks[inode] = sock{port: int(port), addr: hexAddr(host)}
		}
	}
	uid := os.Getuid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Listener
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if st, err := os.Stat(filepath.Join("/proc", e.Name())); err != nil || !ownedBy(st, uid) {
			continue
		}
		dir := fmt.Sprintf("/proc/%d/fd", pid)
		fds, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(dir, fd.Name()))
			if err != nil || !strings.HasPrefix(target, "socket:[") {
				continue
			}
			inode, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]"))
			if err != nil {
				continue
			}
			s, ok := socks[inode]
			if !ok {
				continue
			}
			key := strconv.Itoa(s.port) + "/" + strconv.Itoa(pid)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Listener{Port: s.port, PID: pid, Command: procCommand(pid), Address: s.addr})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].PID < out[j].PID
	})
	return out
}

func procCommand(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		comm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		return strings.TrimSpace(string(comm))
	}
	parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	cmd := strings.Join(parts, " ")
	if len(cmd) > 300 {
		cmd = cmd[:300] + "…"
	}
	return cmd
}

func ProcessUID(pid int) (int, bool) {
	st, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	if err != nil {
		return 0, false
	}
	return statUID(st)
}

func hexAddr(h string) string {
	switch h {
	case "00000000", "00000000000000000000000000000000":
		return "*"
	case "0100007F":
		return "127.0.0.1"
	case "00000000000000000000000001000000":
		return "::1"
	}
	if len(h) == 8 {
		b := make([]string, 4)
		for i := 0; i < 4; i++ {
			v, _ := strconv.ParseUint(h[6-2*i:8-2*i], 16, 8)
			b[i] = strconv.Itoa(int(v))
		}
		return strings.Join(b, ".")
	}
	return "ipv6"
}
