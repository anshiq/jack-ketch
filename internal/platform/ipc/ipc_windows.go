//go:build windows
// +build windows

// Package ipc on Windows: AF_UNIX sockets (Win10 1803+) via Go's net
// package. Peer identity falls back to the 0700/0600 ACL-equivalent
// modes; SO_PEERCRED-style checks arrive with the named-pipe transport.
package ipc

import (
	"fmt"
	"net"
)

// PeerUID is not implemented on Windows (named-pipe
// GetNamedPipeClientProcessId transport is the follow-up); callers must
// rely on directory/socket ACLs.
func PeerUID(conn net.Conn) (uint32, error) {
	return 0, fmt.Errorf("ipc: peeruid not implemented on Windows")
}
