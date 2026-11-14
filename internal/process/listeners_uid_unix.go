//go:build linux

package process

import (
	"os"
	"syscall"
)

func statUID(st os.FileInfo) (int, bool) {
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(s.Uid), true
}

func ownedBy(st os.FileInfo, uid int) bool {
	u, ok := statUID(st)
	return ok && u == uid
}
