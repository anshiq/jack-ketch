//go:build !windows

package server

import (
	"net"
	"os"
	"strconv"
)

func activationListener() (net.Listener, bool) {
	if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) {
		return nil, false
	}
	n, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil || n < 1 {
		return nil, false
	}
	f := os.NewFile(3, "jackketchd.sock")
	l, err := net.FileListener(f)
	_ = f.Close()
	_ = os.Unsetenv("LISTEN_PID")
	_ = os.Unsetenv("LISTEN_FDS")
	_ = os.Unsetenv("LISTEN_FDNAMES")
	if err != nil {
		return nil, false
	}
	return l, true
}
