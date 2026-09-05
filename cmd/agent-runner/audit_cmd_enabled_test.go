//go:build dev_audit

package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/codagent/agent-runner/internal/devaudit"
)

func TestAuditSetupValidatesFlagsAndDelegates(t *testing.T) {
	preserveAuditCommandHooks(t)
	called := false
	importAuditConnection = func(input devaudit.SetupInput) error {
		called = true
		if input.ClientPath != "client.json" || input.TokenPath != "token.json" || input.SpreadsheetID != "sheet" || input.Tab != "audit" {
			t.Fatalf("setup input = %#v", input)
		}
		return nil
	}
	var stdout, stderr bytes.Buffer
	if code := handleAuditSetupCommand([]string{"--client", "client.json"}, &stdout, &stderr); code != 1 || called {
		t.Fatalf("incomplete setup code=%d called=%v", code, called)
	}
	stderr.Reset()
	if code := handleAuditSetupCommand([]string{"--client", "client.json", "--token", "token.json", "--spreadsheet", "sheet", "--tab", "audit"}, &stdout, &stderr); code != 0 || !called {
		t.Fatalf("valid setup code=%d called=%v stderr=%q", code, called, stderr.String())
	}
}

func TestAuditSetupReportsDelegationFailure(t *testing.T) {
	preserveAuditCommandHooks(t)
	importAuditConnection = func(devaudit.SetupInput) error { return errors.New("import failed") }
	var stdout, stderr bytes.Buffer
	code := handleAuditSetupCommand([]string{"--client", "client.json", "--token", "token.json", "--spreadsheet", "sheet", "--tab", "audit"}, &stdout, &stderr)
	if code != 1 || !bytes.Contains(stderr.Bytes(), []byte("import failed")) {
		t.Fatalf("setup failure code=%d stderr=%q", code, stderr.String())
	}
}

func TestAuditRetryValidatesMigrationPairAndDelegates(t *testing.T) {
	preserveAuditCommandHooks(t)
	var migrated, retried bool
	migrateAuditReportDestination = func(dir, spreadsheet, tab string) error {
		migrated = true
		if dir != "audit-dir" || spreadsheet != "sheet" || tab != "tab" {
			t.Fatalf("migration = %q %q %q", dir, spreadsheet, tab)
		}
		return nil
	}
	retryAuditReport = func(dir string) error {
		retried = true
		if dir != "audit-dir" {
			t.Fatalf("retry dir = %q", dir)
		}
		return nil
	}
	var stdout, stderr bytes.Buffer
	if code := handleAuditRetryCommand([]string{"audit-dir", "--migrate-spreadsheet", "sheet"}, &stdout, &stderr); code != 1 || migrated || retried {
		t.Fatalf("unpaired migration code=%d migrated=%v retried=%v", code, migrated, retried)
	}
	stderr.Reset()
	if code := handleAuditRetryCommand([]string{"audit-dir", "--migrate-spreadsheet", "sheet", "--migrate-tab", "tab"}, &stdout, &stderr); code != 0 || !migrated || !retried {
		t.Fatalf("retry code=%d migrated=%v retried=%v stderr=%q", code, migrated, retried, stderr.String())
	}
}

func TestAuditRetryReportsDelegationFailures(t *testing.T) {
	t.Run("migration", func(t *testing.T) {
		preserveAuditCommandHooks(t)
		migrateAuditReportDestination = func(string, string, string) error { return errors.New("migration failed") }
		retryAuditReport = func(string) error { t.Fatal("retry called after migration failure"); return nil }
		var stdout, stderr bytes.Buffer
		code := handleAuditRetryCommand([]string{"audit-dir", "--migrate-spreadsheet", "sheet", "--migrate-tab", "tab"}, &stdout, &stderr)
		if code != 1 || !bytes.Contains(stderr.Bytes(), []byte("migration failed")) {
			t.Fatalf("migration failure code=%d stderr=%q", code, stderr.String())
		}
	})
	t.Run("retry", func(t *testing.T) {
		preserveAuditCommandHooks(t)
		retryAuditReport = func(string) error { return errors.New("delivery failed") }
		var stdout, stderr bytes.Buffer
		code := handleAuditRetryCommand([]string{"audit-dir"}, &stdout, &stderr)
		if code != 1 || !bytes.Contains(stderr.Bytes(), []byte("delivery failed")) {
			t.Fatalf("retry failure code=%d stderr=%q", code, stderr.String())
		}
	})
}

func preserveAuditCommandHooks(t *testing.T) {
	t.Helper()
	originalImport, originalMigrate, originalRetry := importAuditConnection, migrateAuditReportDestination, retryAuditReport
	t.Cleanup(func() {
		importAuditConnection, migrateAuditReportDestination, retryAuditReport = originalImport, originalMigrate, originalRetry
	})
}
