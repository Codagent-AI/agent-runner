//go:build linux

package interactive

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func ReadProcessIdentity(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	// The command name is parenthesized and may itself contain spaces or
	// parentheses. Field 22 is therefore counted only after the final ')'.
	closeParen := strings.LastIndexByte(string(data), ')')
	if closeParen < 0 {
		return "", fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data[closeParen+1:]))
	if len(fields) < 20 {
		return "", fmt.Errorf("malformed /proc/%d/stat: missing starttime", pid)
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse /proc/%d/stat starttime: %w", pid, err)
	}
	return strconv.FormatUint(start, 10), nil
}
