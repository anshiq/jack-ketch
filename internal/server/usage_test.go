package server

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestProcTreeReadsLiveProcess(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	time.Sleep(50 * time.Millisecond)
	tree := procTree(os.Getpid())
	if tree.rss <= 0 || tree.threads <= 0 || tree.fds <= 0 {
		t.Fatalf("expected live usage for self, got %+v", tree)
	}
	if tree.children < 1 {
		t.Fatalf("expected the sleep child to be counted, got %+v", tree)
	}
	if got := procTree(0); got != (treeUsage{}) {
		t.Fatalf("pid 0 should be empty, got %+v", got)
	}
}

func TestUsagePayloadFallsBackToProc(t *testing.T) {
	m := usagePayload("proc_usage_test", os.Getpid(), 0, 0, []int{})
	if m["memoryBytes"].(int64) <= 0 || m["rssBytes"].(int64) <= 0 {
		t.Fatalf("expected /proc memory fallback, got %v", m)
	}
	if m["cpuNanos"].(int64) <= 0 {
		t.Fatalf("expected /proc cpu fallback, got %v", m)
	}
}
