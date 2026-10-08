package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

func validateExternalUserSettings(dir, timeout string) (*model.ExternalUserSettings, error) {
	if dir == "" {
		return nil, fmt.Errorf("--external-user requires a directory")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("--external-user %q: %w", dir, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("--external-user directory %q: %w", absolute, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("--external-user %q is not a directory", absolute)
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return nil, fmt.Errorf("read --external-user directory %q: %w", absolute, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".request.json") || strings.HasSuffix(name, ".reply.json") {
			return nil, fmt.Errorf("--external-user directory contains stale exchange file %q", filepath.Join(absolute, name))
		}
	}
	probe, err := os.CreateTemp(absolute, ".agent-runner-write-*")
	if err != nil {
		return nil, fmt.Errorf("--external-user directory %q is not writable: %w", absolute, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	if timeout != "" {
		d, err := time.ParseDuration(timeout)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("invalid --external-user-timeout %q: expected a positive Go duration", timeout)
		}
	}
	return &model.ExternalUserSettings{Dir: absolute, Timeout: timeout}, nil
}

func validateExternalUserFlagCombinations(resume, intake bool, dir, timeout string) error {
	if resume && (dir != "" || timeout != "") {
		return fmt.Errorf("--resume reuses saved external-user settings; do not pass --external-user or --external-user-timeout")
	}
	if timeout != "" && dir == "" {
		return fmt.Errorf("--external-user-timeout requires --external-user")
	}
	if intake && dir != "" {
		return fmt.Errorf("-i cannot be combined with --external-user")
	}
	return nil
}
func mergeGlobalRunOptions(runOpts *runCommandOptions, opts *commandFlags) {
	if runOpts.until == "" {
		runOpts.until = opts.until
	}
	if runOpts.externalUser == "" {
		runOpts.externalUser = opts.externalUser
	}
	if runOpts.externalUserTimeout == "" {
		runOpts.externalUserTimeout = opts.externalUserTimeout
	}
}
func dispatchResumeCommand(args []string, runOpts *runCommandOptions, opts *commandFlags) int {
	if err := validateExternalUserFlagCombinations(true, false, runOpts.externalUser, runOpts.externalUserTimeout); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	if runOpts.until != "" && len(args) == 0 {
		fmt.Fprintln(os.Stderr, "agent-runner: --until on --resume requires a run ID")
		return 1
	}
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "agent-runner: --resume accepts at most one argument (the session ID)")
		return 1
	}
	if len(args) == 0 {
		return handleListWithProfile(opts.profileOverride())
	}
	if runOpts.until == "" {
		return handleResume(args[0], opts.profileOverride())
	}
	return handleResumeWithUntil(args[0], runOpts.until, opts.profileOverride())
}
func parseExternalUserArg(args []string, index int, opts *runCommandOptions) (next int, handled bool, err error) {
	name, value, hasValue := strings.Cut(args[index], "=")
	var destination *string
	switch name {
	case "--external-user":
		destination = &opts.externalUser
	case "--external-user-timeout":
		destination = &opts.externalUserTimeout
	default:
		return index, false, nil
	}
	if !hasValue {
		index++
		if index >= len(args) {
			return index, true, fmt.Errorf("%s requires a value", name)
		}
		value = args[index]
	}
	if strings.TrimSpace(value) == "" {
		return index, true, fmt.Errorf("%s requires a value", name)
	}
	*destination = value
	return index, true, nil
}

func configureExternalUserNoTUI(headless bool, dir string) {
	if headless || dir != "" {
		_ = os.Setenv("AGENT_RUNNER_NO_TUI", "1")
	}
}

func validateExternalUserGlobalFlags(resume, intake bool, dir, timeout string) error {
	var dirSet, timeoutSet bool
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "external-user":
			dirSet = true
		case "external-user-timeout":
			timeoutSet = true
		}
	})
	if resume && (dirSet || timeoutSet) {
		return fmt.Errorf("--resume reuses saved external-user settings; do not pass --external-user or --external-user-timeout")
	}
	if dirSet && dir == "" {
		return fmt.Errorf("--external-user requires a directory")
	}
	if timeoutSet && timeout == "" {
		return fmt.Errorf("--external-user-timeout requires a duration")
	}
	return validateExternalUserFlagCombinations(resume, intake, dir, timeout)
}
