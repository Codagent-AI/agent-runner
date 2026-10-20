//go:build !windows

package audit

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openUntrackedFileNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path) // #nosec G115 -- unix.Open returns a non-negative native file descriptor.
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open untracked file descriptor")
	}
	return file, nil
}

// Root-relative no-follow opens also avoid blocking on a file replaced by a FIFO.
func rootOpenNoFollow(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
}
