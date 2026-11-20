package logindex

import (
	"path/filepath"
	"testing"
)

func TestWorkspaceIndexRoundtrip(t *testing.T) {
	idx, err := OpenWorkspaceIndex(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer idx.Close()
	lines := []IndexedLine{
		{ProcessID: "proc_1", InstanceID: "run_1", Seq: 1, Stream: 1, Line: "server ready on port 3000"},
		{ProcessID: "proc_1", InstanceID: "run_1", Seq: 2, Stream: 2, Line: "timeout waiting for db"},
		{ProcessID: "proc_2", InstanceID: "run_2", Seq: 1, Stream: 1, Line: "worker started"},
	}
	if err := idx.AppendBatch(lines); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := idx.AppendBatch(lines); err != nil {
		t.Fatalf("re-append: %v", err)
	}
	hits, trunc, err := idx.Search("timeout", nil, nil, 0, 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if trunc || len(hits) != 1 {
		t.Fatalf("hits = %d, trunc=%v", len(hits), trunc)
	}
	if hits[0].ProcessID != "proc_1" {
		t.Fatalf("hit = %+v", hits[0])
	}
}
