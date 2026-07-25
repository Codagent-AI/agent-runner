//go:build darwin || linux

package control

import (
	"os"
	"strconv"
)

func controlPlatformError() error { return nil }

func platformUserID() string { return strconv.Itoa(os.Getuid()) }
