package mcp

import (
	"context"
	"errors"
	"time"

	"jack-ketch/internal/runtime"
)

type LazyBackend struct {
	ready chan struct{}
	rt    runtime.Facade
	err   error
	mode  string
}

func NewLazyBackend() *LazyBackend { return &LazyBackend{ready: make(chan struct{})} }

func (l *LazyBackend) Resolve(rt runtime.Facade, mode string, err error) {
	l.rt, l.mode, l.err = rt, mode, err
	close(l.ready)
}

func (l *LazyBackend) Get(ctx context.Context) (runtime.Facade, error) {
	select {
	case <-l.ready:
		return l.rt, l.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, errors.New("jack-ketch is still connecting to its daemon; retry in a few seconds")
	}
}

func (l *LazyBackend) Mode() string {
	select {
	case <-l.ready:
		return l.mode
	default:
		return "connecting"
	}
}
