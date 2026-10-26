//go:build linux || darwin

package server

import (
	"net"
	"os"

	"jack-ketch/internal/platform/ipc"
)

type peerListener struct {
	net.Listener
	uid uint32
}

func (l *peerListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if err := ipc.CheckPeer(c, l.uid); err != nil {
			_ = c.Close()
			continue
		}
		return c, nil
	}
}

func peerChecked(l net.Listener) net.Listener {
	return &peerListener{Listener: l, uid: uint32(os.Getuid())}
}
