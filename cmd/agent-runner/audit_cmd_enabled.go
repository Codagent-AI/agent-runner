//go:build dev_audit

package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/codagent/agent-runner/internal/devaudit"
)

var (
	importAuditConnection = func(input devaudit.SetupInput) error {
		return (devaudit.ConnectionStore{}).Import(input)
	}
	migrateAuditReportDestination = devaudit.MigrateReportDestination
	retryAuditReport              = devaudit.RetryReport
)

// handleDevelopmentAuditCommand keeps the tagged setup and retry CLI surface
// at the executable boundary. devaudit owns only protected storage and
// delivery operations.
func handleDevelopmentAuditCommand(args []string, stdout, stderr io.Writer) (handled bool, code int) {
	if len(args) == 0 || args[0] != "audit" {
		return false, 0
	}
	if len(args) == 1 || args[1] == "help" || args[1] == "--help" {
		_, _ = fmt.Fprintln(stdout, "Usage: agent-runner audit setup --client <file> --token <file> --spreadsheet <id> --tab <tab> | audit retry <audit-session-dir> [--migrate-spreadsheet <id> --migrate-tab <tab>] | audit status <session-dir> | audit replay <session-dir> --session <execution-session-id>")
		return true, 0
	}
	switch args[1] {
	case "setup":
		return true, handleAuditSetupCommand(args[2:], stdout, stderr)
	case "retry":
		return true, handleAuditRetryCommand(args[2:], stdout, stderr)
	default:
		return false, 0
	}
}

func handleAuditSetupCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("audit setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	client := fs.String("client", "", "installed OAuth client JSON")
	token := fs.String("token", "", "authorized-user OAuth token JSON")
	spreadsheet := fs.String("spreadsheet", "", "existing spreadsheet ID")
	tab := fs.String("tab", "", "existing worksheet tab")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 || *client == "" || *token == "" || *spreadsheet == "" || *tab == "" {
		_, _ = fmt.Fprintln(stderr, "Usage: agent-runner audit setup --client <file> --token <file> --spreadsheet <id> --tab <tab>")
		return 1
	}
	if err := importAuditConnection(devaudit.SetupInput{ClientPath: *client, TokenPath: *token, SpreadsheetID: *spreadsheet, Tab: *tab}); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner audit setup: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "agent-runner audit: Google Sheets connection imported")
	return 0
}

func handleAuditRetryCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "Usage: agent-runner audit retry <audit-session-dir> [--migrate-spreadsheet <id> --migrate-tab <tab>]")
		return 1
	}
	auditSessionDir := args[0]
	fs := flag.NewFlagSet("audit retry", flag.ContinueOnError)
	fs.SetOutput(stderr)
	migrateSpreadsheet := fs.String("migrate-spreadsheet", "", "explicit replacement spreadsheet ID")
	migrateTab := fs.String("migrate-tab", "", "explicit replacement worksheet tab")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "Usage: agent-runner audit retry <audit-session-dir> [--migrate-spreadsheet <id> --migrate-tab <tab>]")
		return 1
	}
	if (*migrateSpreadsheet == "") != (*migrateTab == "") {
		_, _ = fmt.Fprintln(stderr, "agent-runner audit retry: migration requires both --migrate-spreadsheet and --migrate-tab")
		return 1
	}
	if *migrateSpreadsheet != "" {
		if err := migrateAuditReportDestination(auditSessionDir, *migrateSpreadsheet, *migrateTab); err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner audit retry: %v\n", err)
			return 1
		}
	}
	if err := retryAuditReport(auditSessionDir); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner audit retry: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "agent-runner audit: report delivered")
	return 0
}
