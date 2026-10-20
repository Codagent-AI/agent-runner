//go:build windows

package audit

import "os"

func openUntrackedFileNoFollow(path string) (*os.File, error) {
	return os.Open(path) // #nosec G304 -- caller constrains the path and validates the opened descriptor before reading.
}

func rootOpenNoFollow(root *os.Root, path string) (*os.File, error) { return root.Open(path) }
