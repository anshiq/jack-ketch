//go:build !linux

package process

type Listener struct {
	Port    int    `json:"port"`
	PID     int    `json:"pid"`
	Command string `json:"command"`
	Address string `json:"address"`
}

func ListenersSupported() bool { return false }

func Listeners() []Listener { return nil }

func ProcessUID(pid int) (int, bool) { return 0, false }
