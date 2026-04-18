package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/engine"
	_ "github.com/codagent/agent-runner/internal/engine/openspec"
	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/listview"
	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/runview"
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
	resumeFlag := flag.Bool("resume", false, "Resume an interrupted workflow (optionally followed by session ID)")
	listFlag := flag.Bool("list", false, "Launch the run list TUI")
	inspectFlag := flag.String("inspect", "", "Launch the run view TUI for a specific `run-id`")
	validateFlag := flag.Bool("validate", false, "Validate a workflow file without executing")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	vFlag := flag.Bool("v", false, "Print version and exit (shorthand)")
	// Undocumented: internal escape hatch for running without the TUI when
	// the live view is broken. Equivalent to AGENT_RUNNER_NO_TUI=1. Works
	// for both starting and resuming a workflow.
	headlessFlag := flag.Bool("headless", false, "")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: agent-runner [flags] [workflow [params...]]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fmt.Fprintf(os.Stderr, "  -C <dir>\n\tChange to directory before doing anything\n")
		fmt.Fprintf(os.Stderr, "  -inspect <run-id>\n\tLaunch the run view TUI for a specific run\n")
		fmt.Fprintf(os.Stderr, "  -list\n\tLaunch the run list TUI\n")
		fmt.Fprintf(os.Stderr, "  -resume [session-id]\n\tResume an interrupted workflow; launches TUI if no session ID given\n")
		fmt.Fprintf(os.Stderr, "  -validate\n\tValidate a workflow file without executing\n")
		fmt.Fprintf(os.Stderr, "  -v, -version\n\tPrint version and exit\n")
	}

	flag.Parse()

	if *headlessFlag {
		_ = os.Setenv("AGENT_RUNNER_NO_TUI", "1")
	}

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

	// Validate flag combinations.
	if *validateFlag && *resumeFlag {
		fmt.Fprintln(os.Stderr, "agent-runner: --validate and --resume are mutually exclusive")
		return 1
	}
	if *inspectFlag != "" && (*listFlag || *resumeFlag) {
		fmt.Fprintln(os.Stderr, "agent-runner: --inspect is mutually exclusive with --list and --resume")
		return 1
	}

	args := flag.Args()

	if *inspectFlag != "" {
		return handleInspect(*inspectFlag)
	}

	if *listFlag {
		return handleList()
	}

	if *resumeFlag {
		if len(args) > 1 {
			fmt.Fprintln(os.Stderr, "agent-runner: --resume accepts at most one argument (the session ID)")
			return 1
		}
		if len(args) == 1 {
			return handleResume(args[0])
		}
		return handleList()
	}

	if len(args) < 1 {
		return handleList()
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
	if err := requireTTY(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	stateFilePath, err := resolveResumeStatePath(sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	if os.Getenv("AGENT_RUNNER_NO_TUI") == "1" {
		result, runErr := runner.ResumeWorkflow(stateFilePath, &runner.Options{
			ProcessRunner: &realProcessRunner{},
			GlobExpander:  &realGlobExpander{},
			Log:           &realLogger{},
		})
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", runErr)
			return 1
		}
		if result != runner.ResultSuccess {
			return 1
		}
		return 0
	}

	h, err := runner.PrepareResume(stateFilePath, &runner.Options{
		ProcessRunner: &realProcessRunner{},
		GlobExpander:  &realGlobExpander{},
		Log:           &runner.DiscardLogger{},
	})
	if err != nil {
		if errors.Is(err, runner.ErrAlreadyCompleted) {
			fmt.Fprintln(os.Stderr, "agent-runner: workflow already completed")
			return 0
		}
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	return runLiveTUI(h)
}

func handleInspect(runID string) int {
	if err := requireTTY(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	sessionDir, projectDir, err := resolveInspectSession(runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	if runlock.CheckOwnedByOther(sessionDir, os.Getpid()) {
		fmt.Fprintf(os.Stderr, "agent-runner: run %q is active in another process\n", runID)
		return 1
	}

	rv, err := runview.New(sessionDir, projectDir, runview.FromInspect)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	sw := &switcher{runview: rv, mode: showingRunView}
	return runSwitcher(sw)
}

func handleList() int {
	if err := requireTTY(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	m, err := listview.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	sw := &switcher{list: m, mode: showingList}
	return runSwitcher(sw)
}

func runSwitcher(sw *switcher) int {
	p := tea.NewProgram(sw, tea.WithAltScreen(), tea.WithMouseCellMotion())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	final, ok := result.(*switcher)
	if !ok {
		return 0
	}
	if final.resumeSessionID != "" {
		return execAgentResume(final.resumeAgentCLI, final.resumeSessionID)
	}
	return 0
}

// runLiveTUI starts the runview TUI in FromLiveRun mode with the workflow
// running in a background goroutine. Returns the process exit code.
func runLiveTUI(h *runner.RunHandle) int {
	rv, err := runview.New(h.SessionDir, h.ProjectDir, runview.FromLiveRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	p := tea.NewProgram(rv, tea.WithAltScreen(), tea.WithMouseCellMotion())
	coord := liverun.NewCoordinator(p, h.SessionDir)

	resultCh := make(chan string, 1)
	go func() {
		result := string(runner.ResultFailed)
		var runErr error
		defer func() {
			if rec := recover(); rec != nil {
				coord.NotifyDone("failed", fmt.Errorf("panic: %v", rec))
				resultCh <- "failed"
				return
			}
			coord.NotifyDone(result, runErr)
			resultCh <- result
		}()

		res := runner.ExecuteFromHandle(h, &runner.Options{
			ProcessRunner: coord.TUIProcessRunner(&realProcessRunner{}),
			GlobExpander:  &realGlobExpander{},
			Log:           &runner.DiscardLogger{},
			SuspendHook:   coord.BeforeInteractive,
			ResumeHook:    coord.AfterInteractive,
		})
		result = string(res)
	}()

	_, err = p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	if runResult := <-resultCh; runResult != "success" {
		return 1
	}
	return 0
}

// allowedResumeCLIs bounds execAgentResume's `cli` argument. Resume metadata
// originates from audit logs and workflow YAML — both attacker-influenceable
// when inspecting runs from untrusted sources — and the value flows into
// syscall.Exec with the full environment. The allowlist mirrors
// internal/config.validCLI; keep them in sync when adding new agent CLIs.
var allowedResumeCLIs = map[string]bool{
	"claude": true,
	"codex":  true,
}

// execAgentResume replaces the current process with `<cli> --resume <session-id>`
// so the agent CLI inherits the terminal directly. This is the runview resume
// path: it resumes an individual agent conversation, NOT an agent-runner
// workflow run — despite both flags being spelled `--resume`, they live in
// different subsystems with different ID spaces (agent CLI session UUID vs.
// agent-runner run directory name).
func execAgentResume(cli, sessionID string) int {
	if cli == "" {
		cli = "claude"
	}
	if strings.ContainsAny(cli, `/\`) || !allowedResumeCLIs[cli] {
		fmt.Fprintf(os.Stderr, "agent-runner: refusing to resume: unsupported agent CLI %q\n", cli)
		return 1
	}
	path, err := exec.LookPath(cli)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: cannot find agent CLI %q in PATH: %v\n", cli, err)
		return 1
	}
	args := []string{cli, "--resume", sessionID}
	if err := syscall.Exec(path, args, os.Environ()); err != nil { // #nosec G204 -- cli validated against allowlist above
		fmt.Fprintf(os.Stderr, "agent-runner: exec %s --resume: %v\n", cli, err)
		return 1
	}
	return 0
}

// resolveInspectSession resolves a run ID to its session and project
// directories, using the same rules as --resume (cwd's project dir only).
func resolveInspectSession(runID string) (sessionDir, projectDir string, err error) {
	if strings.ContainsAny(runID, "/\\") || runID == ".." || strings.Contains(runID, "..") {
		return "", "", fmt.Errorf("invalid run ID: %s", runID)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("cannot determine working directory: %w", err)
	}

	encoded := audit.EncodePath(cwd)
	projectDir = filepath.Join(home, ".agent-runner", "projects", encoded)
	sessionDir = filepath.Join(projectDir, "runs", runID)

	if !strings.HasPrefix(filepath.Clean(sessionDir), filepath.Clean(projectDir)+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("invalid run ID: %s", runID)
	}
	if _, statErr := os.Stat(sessionDir); statErr != nil {
		return "", "", fmt.Errorf("session not found: %s", runID)
	}
	return sessionDir, projectDir, nil
}

// switcher is the top-level bubbletea Model that routes between the list
// and run-view sub-models.
type switcherMode int

const (
	showingList switcherMode = iota
	showingRunView
)

type switcher struct {
	list    *listview.Model
	runview *runview.Model
	mode    switcherMode

	termWidth  int
	termHeight int

	resumeAgentCLI  string
	resumeSessionID string
	viewErr         string
}

func (s *switcher) Init() tea.Cmd {
	switch s.mode {
	case showingRunView:
		return s.runview.Init()
	default:
		return s.list.Init()
	}
}

func (s *switcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Remember the last size so a newly-constructed sub-Model (runview
		// created on ViewRunMsg) can be sized immediately instead of waiting
		// for the next physical resize event.
		s.termWidth = msg.Width
		s.termHeight = msg.Height

	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return s, tea.Quit
		}

	case listview.ViewRunMsg:
		rv, err := runview.New(msg.SessionDir, msg.ProjectDir, runview.FromList)
		if err != nil {
			s.viewErr = fmt.Sprintf("cannot open run: %v", err)
			return s, nil
		}
		s.viewErr = ""
		s.runview = rv
		s.mode = showingRunView
		cmds := []tea.Cmd{rv.Init()}
		if s.termWidth > 0 && s.termHeight > 0 {
			w, h := s.termWidth, s.termHeight
			cmds = append(cmds, func() tea.Msg {
				return tea.WindowSizeMsg{Width: w, Height: h}
			})
		}
		return s, tea.Batch(cmds...)

	case runview.BackMsg:
		s.mode = showingList
		s.runview = nil
		return s, nil

	case runview.ResumeMsg:
		s.resumeAgentCLI = msg.AgentCLI
		s.resumeSessionID = msg.SessionID
		return s, tea.Quit

	case runview.ExitMsg:
		return s, tea.Quit
	}

	switch s.mode {
	case showingList:
		if s.list != nil {
			newModel, cmd := s.list.Update(msg)
			s.list = newModel.(*listview.Model)
			return s, cmd
		}
	case showingRunView:
		if s.runview != nil {
			newModel, cmd := s.runview.Update(msg)
			s.runview = newModel.(*runview.Model)
			return s, cmd
		}
	}
	return s, nil
}

func (s *switcher) View() string {
	switch s.mode {
	case showingRunView:
		if s.runview != nil {
			return s.runview.View()
		}
	default:
		if s.list != nil {
			v := s.list.View()
			if s.viewErr != "" {
				v += "\n  " + s.viewErr + "\n"
			}
			return v
		}
	}
	return ""
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

func handleValidate(workflowFile string) int {
	_, err := loader.LoadWorkflow(workflowFile, loader.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	fmt.Println("workflow is valid")
	return 0
}

// bareNamePattern matches valid workflow names. A name is either a bare
// identifier (e.g., "myworkflow") or a namespaced name (e.g., "openspec:plan-change")
// where the namespace corresponds to a subdirectory of workflows/.
var bareNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+(:[a-zA-Z0-9_-]+)?$`)

func resolveWorkflowArg(arg string) (string, error) {
	if !bareNamePattern.MatchString(arg) {
		return "", fmt.Errorf("invalid workflow name %q: use bare name (e.g., 'myworkflow' or 'namespace:myworkflow', not 'myworkflow.yaml'); workflows are resolved from workflows/ directory", arg)
	}
	base := filepath.Join("workflows", strings.ReplaceAll(arg, ":", string(os.PathSeparator)))
	yamlPath := base + ".yaml"
	if _, err := os.Stat(yamlPath); err == nil {
		return yamlPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat %s: %w", yamlPath, err)
	}
	ymlPath := base + ".yml"
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
	if err := requireTTY(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

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
		maps.Copy(engConfig, workflow.Engine.Extras)
		eng, err = engine.Create(engConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: create engine: %v\n", err)
			return 1
		}
	}

	if os.Getenv("AGENT_RUNNER_NO_TUI") == "1" {
		result, runErr := runner.RunWorkflow(&workflow, params, &runner.Options{
			WorkflowFile:  workflowFile,
			Engine:        eng,
			ProcessRunner: &realProcessRunner{},
			GlobExpander:  &realGlobExpander{},
			Log:           &realLogger{},
		})
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", runErr)
			return 1
		}
		if result != runner.ResultSuccess {
			return 1
		}
		return 0
	}

	h, err := runner.PrepareRun(&workflow, params, &runner.Options{
		WorkflowFile:  workflowFile,
		Engine:        eng,
		ProcessRunner: &realProcessRunner{},
		GlobExpander:  &realGlobExpander{},
		Log:           &runner.DiscardLogger{},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	return runLiveTUI(h)
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
