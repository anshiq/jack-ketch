//go:build linux
// +build linux

// Package ipc provides Unix domain socket and peer-credential
// functionality for the daemon. The daemon listens on a UDS with
// strict perms and authenticates every connection via SO_PEERCRED.
package ipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// PeerUID returns the OS user ID of the peer connected to conn
// using SO_PEERCRED. It errors when the credential cannot be read
// so callers fail closed.
func PeerUID(conn net.Conn) (uint32, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("ipc: peeruid: not a unix conn (%T)", conn)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("ipc: peeruid syscallconn: %w", err)
	}
	var uid uint32
	var ucredErr error
	ctrlErr := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			ucredErr = err
			return
		}
		uid = cred.Uid
	})
	if ctrlErr != nil {
		return 0, fmt.Errorf("ipc: peeruid control: %w", ctrlErr)
	}
	if ucredErr != nil {
		return 0, fmt.Errorf("ipc: peeruid getsockopt: %w", ucredErr)
	}
	return uid, nil
}

// CheckPeer verifies the peer uid matches want (normally os.Getuid()).
// It is a small helper so handlers fail closed with a useful error.
func CheckPeer(conn net.Conn, want uint32) error {
	got, err := PeerUID(conn)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("ipc: peer uid %d != daemon uid %d", got, want)
	}
	return nil
}
