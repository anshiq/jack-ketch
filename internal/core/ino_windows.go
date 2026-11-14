//go:build windows

package core

import "os"

func fileIno(os.FileInfo) uint64 { return 0 }
