package daemon

import "testing"

func TestSpawnEnv(t *testing.T) {
	got := SpawnEnv([]string{"PATH=/bin", "CLAUDECODE=1", "CODEX_THREAD_ID=x", "HOME=/h"}, "/x/jack-ketch")
	want := []string{"PATH=/bin", "HOME=/h", "JACK_KETCH_BIN=/x/jack-ketch"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
