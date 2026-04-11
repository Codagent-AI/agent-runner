package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/engine"
	_ "github.com/codagent/agent-runner/internal/engine/openspec"
	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// realProcessRunner implements exec.ProcessRunner using os/exec.
type realProcessRunner struct{}

func (r *realProcessRunner) RunShell(cmd string, captureStdout bool, workdir string) (iexec.ProcessResult, error) {
	c := exec.Command("sh", "-c", cmd) // #nosec G204 -- CLI runner executes user-defined shell commands by design
	c.Stdin = os.Stdin
	if workdir != "" {
		c.Dir = filepath.Clean(workdir) // #nosec G304 -- workdir is from user-authored workflow YAML
	}

	if captureStdout {
		var stderrBuf bytes.Buffer
		c.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
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
			Stderr:   strings.TrimSpace(stderrBuf.String()),
		}, nil
	}

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

func (r *realProcessRunner) RunAgent(args []string, captureStdout bool, workdir string) (iexec.ProcessResult, error) {
	c := exec.Command(args[0], args[1:]...) // #nosec G204 -- CLI runner launches agent processes by design
	c.Stdin = os.Stdin
	c.Stderr = os.Stderr
	if workdir != "" {
		c.Dir = filepath.Clean(workdir) // #nosec G304 -- workdir is from user-authored workflow YAML
	}

	if captureStdout {
		var stdoutBuf, stderrBuf bytes.Buffer
		c.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
		c.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
		err := c.Run()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				return iexec.ProcessResult{}, err
			}
		}
		return iexec.ProcessResult{ExitCode: exitCode, Stdout: stdoutBuf.String(), Stderr: stderrBuf.String()}, nil
	}

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
	chdirFlag := flag.String("C", "", "Change to `directory` before doing anything")
	resumeFlag := flag.Bool("resume", false, "Resume an interrupted workflow")
	sessionFlag := flag.String("session", "", "Session ID to resume (implies --resume)")
	validateFlag := flag.Bool("validate", false, "Validate a workflow file without executing")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	vFlag := flag.Bool("v", false, "Print version and exit (shorthand)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: agent-runner [flags] <workflow.yaml> [params...]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fmt.Fprintf(os.Stderr, "  -C <dir>\n\tChange to directory before doing anything\n")
		fmt.Fprintf(os.Stderr, "  -resume\n\tResume an interrupted workflow\n")
		fmt.Fprintf(os.Stderr, "  -session <id>\n\tSession ID to resume (implies -resume)\n")
		fmt.Fprintf(os.Stderr, "  -validate\n\tValidate a workflow file without executing\n")
		fmt.Fprintf(os.Stderr, "  -v, -version\n\tPrint version and exit\n")
	}

	flag.Parse()

	if *chdirFlag != "" {
		if err := os.Chdir(*chdirFlag); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: -C %s: %v\n", *chdirFlag, err)
			return 1
		}
	}

	if *versionFlag || *vFlag {
		fmt.Println(version)
		return 0
	}

	// Infer --resume when --session is provided.
	if *sessionFlag != "" {
		*resumeFlag = true
	}

	// Validate flag combinations.
	if *validateFlag && *resumeFlag {
		fmt.Fprintln(os.Stderr, "agent-runner: --validate and --resume are mutually exclusive")
		return 1
	}

	args := flag.Args()

	if *resumeFlag {
		if len(args) > 0 {
			fmt.Fprintln(os.Stderr, "agent-runner: --resume does not accept positional arguments")
			return 1
		}
		return handleResume(*sessionFlag)
	}

	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "agent-runner: workflow file required")
		flag.Usage()
		return 1
	}

	workflowFile, err := resolveWorkflowArg(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	if *validateFlag {
		return handleValidate(workflowFile)
	}

	return handleRun(append([]string{workflowFile}, args[1:]...))
}

func handleResume(sessionID string) int {
	stateFilePath, err := resolveResumeStatePath(sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	result, err := runner.ResumeWorkflow(stateFilePath, &runner.Options{
		ProcessRunner: &realProcessRunner{},
		GlobExpander:  &realGlobExpander{},
		Log:           &realLogger{},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	if result != runner.ResultSuccess {
		return 1
	}
	return 0
}

func resolveResumeStatePath(sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}

	encoded := audit.EncodePath(cwd)
	runsDir := filepath.Join(home, ".agent-runner", "projects", encoded, "runs")

	if sessionID != "" {
		if strings.ContainsAny(sessionID, "/\\") || sessionID == ".." || strings.Contains(sessionID, "..") {
			return "", fmt.Errorf("invalid session ID: %s", sessionID)
		}
		stateFile := filepath.Join(runsDir, sessionID, "state.json")
		if !strings.HasPrefix(filepath.Clean(stateFile), filepath.Clean(runsDir)+string(os.PathSeparator)) {
			return "", fmt.Errorf("invalid session ID: %s", sessionID)
		}
		if _, err := os.Stat(stateFile); err != nil {
			return "", fmt.Errorf("session not found: %s", sessionID)
		}
		return stateFile, nil
	}

	// Find most recent session by state.json modification time.
	entries, err := os.ReadDir(runsDir)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no previous sessions found")
	}
	if err != nil {
		return "", fmt.Errorf("reading sessions directory: %w", err)
	}

	var bestPath string
	var bestTime time.Time

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		stateFile := filepath.Join(runsDir, entry.Name(), "state.json")
		info, err := os.Stat(stateFile)
		if err != nil {
			continue
		}
		if bestPath == "" || info.ModTime().After(bestTime) {
			bestPath = stateFile
			bestTime = info.ModTime()
		}
	}

	if bestPath == "" {
		return "", fmt.Errorf("no previous sessions found")
	}

	return bestPath, nil
}

func handleValidate(workflowFile string) int {
	_, err := loader.LoadWorkflow(workflowFile, loader.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	fmt.Println("workflow is valid")
	return 0
}

// bareNamePattern matches valid bare workflow names (no paths or extensions).
var bareNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func resolveWorkflowArg(arg string) (string, error) {
	if !bareNamePattern.MatchString(arg) {
		return "", fmt.Errorf("invalid workflow name %q: use bare name (e.g., 'myworkflow' not 'myworkflow.yaml'); workflows are resolved from workflows/ directory", arg)
	}
	yamlPath := filepath.Join("workflows", arg+".yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		return yamlPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat %s: %w", yamlPath, err)
	}
	ymlPath := filepath.Join("workflows", arg+".yml")
	if _, err := os.Stat(ymlPath); err == nil {
		return ymlPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat %s: %w", ymlPath, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("workflow %q not found (tried %s and %s); failed to get cwd: %w", arg, yamlPath, ymlPath, err)
	}
	return "", fmt.Errorf("workflow %q not found in %s (tried %s and %s)", arg, cwd, yamlPath, ymlPath)
}

func handleRun(args []string) int {
	workflowFile := args[0]

	workflow, err := loader.LoadWorkflow(workflowFile, loader.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: load workflow: %v\n", err)
		return 1
	}

	positional, keyed, err := parseParams(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	params, err := matchParams(&workflow, positional, keyed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	var eng engine.Engine
	if workflow.Engine != nil {
		engConfig := map[string]any{"type": workflow.Engine.Type}
		for k, v := range workflow.Engine.Extras {
			engConfig[k] = v
		}
		eng, err = engine.Create(engConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: create engine: %v\n", err)
			return 1
		}
	}

	result, err := runner.RunWorkflow(&workflow, params, &runner.Options{
		WorkflowFile:  workflowFile,
		Engine:        eng,
		ProcessRunner: &realProcessRunner{},
		GlobExpander:  &realGlobExpander{},
		Log:           &realLogger{},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	if result != runner.ResultSuccess {
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
