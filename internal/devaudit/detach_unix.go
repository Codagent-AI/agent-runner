//go:build dev_audit && (darwin || linux)

package devaudit

import "syscall"

func configureDetachedProcess(attr *syscall.SysProcAttr) { attr.Setsid = true }
