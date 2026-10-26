package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestPragmasOnEveryConnection(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	conns := make([]*sql.Conn, 0, 4)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < 4; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		for _, pragma := range []struct {
			name string
			want string
		}{
			{"foreign_keys", "1"},
			{"busy_timeout", "5000"},
			{"journal_mode", "wal"},
			{"synchronous", "1"},
		} {
			var got string
			if err := c.QueryRowContext(ctx, "PRAGMA "+pragma.name).Scan(&got); err != nil {
				t.Fatalf("conn %d PRAGMA %s: %v", i, pragma.name, err)
			}
			if got != pragma.want {
				t.Fatalf("conn %d PRAGMA %s = %q, want %q", i, pragma.name, got, pragma.want)
			}
		}
	}
}
