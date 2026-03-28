package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
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
	rootCmd := &cobra.Command{
		Use:   "agent-runner",
		Short: "CLI workflow orchestrator for AI agents",
	}

	runCmd := &cobra.Command{
		Use:   "run <workflow.yaml> [param=value ...]",
		Short: "Execute a workflow",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflowFile := args[0]
			params := parseParams(args[1:])

			workflow, err := loader.LoadWorkflow(workflowFile, loader.Options{})
			if err != nil {
				return fmt.Errorf("load workflow: %w", err)
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
				os.Exit(1)
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
				os.Exit(1)
			}
			return nil
		},
	}

	rootCmd.AddCommand(runCmd, validateCmd, resumeCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		os.Exit(1)
	}
}

func parseParams(args []string) map[string]string {
	params := make(map[string]string)
	for _, arg := range args {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) == 2 {
			params[parts[0]] = parts[1]
		}
	}
	return params
}
