//go:build darwin
// +build darwin

// Package ipc on macOS: UDS with LOCAL_PEERCRED (effective uid check).
package ipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// PeerUID returns the effective uid of the peer via LOCAL_PEERCRED,
// failing closed when credentials are unreadable.
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
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
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

// CheckPeer verifies the peer uid matches want.
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
