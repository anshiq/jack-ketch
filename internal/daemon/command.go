package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ResolveJackKetchdCommand(exe string) []string {
	dir := filepath.Dir(exe)
	for _, name := range []string{"jackketchd", "jackketchd.exe"} {
		cand := filepath.Join(dir, name)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return []string{cand, "--foreground"}
		}
	}
	base := strings.TrimSuffix(filepath.Base(exe), ".exe")
	if base == "jack-ketch" {
		return []string{exe, "jackketchd", "--foreground"}
	}
	if p, err := exec.LookPath("jackketchd"); err == nil {
		return []string{p, "--foreground"}
	}
	return []string{exe, "jackketchd", "--foreground"}
}
