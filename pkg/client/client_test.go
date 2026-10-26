package client

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func serveSocket(t *testing.T, sock string, handler http.Handler) {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
}

func TestCallAppliesDefaultDeadline(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	serveSocket(t, sock, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Write([]byte(`{"daemonVersion":"x","apiVersion":"v1","minClientVersion":"v1","shimProtocol":1}`))
	}))
	c := New(sock, "test/1")
	if _, err := c.GetVersion(context.Background()); err != nil {
		t.Fatalf("short call failed: %v", err)
	}
}

func TestCallDeadlineFromCaller(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	serveSocket(t, sock, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(`{}`))
	}))
	c := New(sock, "test/1")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.LogService().WaitForLog(ctx, map[string]any{"processId": "p1", "contains": "x"})
	if err == nil {
		t.Fatal("expected deadline error")
	}
	if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "context") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestHeadersSent(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	got := make(chan string, 1)
	serveSocket(t, sock, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case got <- r.Header.Get(SessionHeader):
		default:
		}
		w.Write([]byte(`{"daemonVersion":"x","apiVersion":"v1","minClientVersion":"v1","shimProtocol":1}`))
	}))
	c := New(sock, "test/1")
	c.SetHeader(SessionHeader, "sess-42")
	if _, err := c.GetVersion(context.Background()); err != nil {
		t.Fatalf("call: %v", err)
	}
	select {
	case v := <-got:
		if v != "sess-42" {
			t.Fatalf("session header = %q, want sess-42", v)
		}
	case <-time.After(time.Second):
		t.Fatal("no request seen")
	}
}

func TestDialRecoveryRetriesOnce(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "late.sock")
	var hookCalls atomic.Int32
	c := New(sock, "test/1")
	c.OnDialError(func() error {
		hookCalls.Add(1)
		ln, err := net.Listen("unix", sock)
		if err != nil {
			return err
		}
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"daemonVersion":"x","apiVersion":"v1","minClientVersion":"v1","shimProtocol":1}`))
		})}
		go srv.Serve(ln)
		t.Cleanup(func() { srv.Close() })
		return nil
	})
	if _, err := c.GetVersion(context.Background()); err != nil {
		t.Fatalf("call after recovery: %v", err)
	}
	if n := hookCalls.Load(); n != 1 {
		t.Fatalf("hook calls = %d, want 1", n)
	}
}
