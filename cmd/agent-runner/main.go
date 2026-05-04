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
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/engine"
	_ "github.com/codagent/agent-runner/internal/engine/openspec"
	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/listview"
	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	nativesetup "github.com/codagent/agent-runner/internal/onboarding/native"
	"github.com/codagent/agent-runner/internal/paramform"
	"github.com/codagent/agent-runner/internal/prevalidate"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/runview"
	"github.com/codagent/agent-runner/internal/themeprompt"
	"github.com/codagent/agent-runner/internal/usersettings"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

var userHomeDir = os.UserHomeDir
var currentExecutable = os.Executable
var execProcess = syscall.Exec

type themeDeps struct {
	load   func() (usersettings.Settings, error)
	prompt func() (usersettings.Theme, bool, error)
	save   func(usersettings.Settings) error
	apply  func(usersettings.Theme)
}

var defaultThemeDeps = themeDeps{
	load:   usersettings.Load,
	prompt: themeprompt.Prompt,
	save:   usersettings.Save,
	apply:  applyTheme,
}

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

func (r *realProcessRunner) RunScript(path string, stdin []byte, captureStdout bool, workdir string) (iexec.ProcessResult, error) {
	c := exec.Command(path) // #nosec G204 -- workflow script path is validated by executor
	c.Stdin = bytes.NewReader(stdin)
	c.Env = append(os.Environ(), "AGENT_RUNNER_BUNDLE_DIR="+scriptBundleDir(path))
	if workdir != "" {
		c.Dir = filepath.Clean(workdir) // #nosec G304
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	if captureStdout {
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

func scriptBundleDir(scriptPath string) string {
	clean := filepath.Clean(scriptPath)
	sep := string(filepath.Separator)
	marker := sep + "bundled" + sep
	if idx := strings.Index(clean, marker); idx >= 0 {
		rest := clean[idx+len(marker):]
		if namespace, _, ok := strings.Cut(rest, sep); ok && namespace != "" {
			return clean[:idx+len(marker)+len(namespace)]
		}
	}
	return filepath.Dir(clean)
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
	if len(os.Args) > 1 && os.Args[1] == "internal" {
		return handleInternal(os.Args[2:])
	}

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

	if *validateFlag {
		return handleValidateArgs(args)
	}

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
		return handleListBare()
	}

	workflowFile, err := resolveWorkflowArg(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	return handleRun(append([]string{workflowFile}, args[1:]...))
}

func handleResume(sessionID string) int {
	return handleResumeWithOptions(sessionID, liveTUIOptions{})
}

func handleResumeWithOptions(sessionID string, liveOpts liveTUIOptions) int {
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

	if code := ensureThemeForTUI(defaultThemeDeps); code != 0 {
		return code
	}

	h, err := runner.PrepareResume(stateFilePath, &runner.Options{
		ProcessRunner: &realProcessRunner{},
		GlobExpander:  &realGlobExpander{},
		Log:           &runner.DiscardLogger{},
	})
	if err != nil {
		if errors.Is(err, runner.ErrAlreadyCompleted) {
			sessionDir, projectDir := resumeInspectPaths(stateFilePath)
			return openInspectTUI(sessionID, sessionDir, projectDir)
		}
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	return runLiveTUIWithOptions(h, liveOpts)
}

// resumeInspectPaths maps a resume state-file path to the session and project
// directories the run-view expects. The layout is
// `<projectDir>/runs/<run-id>/state.json`, so sessionDir is the state file's
// parent and projectDir is two levels above that.
func resumeInspectPaths(stateFilePath string) (sessionDir, projectDir string) {
	sessionDir = filepath.Dir(stateFilePath)
	projectDir = filepath.Dir(filepath.Dir(sessionDir))
	return
}

func handleInspect(runID string) int {
	sessionDir, projectDir, err := resolveInspectSession(runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	if err := requireTTY(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if code := ensureThemeForTUI(defaultThemeDeps); code != 0 {
		return code
	}

	return openInspectTUI(runID, sessionDir, projectDir)
}

// openInspectTUI launches the run-view TUI in FromInspect mode for a session
// that is not currently executing. Shared between --inspect and the
// "completed" branch of --resume, since both open a read-only view of a
// recorded run.
func openInspectTUI(runID, sessionDir, projectDir string) int {
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

// handleListBare opens the list TUI starting on the "new" tab (bare invocation).
func handleListBare() int {
	return handleListWithTab(listview.InitialTabNew)
}

// handleList opens the list TUI starting on the current-dir tab (--list / --resume no-arg).
func handleList() int {
	return handleListWithTab(listview.InitialTabCurrentDir)
}

func handleListWithTab(initialTab listview.InitialTab) int {
	if err := requireTTY(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if code := ensureThemeForTUI(defaultThemeDeps); code != 0 {
		return code
	}
	if code := ensureFirstRunForTUI(defaultFirstRunDeps); code != 0 {
		return code
	}

	m, err := listview.New(listview.WithInitialTab(initialTab))
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	sw := &switcher{list: m, mode: showingList}
	return runSwitcher(sw)
}

func runSwitcher(sw *switcher) int {
	for {
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
		if final.resumeRunID != "" {
			return execRunnerResume(final.resumeRunID, final.resumeRunProjectDir)
		}
		if final.startRunReady && final.startRunEntry != nil {
			return execStartRun(final.startRunEntry, final.startRunParams)
		}
		if final.resumeListProjectDir != "" {
			return execRunnerResume("", final.resumeListProjectDir)
		}
		if final.resumeSessionID == "" {
			return 0
		}

		spawnErr := spawnAgentResume(final.resumeAgentCLI, final.resumeSessionID)
		sw, err = switcherForReentry(final, spawnErr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			return 1
		}
	}
}

// switcherForReentry rebuilds a switcher around a fresh runview Model after a
// resumed agent CLI subprocess has exited. The previous list model and the
// runview's target (sessionDir/projectDir/entered) are preserved so esc still
// navigates back to the list where applicable.
func switcherForReentry(prev *switcher, spawnErr error) (*switcher, error) {
	if prev.runview == nil {
		return nil, fmt.Errorf("re-entry: no runview to rebuild")
	}
	rv, err := runview.NewForReentry(
		prev.runview.SessionDir(),
		prev.runview.ProjectDir(),
		prev.runview.Entered(),
		spawnErr,
	)
	if err != nil {
		return nil, err
	}
	return &switcher{
		list:       prev.list,
		runview:    rv,
		mode:       showingRunView,
		termWidth:  prev.termWidth,
		termHeight: prev.termHeight,
	}, nil
}

// runLiveTUI starts the runview TUI in FromLiveRun mode with the workflow
// running in a background goroutine. Returns the process exit code.
type liveTUIOptions struct {
	quitOnDone bool
}

func runLiveTUIWithOptions(h *runner.RunHandle, opts liveTUIOptions) int {
	rv, err := runview.New(h.SessionDir, h.ProjectDir, runview.FromLiveRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	p := tea.NewProgram(rv, tea.WithMouseCellMotion())
	coord := liverun.NewCoordinator(p, h.SessionDir)

	resultCh := make(chan runner.WorkflowResult, 1)
	go func() {
		result := runner.ResultFailed
		var runErr error
		defer func() {
			if rec := recover(); rec != nil {
				coord.NotifyDone(string(runner.ResultFailed), fmt.Errorf("panic: %v", rec))
				resultCh <- runner.ResultFailed
				return
			}
			coord.NotifyDone(string(result), runErr)
			resultCh <- result
			if opts.quitOnDone {
				p.Send(runview.ExitMsg{})
			}
		}()

		result = runner.ExecuteFromHandle(h, &runner.Options{
			ProcessRunner:   coord.TUIProcessRunner(&realProcessRunner{}),
			GlobExpander:    &realGlobExpander{},
			Log:             &runner.DiscardLogger{},
			SuspendHook:     coord.BeforeInteractive,
			ResumeHook:      coord.AfterInteractive,
			PrepareStepHook: coord.PrepareForStep,
			UIStepHandler:   coord.HandleUIStep,
		})
	}()

	rv, err = finalRunviewModel(p.Run())
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	// If the user did not request a resume, map the runner result to an exit
	// code. If the user confirmed quit while the workflow was still running,
	// resultCh has no value yet — keep the documented orphan-on-quit behavior
	// and return 0 without blocking on the lingering goroutine.
	if rv.ResumeSessionID() == "" {
		if rv.ResumeToList() {
			<-resultCh
			return execRunnerResume("", h.ProjectDir)
		}
		if opts.quitOnDone {
			select {
			case runResult := <-resultCh:
				if runResult != runner.ResultSuccess {
					return 1
				}
			default:
				// User quit before the dispatcher-launched workflow reached a
				// terminal state; preserve the normal live-run orphan behavior.
			}
			return 0
		}
		select {
		case runResult := <-resultCh:
			if runResult != runner.ResultSuccess {
				return 1
			}
		default:
		}
		return 0
	}

	// The user pressed enter on a completed agent step. Wait for the runner
	// goroutine so its run lock is released before handing the terminal to the
	// agent CLI, then enter the spawn-and-reenter loop.
	<-resultCh

	for rv.ResumeSessionID() != "" {
		spawnErr := spawnAgentResume(rv.ResumeAgentCLI(), rv.ResumeSessionID())
		rv, err = runview.NewForReentry(h.SessionDir, h.ProjectDir, runview.FromLiveRun, spawnErr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			return 1
		}
		p = tea.NewProgram(rv, tea.WithAltScreen(), tea.WithMouseCellMotion())
		rv, err = finalRunviewModel(p.Run())
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			return 1
		}
		if rv.ResumeToList() {
			return execRunnerResume("", h.ProjectDir)
		}
	}
	return 0
}

// finalRunviewModel extracts the terminal runview Model returned by tea.Program.Run.
// Capturing the returned model (rather than relying on the pointer originally
// passed in) keeps resume-state reads robust against future Update implementations
// that return a fresh instance instead of the same pointer.
func finalRunviewModel(final tea.Model, err error) (*runview.Model, error) {
	if err != nil {
		return nil, err
	}
	rv, ok := final.(*runview.Model)
	if !ok {
		return nil, fmt.Errorf("unexpected model type %T returned by tea.Program.Run", final)
	}
	return rv, nil
}

func execSelf(args ...string) int {
	self, err := currentExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: cannot resolve executable path: %v\n", err)
		return 1
	}
	execArgs := append([]string{filepath.Base(self)}, args...)
	if err := execProcess(self, execArgs, os.Environ()); err != nil { // #nosec G204 -- self is our own os.Executable() path; args are validated workflow names / run IDs
		fmt.Fprintf(os.Stderr, "agent-runner: exec %s: %v\n", strings.Join(args, " "), err)
		return 1
	}
	return 0
}

// allowedResumeCLIs bounds resume CLI arguments. Resume metadata originates
// from audit logs and workflow YAML — both attacker-influenceable when
// inspecting runs from untrusted sources — and the value flows into
// syscall.Exec / exec.Command with the full environment. The allowlist mirrors
// internal/config.validCLI; keep them in sync when adding new agent CLIs.
var allowedResumeCLIs = map[string]bool{
	"claude":  true,
	"codex":   true,
	"copilot": true,
	"cursor":  true,
}

// resolveResumeCLI validates `cli` against the resume allowlist and resolves
// it to an absolute path via PATH lookup. Callers must treat the returned path
// as safe to pass to syscall.Exec / exec.Command even though the surrounding
// arguments originate from audit logs.
func resolveResumeCLI(cli string) (resolvedCLI, path string, err error) {
	if cli == "" {
		cli = "claude"
	}
	if strings.ContainsAny(cli, `/\`) || !allowedResumeCLIs[cli] {
		return cli, "", fmt.Errorf("refusing to resume: unsupported agent CLI %q", cli)
	}
	path, err = exec.LookPath(cli)
	if err != nil {
		return cli, "", fmt.Errorf("cannot find agent CLI %q in PATH: %w", cli, err)
	}
	return cli, path, nil
}

// spawnAgentResume spawns `<cli> --resume <session-id>` as a subprocess and
// waits for it to exit. It does not replace the current process, so the
// caller can re-enter the run view after the CLI exits. A non-zero CLI exit
// code is not treated as an error — the user may have typed /exit or /quit.
// Only spawn failures (binary not found, permission error, etc.) are
// returned as errors.
func spawnAgentResume(cli, sessionID string) error {
	resolved, path, err := resolveResumeCLI(cli)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, "--resume", sessionID) // #nosec G204 -- cli validated by resolveResumeCLI
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn %s --resume: %w", resolved, err)
	}
	_ = cmd.Wait() // non-zero exit is normal (user typed /exit or /quit)
	return nil
}

// execRunnerResume replaces the current process with `agent-runner --resume
// <run-id>`, resuming an interrupted workflow run. Uses the current executable
// path so it works even when agent-runner is not in PATH. If projectDir is
// non-empty, the process chdirs there first so that resolveResumeStatePath
// looks in the correct project tree when the run belongs to a different project.
func execRunnerResume(runID, projectDir string) int {
	if projectDir != "" {
		if err := os.Chdir(projectDir); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: chdir %s: %v\n", projectDir, err)
			return 1
		}
	}
	args := []string{"--resume"}
	if runID != "" {
		args = append(args, runID)
	}
	return execSelf(args...)
}

// execStartRun replaces the current process with `agent-runner run <workflow>`
// using the workflow's canonical name and ordered key=value params.
func execStartRun(entry *discovery.WorkflowEntry, values map[string]string) int {
	if entry == nil || entry.CanonicalName == "" {
		fmt.Fprintln(os.Stderr, "agent-runner: cannot start run: missing workflow name")
		return 1
	}

	args := []string{entry.CanonicalName}
	seen := make(map[string]bool, len(entry.Params))
	for _, param := range entry.Params {
		value, ok := values[param.Name]
		if !ok {
			continue
		}
		args = append(args, param.Name+"="+value)
		seen[param.Name] = true
	}

	var extraKeys []string
	for key := range values {
		if !seen[key] {
			extraKeys = append(extraKeys, key)
		}
	}
	sort.Strings(extraKeys)
	for _, key := range extraKeys {
		args = append(args, key+"="+values[key])
	}

	return execSelf(args...)
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
	showingParamForm
)

type switcher struct {
	list       *listview.Model
	runview    *runview.Model
	paramform  *paramform.Model
	mode       switcherMode
	returnMode switcherMode

	termWidth  int
	termHeight int

	resumeAgentCLI       string
	resumeSessionID      string
	resumeRunID          string
	resumeRunProjectDir  string
	resumeListProjectDir string
	startRunEntry        *discovery.WorkflowEntry
	startRunParams       map[string]string
	startRunReady        bool
	viewErr              string
}

func (s *switcher) Init() tea.Cmd {
	switch s.mode {
	case showingRunView:
		return s.runview.Init()
	case showingParamForm:
		if s.paramform != nil {
			return s.paramform.Init()
		}
		return nil
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

	case discovery.ViewDefinitionMsg:
		rv, err := runview.NewForDefinition(&msg.Entry, "")
		if err != nil {
			s.viewErr = fmt.Sprintf("cannot open definition: %v", err)
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

	case discovery.StartRunMsg:
		entry := msg.Entry
		s.startRunEntry = &entry
		s.startRunParams = nil
		s.startRunReady = false
		if len(entry.Params) == 0 {
			s.startRunReady = true
			return s, tea.Quit
		}
		s.returnMode = s.mode
		s.paramform = paramform.New(&entry).WithWidth(s.termWidth)
		s.mode = showingParamForm
		return s, nil

	case paramform.SubmittedMsg:
		if s.startRunEntry == nil {
			return s, nil
		}
		s.startRunParams = map[string]string(msg)
		s.startRunReady = true
		return s, tea.Quit

	case paramform.CancelledMsg:
		s.startRunEntry = nil
		s.startRunParams = nil
		s.startRunReady = false
		s.paramform = nil
		s.mode = s.returnMode
		return s, nil

	case runview.BackMsg:
		s.mode = showingList
		s.runview = nil
		return s, nil

	case runview.ResumeMsg:
		s.resumeAgentCLI = msg.AgentCLI
		s.resumeSessionID = msg.SessionID
		return s, tea.Quit

	case runview.ResumeRunMsg:
		s.resumeRunID = msg.RunID
		return s, tea.Quit

	case runview.ResumeListMsg:
		if s.runview != nil {
			s.resumeListProjectDir = s.runview.ProjectDir()
		}
		return s, tea.Quit

	case listview.ResumeRunMsg:
		s.resumeRunID = msg.RunID
		s.resumeRunProjectDir = msg.ProjectDir
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
	case showingParamForm:
		if s.paramform != nil {
			newModel, cmd := s.paramform.Update(msg)
			s.paramform = newModel.(*paramform.Model)
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
	case showingParamForm:
		if s.paramform != nil {
			return s.paramform.View()
		}
		return ""
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

func handleValidateArgs(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "agent-runner: --validate requires a workflow name or YAML file path")
		return 1
	}
	workflowFile, err := resolveValidateWorkflowArg(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	positional, keyed, err := parseParams(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	if len(positional) > 0 {
		fmt.Fprintln(os.Stderr, "agent-runner: --validate parameters must use key=value syntax")
		return 1
	}
	result, err := prevalidate.Pipeline(workflowFile, keyed, prevalidate.Lenient, prevalidate.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	for i := range result.DeferredWarnings {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", result.DeferredWarnings[i])
	}
	fmt.Println("workflow is valid")
	return 0
}

func resolveValidateWorkflowArg(arg string) (string, error) {
	ext := strings.ToLower(filepath.Ext(arg))
	if (ext == ".yaml" || ext == ".yml") && fileExists(arg) {
		return arg, nil
	}
	return resolveWorkflowArg(arg)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

var workflowNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+(:[a-zA-Z0-9_-]+|(/[a-zA-Z0-9_-]+)+)?$`)

func resolveWorkflowArg(arg string) (string, error) {
	if !workflowNamePattern.MatchString(arg) {
		return "", fmt.Errorf("invalid workflow name %q: use a bare name or path under .agent-runner/workflows/ (e.g., 'myworkflow' or 'team/deploy') or a builtin name like 'core:finalize-pr'", arg)
	}
	if strings.Contains(arg, ":") {
		return builtinworkflows.Resolve(arg)
	}
	localBase := filepath.Join(".agent-runner", "workflows", filepath.FromSlash(arg))
	localPaths := []string{localBase + ".yaml", localBase + ".yml"}
	if resolved, err := resolveWorkflowFile(localPaths...); err != nil {
		return "", err
	} else if resolved != "" {
		return resolved, nil
	}

	globalPaths := []string{}
	var homeErr error
	if home, err := userHomeDir(); err == nil {
		globalBase := filepath.Join(home, ".agent-runner", "workflows", filepath.FromSlash(arg))
		globalPaths = []string{globalBase + ".yaml", globalBase + ".yml"}
		if resolved, err := resolveWorkflowFile(globalPaths...); err != nil {
			return "", err
		} else if resolved != "" {
			return resolved, nil
		}
	} else {
		homeErr = err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("workflow %q not found (%s); failed to get cwd: %w", arg, triedWorkflowPaths(localPaths, globalPaths), err)
	}
	if homeErr != nil {
		return "", fmt.Errorf("workflow %q not found in %s (%s; home dir lookup failed: %v)", arg, cwd, triedWorkflowPaths(localPaths, globalPaths), homeErr)
	}
	return "", fmt.Errorf("workflow %q not found in %s (%s)", arg, cwd, triedWorkflowPaths(localPaths, globalPaths))
}

func resolveWorkflowFile(paths ...string) (string, error) {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat %s: %w", path, err)
		}
	}
	return "", nil
}

func triedWorkflowPaths(groups ...[]string) string {
	var paths []string
	for _, group := range groups {
		paths = append(paths, group...)
	}
	return "tried " + strings.Join(paths, ", ")
}

func handleRun(args []string) int {
	return handleRunWithOptions(args, liveTUIOptions{})
}

func handleRunWithOptions(args []string, liveOpts liveTUIOptions) int {
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

	if !builtinworkflows.IsRef(workflowFile) {
		if _, err := prevalidate.Pipeline(workflowFile, params, prevalidate.Strict, prevalidate.Options{}); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			return 1
		}
	}

	if err := requireTTY(); err != nil {
		fmt.Fprintln(os.Stderr, err)
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

	if code := ensureThemeForTUI(defaultThemeDeps); code != 0 {
		return code
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

	return runLiveTUIWithOptions(h, liveOpts)
}

func ensureThemeForTUI(deps themeDeps) int {
	settings, err := deps.load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	if settings.Theme == "" {
		theme, ok, err := deps.prompt()
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			return 1
		}
		if !ok {
			return 1
		}
		settings.Theme = theme
		if err := deps.save(settings); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: failed to save settings: %v\n", err)
			return 1
		}
	}

	deps.apply(settings.Theme)
	return 0
}

func applyTheme(theme usersettings.Theme) {
	lipgloss.SetHasDarkBackground(theme == usersettings.ThemeDark)
}

type nativeSetupResult int

const (
	nativeSetupCompleted nativeSetupResult = iota
	nativeSetupCancelled
	nativeSetupFailed
)

type firstRunDeps struct {
	load           func() (usersettings.Settings, error)
	isStdinTTY     func() bool
	isStdoutTTY    func() bool
	runNativeSetup func() (nativeSetupResult, error)
	runWorkflow    func(ref string) int
}

var defaultFirstRunDeps = firstRunDeps{
	load:        usersettings.Load,
	isStdinTTY:  func() bool { return isatty.IsTerminal(os.Stdin.Fd()) },
	isStdoutTTY: func() bool { return isatty.IsTerminal(os.Stdout.Fd()) },
	runNativeSetup: func() (nativeSetupResult, error) {
		result, err := nativesetup.Run(nativesetup.Deps{})
		switch result {
		case nativesetup.ResultCompleted:
			return nativeSetupCompleted, err
		case nativesetup.ResultFailed:
			return nativeSetupFailed, err
		default:
			return nativeSetupCancelled, err
		}
	},
	runWorkflow: func(ref string) int {
		return handleRunWithOptions([]string{ref}, liveTUIOptions{quitOnDone: true})
	},
}

func ensureFirstRunForTUI(deps firstRunDeps) int {
	settings, err := deps.load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	if !deps.isStdinTTY() || !deps.isStdoutTTY() {
		return 0
	}
	if settings.Setup.CompletedAt == "" {
		result, err := deps.runNativeSetup()
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			return 0
		}
		if result != nativeSetupCompleted {
			return 0
		}
		settings.Setup.CompletedAt = "completed"
	}
	if settings.Setup.CompletedAt == "" || settings.Onboarding.CompletedAt != "" || settings.Onboarding.Dismissed != "" {
		return 0
	}
	ref, err := builtinworkflows.Resolve("onboarding:onboarding")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	return deps.runWorkflow(ref)
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
