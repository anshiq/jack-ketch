//go:build !linux && !darwin

package server

import "net"

func peerChecked(l net.Listener) net.Listener { return l }
