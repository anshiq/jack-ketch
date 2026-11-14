package core

import "jack-ketch/internal/process"

func (e *Engine) CloseStaleInstances() int {
	lives, err := e.store.LiveInstances()
	if err != nil {
		return 0
	}
	n := 0
	for _, inst := range lives {
		if process.PIDAlive(int(inst.PID)) {
			continue
		}
		if e.store.MarkInstanceExited(inst.ID, "daemon-restart") == nil {
			n++
		}
	}
	return n
}
