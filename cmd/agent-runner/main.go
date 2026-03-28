package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	_ "github.com/codagent/agent-runner/internal/engine/openspec"
	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
)

// realProcessRunner implements exec.ProcessRunner using os/exec.
type realProcessRunner struct{}

func (r *realProcessRunner) RunShell(cmd string, captureStdout bool) (iexec.ProcessResult, error) {
	c := exec.Command("sh", "-c", cmd) // #nosec G204 -- CLI runner executes user-defined shell commands by design
	c.Stdin = os.Stdin
	c.Stderr = os.Stderr

	if captureStdout {
		out, err := c.Output()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				return iexec.ProcessResult{}, err
			}
		}
		return iexec.ProcessResult{
			ExitCode: exitCode,
			Stdout:   strings.TrimSpace(string(out)),
		}, nil
	}

	c.Stdout = os.Stdout
	err := c.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return iexec.ProcessResult{}, err
		}
	}
	return iexec.ProcessResult{ExitCode: exitCode}, nil
}

func (r *realProcessRunner) RunAgent(args []string) (iexec.ProcessResult, error) {
	c := exec.Command(args[0], args[1:]...) // #nosec G204 -- CLI runner launches agent processes by design
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	err := c.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return iexec.ProcessResult{}, err
		}
	}
	return iexec.ProcessResult{ExitCode: exitCode}, nil
}

// realGlobExpander implements exec.GlobExpander using filepath.Glob.
type realGlobExpander struct{}

func (g *realGlobExpander) Expand(pattern string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if matches == nil {
		matches = []string{}
	}
	sort.Strings(matches)
	return matches, nil
}

// realLogger implements exec.Logger.
type realLogger struct{}

func (l *realLogger) Println(args ...any)               { fmt.Println(args...) }
func (l *realLogger) Printf(format string, args ...any) { fmt.Printf(format, args...) }
func (l *realLogger) Errorf(format string, args ...any) { fmt.Fprintf(os.Stderr, format, args...) }

func main() {
	os.Exit(run())
}

func run() int {
	rootCmd := &cobra.Command{
		Use:   "agent-runner",
		Short: "CLI workflow orchestrator for AI agents",
	}

	runCmd := &cobra.Command{
		Use:   "run <workflow.yaml> [params...] [--from <step>] [--session <id>]",
		Short: "Execute a workflow",
		Long: `Execute a workflow with positional or key=value parameters.
Parameters from the workflow's params list are required unless marked optional.
Positional args map to params in order; use key=value for explicit mapping.
  Examples:
    run workflow.yaml my-feature              # positional: maps to first param
    run workflow.yaml my-feature "a description"  # multiple positional params
    run workflow.yaml name=my-feature         # key=value format
    run workflow.yaml my-feature desc="more"  # mixed: positional + override`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflowFile := args[0]

			// Load workflow first to validate params against its schema.
			workflow, err := loader.LoadWorkflow(workflowFile, loader.Options{})
			if err != nil {
				return fmt.Errorf("load workflow: %w", err)
			}

			// Parse and match parameters.
			positional, keyed, err := parseParams(args[1:])
			if err != nil {
				return err
			}
			params, err := matchParams(&workflow, positional, keyed)
			if err != nil {
				return err
			}

			result, err := runner.RunWorkflow(&workflow, params, &runner.Options{
				WorkflowFile:  workflowFile,
				ProcessRunner: &realProcessRunner{},
				GlobExpander:  &realGlobExpander{},
				Log:           &realLogger{},
			})
			if err != nil {
				return err
			}
			if result != runner.ResultSuccess {
				cmd.SilenceUsage = true
				cmd.SilenceErrors = true
				return fmt.Errorf("workflow failed")
			}
			return nil
		},
	}

	validateCmd := &cobra.Command{
		Use:   "validate <workflow.yaml>",
		Short: "Validate a workflow file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := loader.LoadWorkflow(args[0], loader.Options{})
			if err != nil {
				return err
			}
			fmt.Println("workflow is valid")
			return nil
		},
	}

	resumeCmd := &cobra.Command{
		Use:   "resume <state-file>",
		Short: "Resume an interrupted workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := runner.ResumeWorkflow(args[0], &runner.Options{
				ProcessRunner: &realProcessRunner{},
				GlobExpander:  &realGlobExpander{},
				Log:           &realLogger{},
			})
			if err != nil {
				return err
			}
			if result != runner.ResultSuccess {
				cmd.SilenceUsage = true
				cmd.SilenceErrors = true
				return fmt.Errorf("workflow failed")
			}
			return nil
		},
	}

	rootCmd.AddCommand(runCmd, validateCmd, resumeCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	return 0
}

// parseParams separates positional args from key=value pairs.
// Returns (positional values, key=value map, error).
func parseParams(args []string) (positional []string, keyed map[string]string, err error) {
	positional = []string{}
	keyed = make(map[string]string)

	for _, arg := range args {
		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			if parts[0] == "" {
				return nil, nil, fmt.Errorf("invalid parameter format: empty key in %q", arg)
			}
			keyed[parts[0]] = parts[1]
		} else {
			positional = append(positional, arg)
		}
	}

	return positional, keyed, nil
}

// matchParams maps CLI args to workflow parameters, validating required params.
// Supports positional args (mapped to params in order) and key=value overrides.
func matchParams(workflow *model.Workflow, positional []string, keyed map[string]string) (map[string]string, error) {
	result := make(map[string]string)

	// Apply positional arguments to workflow params in order.
	if len(positional) > len(workflow.Params) {
		return nil, fmt.Errorf("too many arguments: expected %d, got %d", len(workflow.Params), len(positional))
	}

	for i, val := range positional {
		result[workflow.Params[i].Name] = val
	}

	// Apply key=value overrides.
	for key, val := range keyed {
		found := false
		for _, p := range workflow.Params {
			if p.Name == key {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown parameter: %q", key)
		}
		result[key] = val
	}

	// Check for required parameters (default to required if not specified).
	for _, p := range workflow.Params {
		required := p.Required == nil || *p.Required
		if required {
			if _, ok := result[p.Name]; !ok {
				return nil, fmt.Errorf("missing required parameter: %q", p.Name)
			}
		}
	}

	return result, nil
}
