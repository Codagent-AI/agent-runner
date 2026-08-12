package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/codagent/agent-runner/internal/agentcall"
	agentconfig "github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/control"
	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/intakeroute"
	"github.com/codagent/agent-runner/internal/interactive"
	"github.com/codagent/agent-runner/internal/profilewrite"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/usersettings"
	"gopkg.in/yaml.v3"
)

type writeProfilePayload struct {
	LeadCLI          string `json:"lead_cli"`
	LeadModel        string `json:"lead_model"`
	CrosscheckCLI    string `json:"crosscheck_cli"`
	CrosscheckModel  string `json:"crosscheck_model"`
	ImplementorCLI   string `json:"implementor_cli"`
	ImplementorModel string `json:"implementor_model"`
	TesterCLI        string `json:"tester_cli"`
	TesterModel      string `json:"tester_model"`
	TargetPath       string `json:"target_path"`
}

func (p *writeProfilePayload) request() *profilewrite.Request {
	return &profilewrite.Request{
		LeadCLI:          p.LeadCLI,
		LeadModel:        p.LeadModel,
		CrosscheckCLI:    p.CrosscheckCLI,
		CrosscheckModel:  p.CrosscheckModel,
		ImplementorCLI:   p.ImplementorCLI,
		ImplementorModel: p.ImplementorModel,
		TesterCLI:        p.TesterCLI,
		TesterModel:      p.TesterModel,
		TargetPath:       p.TargetPath,
	}
}

func handleInternal(args []string) int {
	if len(args) > 0 && args[0] == "watchdog" {
		parent := os.NewFile(3, "agent-runner-watchdog-parent")
		if parent == nil {
			_, _ = fmt.Fprintln(os.Stderr, "agent-runner: watchdog parent pipe is unavailable")
			return 1
		}
		defer func() { _ = parent.Close() }()
		return handleWatchdog(args[1:], parent, os.Stderr)
	}
	return handleInternalWithIO(args, os.Stdin, os.Stderr)
}

func handleInternalWithIO(args []string, stdin io.Reader, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "agent-runner: missing internal command")
		return 1
	}
	switch args[0] {
	case "launch-intake-route":
		return handleLaunchIntakeRoute(args[1:], stderr)
	case "watchdog":
		return handleWatchdog(args[1:], stdin, stderr)
	case "turn-committed":
		return handleTurnCommitted(args, stderr)
	case "call-agent-mcp":
		return handleCallAgentMCP(args, stderr)
	case "write-profile":
		if len(args) != 1 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal write-profile accepts no arguments")
			return 1
		}
		var payload writeProfilePayload
		if err := decodeWriteProfilePayload(stdin, &payload); err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		if err := writeProfile(&payload); err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		return 0
	case "write-setting":
		if len(args) != 3 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal write-setting requires key and value")
			return 1
		}
		if err := writeSetting(args[1], args[2]); err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		return 0
	case "configured-agent-clis":
		if len(args) != 1 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal configured-agent-clis accepts no arguments")
			return 1
		}
		value, err := configuredAgentCLIs(filepath.Join(".agent-runner", "config.yaml"))
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprint(os.Stdout, value)
		return 0
	case "validator-init":
		if len(args) != 1 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal validator-init accepts no arguments")
			return 1
		}
		if err := runValidatorInit(filepath.Join(".agent-runner", "config.yaml")); err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		return 0
	case "json-value":
		if len(args) != 2 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal json-value requires key")
			return 1
		}
		value, err := decodeJSONStringField(stdin, args[1])
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprint(os.Stdout, value)
		return 0
	case "json-list-count":
		if len(args) != 2 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal json-list-count requires key")
			return 1
		}
		values, err := decodeJSONStringListField(stdin, args[1])
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprint(os.Stdout, len(values))
		return 0
	case "json-list-join":
		if len(args) != 2 {
			_, _ = fmt.Fprintln(stderr, "agent-runner: internal json-list-join requires key")
			return 1
		}
		values, err := decodeJSONStringListField(stdin, args[1])
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprint(os.Stdout, strings.Join(values, ", "))
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "agent-runner: unknown internal command %q\n", args[0])
		return 1
	}
}

func handleLaunchIntakeRoute(args []string, stderr io.Writer) int {
	if len(args) != 1 || !filepath.IsAbs(args[0]) {
		_, _ = fmt.Fprintln(stderr, "agent-runner: internal launch-intake-route requires one absolute sidecar path")
		return 1
	}
	sealed, err := intakeroute.LoadStrict(args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner: launch intake route: %v\n", err)
		return 1
	}
	if err := intakeroute.ValidateLaunchSealed(sealed); err != nil {
		return reportIntakeLaunchError(stderr, sealed, err)
	}
	// prepareFreshRun loads the sealed source itself and reports through the same
	// error path, so no pre-flight load is needed here.
	info, err := os.Stat(sealed.HandoffPath) // #nosec G703 -- handoff is validated as a sealed run-owned artifact before launch.
	if err != nil {
		return reportIntakeLaunchError(stderr, sealed, fmt.Errorf("stat sealed handoff: %w", err))
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return reportIntakeLaunchError(stderr, sealed, fmt.Errorf("sealed handoff must be a non-empty regular file"))
	}

	launchLog := iexec.Logger(&runner.DiscardLogger{})
	if os.Getenv("AGENT_RUNNER_NO_TUI") == "1" {
		launchLog = &realLogger{}
	}
	h, err := prepareFreshRun(&freshRunRequest{
		SourceRef:           sealed.SourceRef,
		Keyed:               sealed.Params,
		IntakeParentRunID:   sealed.ParentRunID,
		IntakeHandoffSource: sealed.HandoffPath,
		Log:                 launchLog,
	})
	if err != nil {
		return reportIntakeLaunchError(stderr, sealed, err)
	}
	if os.Getenv("AGENT_RUNNER_NO_TUI") == "1" {
		result := runner.ExecuteFromHandle(h, &runner.Options{
			ProcessRunner: &realProcessRunner{},
			GlobExpander:  &realGlobExpander{},
			Log:           &realLogger{},
		})
		return resultExitCode(result)
	}
	if code := ensureThemeForTUI(defaultThemeDeps); code != 0 {
		return code
	}
	return runLiveTUIWithResult(h, liveTUIOptions{}.withEnv()).exitCode
}

func reportIntakeLaunchError(stderr io.Writer, sealed *intakeroute.Sealed, err error) int {
	_, _ = fmt.Fprintf(stderr, "agent-runner: launch intake route: %v\n", err)
	if sealed != nil && sealed.HandoffPath != "" {
		_, _ = fmt.Fprintf(stderr, "agent-runner: sealed handoff: %s\n", sealed.HandoffPath)
	}
	return 1
}

func handleCallAgentMCP(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "agent-runner: internal call-agent-mcp accepts no arguments")
		return 1
	}
	if err := agentcall.RunStdio(context.Background(), os.Getenv); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner: call-agent MCP bridge: %v\n", err)
		return 1
	}
	return 0
}

func handleWatchdog(args []string, parent io.Reader, stderr io.Writer) int {
	flags := flag.NewFlagSet("internal watchdog", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pid := flags.Int("pid", 0, "child process id")
	pgid := flags.Int("pgid", 0, "child process group id")
	startTime := flags.String("start-time", "", "child process start identity")
	grace := flags.Duration("grace", interactive.DefaultTerminationGrace, "termination grace")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *pid <= 0 || *pgid <= 0 || *startTime == "" || *grace <= 0 {
		_, _ = fmt.Fprintln(stderr, "agent-runner: watchdog requires --pid, --pgid, --start-time, and a positive --grace")
		return 1
	}
	metadata := interactive.ProcessMetadata{ChildPID: *pid, PGID: *pgid, StartTime: *startTime}
	if err := interactive.RunWatchdog(parent, metadata, *grace); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner: watchdog: %v\n", err)
		return 1
	}
	return 0
}

func handleTurnCommitted(args []string, stderr io.Writer) int {
	// Codex appends one JSON notification payload to notify commands. Other
	// hooks invoke the same sender without it; the authenticated environment is
	// the only input used for the control event.
	if len(args) < 1 || len(args) > 2 {
		_, _ = fmt.Fprintln(stderr, "agent-runner: internal turn-committed accepts only the optional hook payload")
		return 1
	}
	if _, err := sendControlMessage(control.MessageTurnCommitted); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner: %v\n", err)
		return 1
	}
	return 0
}

func configuredAgentCLIs(projectConfigPath string) (string, error) {
	values, err := agentconfig.ConfiguredCLIs(projectConfigPath)
	if err != nil {
		return "", err
	}
	return strings.Join(values, ","), nil
}

func validatorInitArgs(projectConfigPath string) ([]string, error) {
	args := []string{"init"}
	clis, err := agentconfig.ConfiguredCLIs(projectConfigPath)
	if err != nil {
		return nil, err
	}
	if len(clis) > 0 {
		args = append(args, "--agents")
		args = append(args, clis...)
	}
	args = append(args, "--enable-builtin", "task-compliance")
	return args, nil
}

func runValidatorInit(projectConfigPath string) error {
	args, err := validatorInitArgs(projectConfigPath)
	if err != nil {
		return err
	}
	cmd := exec.Command("agent-validator", args...) // #nosec G204 -- executable is fixed and args are validated config CLI names
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func decodeJSONStringField(r io.Reader, key string) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&fields); err != nil {
		return "", fmt.Errorf("decode json input: %w", err)
	}
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("json input missing %s", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("json field %s must be a string: %w", key, err)
	}
	return value, nil
}

func decodeJSONStringListField(r io.Reader, key string) ([]string, error) {
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&fields); err != nil {
		return nil, fmt.Errorf("decode json input: %w", err)
	}
	raw, ok := fields[key]
	if !ok {
		return nil, fmt.Errorf("json input missing %s", key)
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("json field %s must be an array of strings: %w", key, err)
	}
	return values, nil
}

func decodeWriteProfilePayload(r io.Reader, payload *writeProfilePayload) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(payload); err != nil {
		return fmt.Errorf("decode write-profile payload: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("write-profile payload must contain a single JSON object")
		}
		return fmt.Errorf("decode trailing write-profile payload: %w", err)
	}
	if payload.LeadCLI == "" {
		return fmt.Errorf("write-profile payload missing lead_cli")
	}
	if payload.CrosscheckCLI == "" {
		return fmt.Errorf("write-profile payload missing crosscheck_cli")
	}
	if payload.ImplementorCLI == "" {
		return fmt.Errorf("write-profile payload missing implementor_cli")
	}
	if payload.TesterCLI == "" {
		return fmt.Errorf("write-profile payload missing tester_cli")
	}
	if payload.TargetPath == "" {
		return fmt.Errorf("write-profile payload missing target_path")
	}
	return nil
}

func writeProfile(payload *writeProfilePayload) error {
	return profilewrite.Write(payload.request())
}

func mergeProfileAgents(doc *yaml.Node, payload *writeProfilePayload) error {
	return profilewrite.Merge(doc, payload.request())
}

func writeSetting(key, value string) error {
	settings, err := usersettings.Load()
	if err != nil {
		return err
	}
	switch key {
	case "setup.completed_at":
		settings.Setup.CompletedAt = value
	case "onboarding.completed_at":
		settings.Onboarding.CompletedAt = value
	case "onboarding.dismissed":
		settings.Onboarding.Dismissed = value
	default:
		return fmt.Errorf("unsupported setting key %q", key)
	}
	return usersettings.Save(settings)
}
