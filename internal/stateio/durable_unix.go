//go:build !windows

package stateio

import "os"

func syncDirectory(path string) error {
	directory, err := os.Open(path) // #nosec G304 -- caller created the containing directory for its fixed output path.
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}
