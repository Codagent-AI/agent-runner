//go:build !dev_audit

package main

import "io"

func handleDevelopmentAuditCommand(_ []string, _, _ io.Writer) (handled bool, code int) {
	return false, 0
}
