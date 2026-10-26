package server

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cpuSample struct {
	cpu int64
	at  time.Time
}

var cpuSamples = map[string]cpuSample{}
var cpuMu sync.Mutex

func usagePayload(processID string, pid int, mem, cpu int64, ports []int) map[string]any {
	tree := procTree(pid)
	if cpu <= 0 {
		cpu = tree.cpuNanos
	}
	if mem <= 0 {
		mem = tree.rss
	}
	cpuMu.Lock()
	prev, ok := cpuSamples[processID]
	now := time.Now()
	cpuSamples[processID] = cpuSample{cpu: cpu, at: now}
	cpuMu.Unlock()
	percent := 0.0
	if ok && cpu >= prev.cpu {
		dt := now.Sub(prev.at).Seconds()
		if dt > 0 {
			percent = float64(cpu-prev.cpu) / 1e9 / dt * 100
		}
	}
	return map[string]any{
		"processId": processID, "pid": pid,
		"memoryBytes": mem, "cpuNanos": cpu, "cpuPercent": percent,
		"rssBytes": tree.rss, "threads": tree.threads, "openFds": tree.fds,
		"children": tree.children, "ports": ports,
		"at": now.UnixNano(),
	}
}

type treeUsage struct {
	rss      int64
	cpuNanos int64
	threads  int
	fds      int
	children int
}

type procStat struct {
	ppid    int
	ticks   int64
	threads int
	rss     int64
}

const clockTicksPerSecond = 100

func readProcStat(pid string) (procStat, bool) {
	d, err := os.ReadFile("/proc/" + pid + "/stat")
	if err != nil {
		return procStat{}, false
	}
	idx := strings.LastIndex(string(d), ")")
	if idx < 0 {
		return procStat{}, false
	}
	f := strings.Fields(string(d)[idx+1:])
	if len(f) < 22 {
		return procStat{}, false
	}
	ppid, _ := strconv.Atoi(f[1])
	utime, _ := strconv.ParseInt(f[11], 10, 64)
	stime, _ := strconv.ParseInt(f[12], 10, 64)
	threads, _ := strconv.Atoi(f[17])
	pages, _ := strconv.ParseInt(f[21], 10, 64)
	return procStat{ppid: ppid, ticks: utime + stime, threads: threads, rss: pages * int64(os.Getpagesize())}, true
}

func procTree(pid int) treeUsage {
	if pid <= 0 {
		return treeUsage{}
	}
	root := strconv.Itoa(pid)
	stats := map[int]procStat{}
	kids := map[int][]int{}
	if entries, err := os.ReadDir("/proc"); err == nil {
		for _, e := range entries {
			n, err := strconv.Atoi(e.Name())
			if err != nil {
				continue
			}
			st, ok := readProcStat(e.Name())
			if !ok {
				continue
			}
			stats[n] = st
			kids[st.ppid] = append(kids[st.ppid], n)
		}
	}
	if _, ok := stats[pid]; !ok {
		st, ok := readProcStat(root)
		if !ok {
			return treeUsage{}
		}
		stats[pid] = st
	}
	var out treeUsage
	out.children = len(kids[pid])
	seen := map[int]bool{}
	queue := []int{pid}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		st := stats[cur]
		out.rss += st.rss
		out.threads += st.threads
		out.cpuNanos += st.ticks * (int64(time.Second) / clockTicksPerSecond)
		queue = append(queue, kids[cur]...)
	}
	if entries, err := os.ReadDir("/proc/" + root + "/fd"); err == nil {
		out.fds = len(entries)
	}
	return out
}
