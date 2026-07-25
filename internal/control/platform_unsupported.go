//go:build !darwin && !linux

package control

import (
	"fmt"
	"runtime"
)

func controlPlatformError() error {
	return fmt.Errorf("control sockets are unsupported on %s", runtime.GOOS)
}

func platformUserID() string { return "unsupported" }
