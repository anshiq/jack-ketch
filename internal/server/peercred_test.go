//go:build linux || darwin

package server

import (
	"bufio"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestPeerChecked_SameUIDAccepted(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "peer.sock")
	raw, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln := peerChecked(raw)
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write([]byte("x"))
	}()
	conn, err := net.DialTimeout("unix", sock, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	b, err := bufio.NewReader(conn).ReadByte()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if b != 'x' {
		t.Fatalf("read %q, want x", b)
	}
}
