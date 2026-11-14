//go:build !windows

package core

import (
	"os"
	"syscall"
)

func fileIno(st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Ino)
	}
	return 0
}
