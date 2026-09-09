//go:build windows

package stateio

// Windows cannot reliably sync an open directory through os.File.Sync. The
// replacement file has already been flushed before rename, which is the
// available durability boundary on this platform.
func syncDirectory(string) error { return nil }
