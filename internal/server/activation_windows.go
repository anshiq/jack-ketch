//go:build windows

package server

import "net"

func activationListener() (net.Listener, bool) { return nil, false }
