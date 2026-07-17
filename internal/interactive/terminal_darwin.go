//go:build darwin

package interactive

import "golang.org/x/sys/unix"

func readTerminalModes(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, unix.TIOCGETA)
}

func writeTerminalModes(fd int, modes *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, modes)
}
