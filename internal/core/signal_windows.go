//go:build windows

package core

import "errors"

func signalPID(int, bool) error { return errors.New("not supported on Windows") }
