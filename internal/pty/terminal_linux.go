//go:build linux

package pty

import (
	"os"

	"golang.org/x/sys/unix"
)

func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS) // #nosec G115 -- uintptr->int safe on supported platforms
	return err == nil
}

func makeRaw(fd uintptr) (*unix.Termios, error) {
	termios, err := unix.IoctlGetTermios(int(fd), unix.TCGETS) // #nosec G115 -- uintptr->int safe on supported platforms
	if err != nil {
		return nil, err
	}

	oldState := *termios

	termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	termios.Oflag &^= unix.OPOST
	termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	termios.Cflag &^= unix.CSIZE | unix.PARENB
	termios.Cflag |= unix.CS8
	termios.Cc[unix.VMIN] = 1
	termios.Cc[unix.VTIME] = 0

	if err := unix.IoctlSetTermios(int(fd), unix.TCSETS, termios); err != nil { // #nosec G115 -- uintptr->int safe on supported platforms
		return nil, err
	}

	return &oldState, nil
}

func restoreTerminal(fd uintptr, state *unix.Termios) {
	_ = unix.IoctlSetTermios(int(fd), unix.TCSETS, state) // #nosec G115 -- uintptr->int safe on supported platforms
}
