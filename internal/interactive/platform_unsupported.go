//go:build !darwin && !linux

package interactive

import (
	"fmt"
	"runtime"
)

func interactivePlatformError() error {
	return fmt.Errorf("interactive terminal handoff is unsupported on %s", runtime.GOOS)
}
