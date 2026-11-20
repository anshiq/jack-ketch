package process

func PIDAlive(pid int) bool { return pid > 0 && pidAlive(pid) }
