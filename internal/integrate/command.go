package integrate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func binName() string {
	if runtime.GOOS == "windows" {
		return "jack-ketch.exe"
	}
	return "jack-ketch"
}

func isJackKetchBinary(path string) bool {
	return strings.TrimSuffix(filepath.Base(path), ".exe") == "jack-ketch"
}

func realPath(path string) string {
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return r
}

func unstablePath(path string) bool {
	clean := filepath.Clean(path)
	if strings.Contains(clean, string(filepath.Separator)+"go-build") {
		return true
	}
	tmp := filepath.Clean(os.TempDir())
	return strings.HasPrefix(clean, tmp+string(filepath.Separator))
}

func ResolveServerCommand(portable bool) (string, error) {
	if portable {
		return "jack-ketch", nil
	}
	if v := os.Getenv("JACK_KETCH_BIN"); v != "" {
		return v, nil
	}
	self, _ := os.Executable()
	selfIsCLI := self != "" && isJackKetchBinary(self)
	if found, err := exec.LookPath(binName()); err == nil {
		if abs, err := filepath.Abs(found); err == nil {
			if !selfIsCLI || realPath(abs) == realPath(self) {
				return abs, nil
			}
		}
	}
	if selfIsCLI {
		if unstablePath(self) {
			return "", fmt.Errorf("refusing to write the temporary build path %s into agent configs; install jack-ketch first (make install or go install ./cmd/jack-ketch) or set JACK_KETCH_BIN", self)
		}
		return self, nil
	}
	if self != "" {
		sib := filepath.Join(filepath.Dir(self), binName())
		if st, err := os.Stat(sib); err == nil && !st.IsDir() {
			return sib, nil
		}
	}
	return "", errors.New("cannot locate the jack-ketch binary; put it on PATH or set JACK_KETCH_BIN")
}
