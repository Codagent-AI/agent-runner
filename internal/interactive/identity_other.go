//go:build !darwin && !linux

package interactive

import "fmt"

func ReadProcessIdentity(pid int) (string, error) {
	return "", fmt.Errorf("process identity is unsupported on this platform for pid %d", pid)
}
