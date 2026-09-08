//go:build dev_audit && linux

package devaudit

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func init() {
	if len(os.Args) < 4 || os.Args[1] != "internal" || os.Args[2] != "audit-model-exec" {
		return
	}
	// Mark all nonstandard descriptors for closure on the final exec. CLOEXEC
	// avoids closing descriptors used by the Go runtime before exec replaces it.
	// Unsupported close_range is an audit failure, never a direct-exec fallback.
	err := unix.CloseRange(3, ^uint(0), unix.CLOSE_RANGE_CLOEXEC)
	if err == nil {
		var executable string
		executable, err = exec.LookPath(os.Args[3])
		if err == nil {
			err = syscall.Exec(executable, os.Args[3:], os.Environ()) // #nosec G204,G702 -- intentional adapter argv passthrough without shell interpolation, after OS confinement and descriptor closure.
		}
	}
	_, _ = fmt.Fprintf(os.Stderr, "audit model exec: %v\n", err)
	os.Exit(125)
}
