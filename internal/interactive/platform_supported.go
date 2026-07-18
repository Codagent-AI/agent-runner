//go:build darwin || linux

package interactive

import (
	"os"
	"strconv"
)

func interactivePlatformError() error { return nil }

func platformUserID() string { return strconv.Itoa(os.Getuid()) }
