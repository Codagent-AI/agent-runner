//go:build dev_audit && darwin

package devaudit

import "testing"

func TestDarwinAuditEvidenceSymlinkReadOnly(t *testing.T) {
	testAuditEvidenceSymlinkReadOnly(t)
}
