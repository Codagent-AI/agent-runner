//go:build windows

package audit

import "os"

func openUntrackedFileNoFollow(path string) (*os.File, error) {
	return os.Open(path) // #nosec G304 -- caller constrains the path and validates the opened descriptor before reading.
}
