//go:build linux

package interactive

import "golang.org/x/sys/unix"

func readTerminalModes(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, unix.TCGETS)
}

func writeTerminalModes(fd int, modes *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TCSETS, modes)
}
