package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	"github.com/muesli/termenv"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/engine"
	_ "github.com/codagent/agent-runner/internal/engine/openspec"
	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/intakeroute"
	"github.com/codagent/agent-runner/internal/listview"
	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	nativesetup "github.com/codagent/agent-runner/internal/onboarding/native"
	"github.com/codagent/agent-runner/internal/onboarding/splash"
	"github.com/codagent/agent-runner/internal/paramform"
	"github.com/codagent/agent-runner/internal/prevalidate"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/runview"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/codagent/agent-runner/internal/themeprompt"
	"github.com/codagent/agent-runner/internal/tuistyle"
	"github.com/codagent/agent-runner/internal/usersettings"
	"github.com/codagent/agent-runner/internal/workflowcatalog"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

var userHomeDir = os.UserHomeDir
var currentExecutable = os.Executable
var execProcess = syscall.Exec

const liveRunImmediateAltScreenEnv = "AGENT_RUNNER_LIVE_RUN_IMMEDIATE_ALT_SCREEN"
const agentRunnerExecutableEnv = "AGENT_RUNNER_EXECUTABLE"

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

func agentRunnerCommandEnv() []string {
	env := os.Environ()
	self, err := currentExecutable()
	if err != nil || self == "" {
		return env
	}
	return append(env, agentRunnerExecutableEnv+"="+self)
}

func ensureAgentRunnerExecutableEnv() {
	if self, err := currentExecutable(); err == nil && self != "" {
		_ = os.Setenv(agentRunnerExecutableEnv, self)
		return
	}
	if os.Getenv(agentRunnerExecutableEnv) != "" {
		return
	}
	if len(os.Args) == 0 {
		return
	}
	if self, err := exec.LookPath(os.Args[0]); err == nil && self != "" {
		_ = os.Setenv(agentRunnerExecutableEnv, self)
	}
}

func (r *realProcessRunner) RunShell(cmd string, captureStdout bool, workdir string) (iexec.ProcessResult, error) {
	c := exec.Command("sh", "-c", cmd) // #nosec G204 -- CLI runner executes user-defined shell commands by design
	c.Stdin = os.Stdin
	c.Env = agentRunnerCommandEnv()
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

func (r *realProcessRunner) RunAgent(options *iexec.AgentProcessOptions) (iexec.ProcessResult, error) {
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	c := exec.CommandContext(ctx, options.Args[0], options.Args[1:]...) // #nosec G204 -- CLI runner launches agent processes by design
	iexec.ConfigureAgentCommand(c, options.Supervision)
	c.Stderr = os.Stderr
	c.Env = iexec.BuildAgentEnvironment(agentRunnerCommandEnv(), options.DropEnv, options.Env)
	if options.Workdir != "" {
		c.Dir = filepath.Clean(options.Workdir) // #nosec G304 -- workdir is from user-authored workflow YAML
	}

	if options.CaptureStdout {
		var stdoutBuf, stderrBuf bytes.Buffer
		stdout, closeStdout := wrapAgentWriter(os.Stdout, options.StdoutWrapper)
		stderr, closeStderr := wrapAgentWriter(os.Stderr, options.StderrWrapper)
		defer closeStdout()
		defer closeStderr()
		c.Stdout = io.MultiWriter(stdout, &stdoutBuf)
		c.Stderr = io.MultiWriter(stderr, &stderrBuf)
		if err := c.Start(); err != nil {
			return iexec.ProcessResult{}, err
		}
		err := c.Wait()
		result := iexec.ProcessResult{Started: true, ExitCode: -1, Stdout: stdoutBuf.String(), Stderr: stderrBuf.String()}
		exitCode := 0
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return result, err
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, ctxErr
			}
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				return result, err
			}
		}
		return iexec.ProcessResult{Started: true, ExitCode: exitCode, Stdout: stdoutBuf.String(), Stderr: stderrBuf.String()}, nil
	}

	stdout, closeStdout := wrapAgentWriter(os.Stdout, options.StdoutWrapper)
	stderr, closeStderr := wrapAgentWriter(os.Stderr, options.StderrWrapper)
	defer closeStdout()
	defer closeStderr()
	c.Stdout = stdout
	c.Stderr = stderr
	if err := c.Start(); err != nil {
		return iexec.ProcessResult{}, err
	}
	err := c.Wait()
	result := iexec.ProcessResult{Started: true, ExitCode: -1}
	exitCode := 0
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return result, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return result, err
		}
	}
	return iexec.ProcessResult{Started: true, ExitCode: exitCode}, nil
}

func wrapAgentWriter(writer io.Writer, wrapper func(io.Writer) io.Writer) (wrappedWriter io.Writer, cleanup func()) {
	if wrapper == nil {
		return writer, func() {}
	}
	wrapped := wrapper(writer)
	return wrapped, func() {
		if closer, ok := wrapped.(io.Closer); ok {
			_ = closer.Close()
		}
	}
}

func (r *realProcessRunner) RunScript(path string, stdin []byte, captureStdout bool, workdir string) (iexec.ProcessResult, error) {
	c := exec.Command(path) // #nosec G204 -- workflow script path is validated by executor
	c.Stdin = bytes.NewReader(stdin)
	c.Env = append(agentRunnerCommandEnv(), "AGENT_RUNNER_BUNDLE_DIR="+scriptBundleDir(path))
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
	ensureAgentRunnerExecutableEnv()

	if len(os.Args) > 1 && os.Args[1] == "step" {
		return handleStep(os.Args[2:])
	}
	if len(os.Args) > 1 && os.Args[1] == "internal" {
		return handleInternal(os.Args[2:])
	}

	chdirFlag := flag.String("C", "", "Change to `directory` before doing anything")
	resumeFlag := flag.Bool("resume", false, "Resume an interrupted workflow (optionally followed by session ID)")
	listFlag := flag.Bool("list", false, "Launch the run list TUI")
	inspectFlag := flag.String("inspect", "", "Launch the run view TUI for a specific `run-id`")
	validateFlag := flag.Bool("validate", false, "Validate a workflow file without executing")
	resetOnboardingFlag := flag.Bool("reset-onboarding", false, "Clear onboarding settings, project validator state, and saved onboarding runs before launching")
	onboardingFromFlag := flag.String("onboarding-from", "", "Start the built-in onboarding workflow from top-level `step-id`")
	intakeFlag := flag.Bool("i", false, "Start the built-in intake workflow")
	cliFlag := flag.String("cli", "", "CLI override for intake")
	modelFlag := flag.String("model", "", "Model override for intake")
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
		fmt.Fprintf(os.Stderr, "  -reset-onboarding\n\tClear onboarding settings, project .validator/, and saved onboarding runs before launching\n")
		fmt.Fprintf(os.Stderr, "  -onboarding-from <step-id>\n\tStart the built-in onboarding workflow from a top-level step\n")
		fmt.Fprintf(os.Stderr, "  --profile <name>\n\tSelect the profile set for this invocation\n")
		fmt.Fprintf(os.Stderr, "  -i\n\tStart the built-in intake workflow\n")
		fmt.Fprintf(os.Stderr, "  -cli <name>\n\tCLI override for intake\n")
		fmt.Fprintf(os.Stderr, "  -model <name>\n\tModel override for intake\n")
		fmt.Fprintf(os.Stderr, "  -validate\n\tValidate a workflow file without executing\n")
		fmt.Fprintf(os.Stderr, "  -v, -version\n\tPrint version and exit\n")
	}

	// --profile is extracted from the full argv before flag parsing so a single
	// parser owns every occurrence. The standard flag package would otherwise
	// consume (and silently merge) occurrences appearing before the first
	// positional argument, leaving extractProfileArgs unable to see them.
	profileArgs, profileValue, profileSet, profileErr := extractProfileArgs(os.Args[1:])
	if profileErr != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", profileErr)
		return 1
	}

	_ = flag.CommandLine.Parse(profileArgs)

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

	args := flag.Args()
	if handled, code := routeDebugCommand(args, os.Stdout, os.Stderr); handled {
		return code
	}

	// Validate flag combinations.
	if *validateFlag && *resumeFlag {
		fmt.Fprintln(os.Stderr, "agent-runner: --validate and --resume are mutually exclusive")
		return 1
	}
	if *resetOnboardingFlag && (*validateFlag || *resumeFlag || *inspectFlag != "") {
		fmt.Fprintln(os.Stderr, "agent-runner: --reset-onboarding is mutually exclusive with --validate, --resume, and --inspect")
		return 1
	}
	if *onboardingFromFlag != "" && (*validateFlag || *resumeFlag || *inspectFlag != "") {
		fmt.Fprintln(os.Stderr, "agent-runner: --onboarding-from is mutually exclusive with --validate, --resume, and --inspect")
		return 1
	}
	if *inspectFlag != "" && (*listFlag || *resumeFlag) {
		fmt.Fprintln(os.Stderr, "agent-runner: --inspect is mutually exclusive with --list and --resume")
		return 1
	}
	if profileSet && *listFlag {
		fmt.Fprintln(os.Stderr, "agent-runner: --profile and --list are mutually exclusive")
		return 1
	}
	if profileSet && *inspectFlag != "" {
		fmt.Fprintln(os.Stderr, "agent-runner: --profile and --inspect are mutually exclusive")
		return 1
	}
	if err := validateIntakeInvocation(&intakeInvocationOptions{
		intake: *intakeFlag, headless: *headlessFlag, list: *listFlag, resume: *resumeFlag,
		inspect: *inspectFlag, validate: *validateFlag, onboardingFrom: strings.TrimSpace(*onboardingFromFlag),
		args: args, cli: strings.TrimSpace(*cliFlag), model: strings.TrimSpace(*modelFlag),
		profileOverride: (&commandFlags{profile: profileValue, profileSet: profileSet}).profileOverride(),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	if *resetOnboardingFlag {
		if err := resetOnboardingState(); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: reset onboarding: %v\n", err)
			return 1
		}
	}

	return dispatchRunCommand(args, &commandFlags{
		validate:       *validateFlag,
		inspect:        *inspectFlag,
		list:           *listFlag,
		resume:         *resumeFlag,
		profile:        profileValue,
		profileSet:     profileSet,
		onboardingFrom: strings.TrimSpace(*onboardingFromFlag),
		intake:         *intakeFlag,
		headless:       *headlessFlag,
		agentOverride: &model.AgentOverride{
			CLI: strings.TrimSpace(*cliFlag), Model: strings.TrimSpace(*modelFlag),
		},
	})
}

// extractProfileArgs pulls every --profile occurrence out of the given argv,
// in both the space-separated and `=` forms and with either dash prefix, and
// returns the remaining arguments for the standard flag parser. It is the sole
// owner of --profile parsing, so a duplicate is rejected regardless of where
// the occurrences sit relative to positional arguments.
func extractProfileArgs(args []string) (filtered []string, profile string, set bool, err error) {
	filtered = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := ""
		hasProfile := false
		switch {
		case arg == "--profile" || arg == "-profile":
			hasProfile = true
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, "", false, fmt.Errorf("--profile requires a profile set name")
			}
			i++
			value = args[i]
		case strings.HasPrefix(arg, "--profile="):
			hasProfile = true
			value = strings.TrimPrefix(arg, "--profile=")
		case strings.HasPrefix(arg, "-profile="):
			hasProfile = true
			value = strings.TrimPrefix(arg, "-profile=")
		}
		if !hasProfile {
			filtered = append(filtered, arg)
			continue
		}
		if set {
			return nil, "", false, fmt.Errorf("--profile may only be specified once")
		}
		profile = strings.TrimSpace(value)
		if profile == "" {
			return nil, "", false, fmt.Errorf("--profile requires a profile set name")
		}
		set = true
	}
	return filtered, profile, set, nil
}

type commandFlags struct {
	validate       bool
	inspect        string
	list           bool
	resume         bool
	profile        string
	profileSet     bool
	onboardingFrom string
	intake         bool
	headless       bool
	agentOverride  *model.AgentOverride
}

type intakeInvocationOptions struct {
	intake          bool
	headless        bool
	list            bool
	resume          bool
	inspect         string
	validate        bool
	onboardingFrom  string
	args            []string
	cli             string
	model           string
	profileOverride config.ProfileOverride
}

func validateIntakeInvocation(opts *intakeInvocationOptions) error {
	if (opts.cli != "" || opts.model != "") && !opts.intake {
		return fmt.Errorf("--cli and --model require -i")
	}
	if !opts.intake {
		return nil
	}
	if opts.headless || opts.list || opts.resume || opts.inspect != "" || opts.validate || opts.onboardingFrom != "" {
		return fmt.Errorf("-i is mutually exclusive with --headless, --list, --resume, --inspect, --validate, and --onboarding-from")
	}
	if len(opts.args) > 0 {
		return fmt.Errorf("-i cannot be combined with an explicit workflow")
	}
	if opts.cli == "" {
		return nil
	}
	adapter, err := cli.Get(opts.cli)
	if err != nil {
		known := cli.KnownCLIs()
		sort.Strings(known)
		return fmt.Errorf("invalid --cli %q; accepted values: %s", opts.cli, strings.Join(known, ", "))
	}
	if rejector, ok := adapter.(cli.InteractiveRejector); ok {
		if err := rejector.InteractiveModeError(); err != nil {
			return fmt.Errorf("--cli %q cannot be used for intake: intake requires an interactive-capable CLI: %w", opts.cli, err)
		}
	}
	return validateIntakeOverrideModel(adapter, opts.cli, opts.model, opts.profileOverride)
}

// intakeAgentProfile is the agent profile core:intake declares for its session.
const intakeAgentProfile = "lead"

// validateIntakeOverrideModel probes the effective model when --cli is given
// without --model.
//
// Fresh builtin runs deliberately skip pre-validation because builtins are
// validated at the agent-runner repo's build time. That justification does not
// extend to a runtime override: pairing a new CLI with a model name inherited
// from the intake agent's profile produces a (cli, model) pair that existed
// nowhere at build time. Without this check the mismatch surfaces only as an
// opaque provider-side error partway into the conversation — for example Codex
// rejecting the Claude-specific name "opus" with a 400.
func validateIntakeOverrideModel(adapter cli.Adapter, cliName, modelOverride string, profileOverride config.ProfileOverride) error {
	if modelOverride != "" {
		// An explicit --model is the user's own choice for this adapter and is
		// probed by the normal step path; nothing is inherited across providers.
		return nil
	}
	inherited, err := intakeProfileModel(profileOverride)
	if err != nil || inherited == "" {
		// No resolvable profile model means nothing is inherited, so there is
		// no cross-provider mismatch to report here.
		return nil
	}
	if _, probeErr := adapter.ProbeModel(inherited, ""); probeErr != nil {
		return fmt.Errorf(
			"model %q comes from the intake agent's profile and is not valid for --cli %q; pass --model with --cli: %w",
			inherited, cliName, probeErr,
		)
	}
	return nil
}

// intakeProfileModel resolves the model the intake agent would inherit from its
// configured profile. A missing or unreadable configuration is not an error
// here: it simply means there is no inherited model to validate.
func intakeProfileModel(profileOverride config.ProfileOverride) (string, error) {
	cfg, err := config.LoadWithProfile(filepath.Join(".agent-runner", "config.yaml"), profileOverride)
	if err != nil {
		return "", err
	}
	resolved, err := cfg.Resolve(intakeAgentProfile)
	if err != nil {
		return "", err
	}
	return resolved.Model, nil
}

func dispatchRunCommand(args []string, opts *commandFlags) int {
	if isRunCommandHelp(args) {
		printRunUsage(os.Stderr)
		return 0
	}

	var err error
	var runOpts runCommandOptions
	args, runOpts, err = parseRunCommandArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	runOpts.headless = opts.headless

	if opts.validate {
		return handleValidateArgs(args, opts.profileOverride())
	}
	if opts.intake {
		workflowFile, err := builtinworkflows.Resolve(builtinworkflows.IntakeCanonicalName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			return 1
		}
		return handleRunWithRunOptions([]string{workflowFile}, &runCommandOptions{
			headless: opts.headless, agentOverride: opts.agentOverride, profileOverride: opts.profileOverride(),
		}).exitCode
	}

	if opts.inspect != "" {
		return handleInspect(opts.inspect)
	}

	if opts.list {
		return handleListWithDeps(listview.InitialTabCurrentDir, firstRunDepsWithOnboardingFrom(opts.onboardingFrom))
	}

	if opts.resume {
		if len(args) > 1 {
			fmt.Fprintln(os.Stderr, "agent-runner: --resume accepts at most one argument (the session ID)")
			return 1
		}
		if len(args) == 1 {
			return handleResume(args[0], opts.profileOverride())
		}
		return handleListWithProfile(opts.profileOverride())
	}

	if len(args) < 1 {
		if opts.onboardingFrom != "" {
			ref, err := builtinworkflows.Resolve("onboarding:onboarding")
			if err != nil {
				fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
				return 1
			}
			return handleOnboardingFromRun(ref, opts.onboardingFrom, &runCommandOptions{profileOverride: opts.profileOverride()})
		}
		return handleListWithDeps(listview.InitialTabNew, defaultFirstRunDeps)
	}

	workflowFile, err := resolveWorkflowArg(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	if opts.onboardingFrom != "" && !isTopLevelOnboardingWorkflow(workflowFile) {
		fmt.Fprintln(os.Stderr, "agent-runner: --onboarding-from can only be used with onboarding:onboarding")
		return 1
	}

	if opts.onboardingFrom != "" {
		if runOpts.until != "" {
			fmt.Fprintln(os.Stderr, "agent-runner: --until cannot be combined with --onboarding-from")
			return 1
		}
		runOpts.profileOverride = opts.profileOverride()
		return handleOnboardingFromRun(workflowFile, opts.onboardingFrom, &runOpts, args[1:]...)
	}
	runOpts.profileOverride = opts.profileOverride()
	return handleRunWithRunOptions(append([]string{workflowFile}, args[1:]...), &runOpts).exitCode
}

func (opts *commandFlags) profileOverride() config.ProfileOverride {
	if !opts.profileSet {
		return config.ProfileOverride{}
	}
	return config.ProfileOverride{Name: opts.profile, Origin: config.OriginFlag}
}

func isRunCommandHelp(args []string) bool {
	return len(args) == 2 && args[0] == "run" && (args[1] == "--help" || args[1] == "-h")
}

func printRunUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: agent-runner run <workflow> [--until <step-id>] [--param key=value] [key=value ...]")
	_, _ = fmt.Fprintln(w, "\nFlags:")
	_, _ = fmt.Fprintln(w, "  --until <step-id>\n\tStop successfully after reaching the named top-level step")
}

func normalizeRunCommandArgs(args []string) ([]string, error) {
	normalized, _, err := parseRunCommandArgs(args)
	return normalized, err
}

func parseRunCommandArgs(args []string) ([]string, runCommandOptions, error) {
	var opts runCommandOptions
	if len(args) <= 1 || args[0] != "run" || strings.HasPrefix(args[1], "-") {
		return args, opts, nil
	}

	normalized := []string{args[1]}
	for i := 2; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--param":
			if i+1 >= len(args) {
				return nil, opts, fmt.Errorf("--param requires key=value")
			}
			i++
			if !strings.Contains(args[i], "=") {
				return nil, opts, fmt.Errorf("--param requires key=value")
			}
			normalized = append(normalized, args[i])
		case strings.HasPrefix(arg, "--param="):
			value := strings.TrimPrefix(arg, "--param=")
			if !strings.Contains(value, "=") {
				return nil, opts, fmt.Errorf("--param requires key=value")
			}
			normalized = append(normalized, value)
		case arg == "--until":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return nil, opts, fmt.Errorf("--until requires a step ID")
			}
			i++
			opts.until = args[i]
		case strings.HasPrefix(arg, "--until="):
			value := strings.TrimPrefix(arg, "--until=")
			if strings.TrimSpace(value) == "" {
				return nil, opts, fmt.Errorf("--until requires a step ID")
			}
			opts.until = value
		default:
			normalized = append(normalized, arg)
		}
	}
	return normalized, opts, nil
}

func handleResume(sessionID string, profile ...config.ProfileOverride) int {
	return handleResumeWithOptions(sessionID, liveTUIOptions{}, profile...)
}

func handleResumeWithOptions(sessionID string, liveOpts liveTUIOptions, profile ...config.ProfileOverride) int {
	liveOpts = liveOpts.withEnv()
	if err := requireTTY(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	stateFilePath, err := resolveResumeStatePath(sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	override := config.ProfileOverride{}
	if len(profile) > 0 {
		override = profile[0]
	}
	state, err := stateio.ReadState(stateFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	if override.Name == "" && state.ProfileSet != "" {
		override = config.ProfileOverride{Name: state.ProfileSet, Origin: config.OriginState}
	}
	profiles, err := config.LoadWithProfile(filepath.Join(".agent-runner", "config.yaml"), override)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	if os.Getenv("AGENT_RUNNER_NO_TUI") == "1" {
		result, runErr := runner.ResumeWorkflow(stateFilePath, &runner.Options{
			ProfileStore: profiles, ProfileOverride: override,
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
		return launchFrozenIntakeRoute(result, filepath.Dir(stateFilePath))
	}

	if code := ensureThemeForTUI(defaultThemeDeps); code != 0 {
		return code
	}

	h, err := runner.PrepareResume(stateFilePath, &runner.Options{
		ProfileStore: profiles, ProfileOverride: override,
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

	return launchResultAfterRun(runLiveTUIWithResult(h, liveOpts)).exitCode
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

func handleListWithProfile(profile config.ProfileOverride) int {
	return handleListWithDepsProfile(listview.InitialTabCurrentDir, defaultFirstRunDeps, profile)
}

func handleListWithDeps(initialTab listview.InitialTab, firstRun firstRunDeps) int {
	return handleListWithDepsProfile(initialTab, firstRun, config.ProfileOverride{})
}

func handleListWithDepsProfile(initialTab listview.InitialTab, firstRun firstRunDeps, profile config.ProfileOverride) int {
	if err := requireTTY(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if code := ensureThemeForTUI(defaultThemeDeps); code != 0 {
		return code
	}
	if result := ensureFirstRunForTUI(firstRun); !result.continueToList {
		return result.exitCode
	} else if len(result.listOptions) > 0 {
		return handleListAfterFirstRunWithProfile(initialTab, firstRun, result.listOptions, profile)
	}

	return handleListAfterFirstRunWithProfile(initialTab, firstRun, nil, profile)
}

func handleListAfterFirstRun(initialTab listview.InitialTab, firstRun firstRunDeps, extraOptions []func(*listview.Model)) int {
	return handleListAfterFirstRunWithProfile(initialTab, firstRun, extraOptions, config.ProfileOverride{})
}

func handleListAfterFirstRunWithProfile(initialTab listview.InitialTab, firstRun firstRunDeps, extraOptions []func(*listview.Model), profile config.ProfileOverride) int {
	settings := loadSplashSettingsForList(firstRun.load, os.Stderr)

	options := []func(*listview.Model){
		listview.WithInitialTab(initialTab),
		listview.WithVersion(version),
		listview.WithSplash(shouldShowSplash(&settings, firstRun.isStdinTTY(), firstRun.isStdoutTTY())),
	}
	options = append(options, extraOptions...)

	m, err := listview.New(options...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}

	sw := &switcher{list: m, mode: showingList}
	return runSwitcherWithProfile(sw, profile.Name)
}

func shouldShowSplash(settings *usersettings.Settings, stdinTTY, stdoutTTY bool) bool {
	return settings != nil && stdinTTY && stdoutTTY && settings.Splash.Dismissed == ""
}

func loadSplashSettingsForList(load func() (usersettings.Settings, error), stderr io.Writer) usersettings.Settings {
	settings, err := load()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent-runner: warning: could not load settings for splash: %v\n", err)
		return usersettings.Settings{}
	}
	return settings
}

func shouldShowOnboardingFailureModal(result liveTUIResult, settings *usersettings.Settings) bool {
	if settings == nil {
		settings = &usersettings.Settings{}
	}
	return result.workflowResult == runner.ResultFailed &&
		!result.exitRequested &&
		result.sessionDir != "" &&
		settings.Onboarding.Dismissed == ""
}

func runSwitcher(sw *switcher) int {
	return runSwitcherWithProfile(sw, "")
}

func runSwitcherWithProfile(sw *switcher, profile string) int {
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
			return execRunnerResumeWithProfile(final.resumeRunID, final.resumeRunProjectDir, profile)
		}
		if final.launchDebugRunID != "" || final.launchDebugSessionDir != "" {
			return execRunnerDebug(final.launchDebugRunID, final.launchDebugSessionDir, final.launchDebugProjectDir)
		}
		if final.startRunReady && final.startRunEntry != nil {
			return execStartRun(final.startRunEntry, final.startRunParams)
		}
		if final.startIntakeReady {
			return execStartIntake()
		}
		if final.resumeListProjectDir != "" {
			return execRunnerResumeWithProfile("", final.resumeListProjectDir, profile)
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
	quitOnDone       bool
	startInAltScreen bool
}

func (opts liveTUIOptions) withEnv() liveTUIOptions {
	if os.Getenv(liveRunImmediateAltScreenEnv) == "1" {
		opts.startInAltScreen = true
	}
	return opts
}

type liveTUIResult struct {
	exitCode       int
	exitRequested  bool
	workflowResult runner.WorkflowResult
	sessionDir     string
}

var liveRunCoordinatorFactory = func(program *tea.Program, sessionDir string) *liverun.Coordinator {
	return liverun.NewCoordinator(program, sessionDir)
}

func runLiveTUIWithResult(h *runner.RunHandle, opts liveTUIOptions) liveTUIResult {
	rv, err := runview.New(h.SessionDir, h.ProjectDir, runview.FromLiveRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return liveTUIResult{exitCode: 1, sessionDir: h.SessionDir}
	}

	programOptions := []tea.ProgramOption{tea.WithMouseCellMotion()}
	if opts.startInAltScreen {
		rv.StartInAltScreen()
		programOptions = append(programOptions, tea.WithAltScreen())
	}
	p := tea.NewProgram(rv, programOptions...)
	coord := liveRunCoordinatorFactory(p, h.SessionDir)

	resultCh := make(chan runner.WorkflowResult, 1)
	go func() {
		result := runner.ResultFailed
		var runErr error
		defer func() {
			if rec := recover(); rec != nil {
				_ = coord.NotifyDone(string(runner.ResultFailed), fmt.Errorf("panic: %v", rec))
				resultCh <- runner.ResultFailed
				return
			}
			notifyErr := coord.NotifyDone(string(result), runErr)
			if notifyErr != nil && result == runner.ResultSuccess {
				resultCh <- runner.ResultFailed
			} else {
				resultCh <- result
			}
			if opts.quitOnDone || shouldExitAfterFrozenIntakeRoute(result, h.SessionDir) {
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
		return liveTUIResult{exitCode: 1, sessionDir: h.SessionDir}
	}

	// If the user did not request a resume, map the runner result to an exit
	// code. If the user confirmed quit while the workflow was still running,
	// resultCh has no value yet — keep the documented orphan-on-quit behavior
	// and return 0 without blocking on the lingering goroutine.
	if result, ok := terminalLiveTUIResult(rv, resultCh, h.ProjectDir, h.SessionDir, opts); ok {
		return result
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
			return liveTUIResult{exitCode: 1}
		}
		p = tea.NewProgram(rv, tea.WithAltScreen(), tea.WithMouseCellMotion())
		rv, err = finalRunviewModel(p.Run())
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			return liveTUIResult{exitCode: 1}
		}
		if rv.ExitRequested() {
			return liveTUIResult{exitRequested: true, sessionDir: h.SessionDir}
		}
		if rv.ResumeToList() {
			return liveTUIResult{exitCode: execRunnerResume("", h.ProjectDir), sessionDir: h.SessionDir}
		}
		if rv.LaunchDebugRunID() != "" || rv.LaunchDebugSessionDir() != "" {
			return liveTUIResult{exitCode: execRunnerDebug(rv.LaunchDebugRunID(), rv.LaunchDebugSessionDir(), rv.LaunchDebugProjectDir()), sessionDir: h.SessionDir}
		}
	}
	return liveTUIResult{sessionDir: h.SessionDir}
}

func terminalLiveTUIResult(rv *runview.Model, resultCh <-chan runner.WorkflowResult, projectDir, sessionDir string, opts liveTUIOptions) (liveTUIResult, bool) {
	if rv.ResumeSessionID() != "" {
		return liveTUIResult{}, false
	}
	if rv.ExitRequested() {
		return liveTUIResult{exitRequested: true, sessionDir: sessionDir}, true
	}
	if rv.ResumeToList() {
		<-resultCh
		return liveTUIResult{exitCode: execRunnerResume("", projectDir), sessionDir: sessionDir}, true
	}
	if rv.LaunchDebugRunID() != "" || rv.LaunchDebugSessionDir() != "" {
		<-resultCh
		return liveTUIResult{exitCode: execRunnerDebug(rv.LaunchDebugRunID(), rv.LaunchDebugSessionDir(), rv.LaunchDebugProjectDir()), sessionDir: sessionDir}, true
	}
	if opts.quitOnDone {
		return dispatcherLiveTUIResult(resultCh, sessionDir), true
	}
	return completedLiveTUIResult(resultCh, sessionDir), true
}

func dispatcherLiveTUIResult(resultCh <-chan runner.WorkflowResult, sessionDir string) liveTUIResult {
	select {
	case runResult := <-resultCh:
		if runResult != runner.ResultSuccess {
			return liveTUIResult{exitCode: 1, workflowResult: runResult, sessionDir: sessionDir}
		}
		return liveTUIResult{workflowResult: runResult, sessionDir: sessionDir}
	default:
		// User quit before the dispatcher-launched workflow reached a terminal
		// state; preserve the normal live-run orphan behavior.
	}
	return liveTUIResult{sessionDir: sessionDir}
}

func completedLiveTUIResult(resultCh <-chan runner.WorkflowResult, sessionDir string) liveTUIResult {
	select {
	case runResult := <-resultCh:
		if runResult != runner.ResultSuccess {
			return liveTUIResult{exitCode: 1, workflowResult: runResult, sessionDir: sessionDir}
		}
		return liveTUIResult{workflowResult: runResult, sessionDir: sessionDir}
	default:
	}
	return liveTUIResult{sessionDir: sessionDir}
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
	return execSelfWithEnv(nil, args...)
}

func execSelfWithEnv(extraEnv []string, args ...string) int {
	self, err := currentExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: cannot resolve executable path: %v\n", err)
		return 1
	}
	execArgs := append([]string{filepath.Base(self)}, args...)
	env := envWithOverrides(os.Environ(), extraEnv...)
	if err := execProcess(self, execArgs, env); err != nil { // #nosec G204 -- self is our own os.Executable() path; args are validated workflow names / run IDs
		fmt.Fprintf(os.Stderr, "agent-runner: exec %s: %v\n", strings.Join(args, " "), err)
		return 1
	}
	return 0
}

// launchFrozenIntakeRoute performs the final gate after a top-level run has
// finalized. The exec itself deliberately happens only after the parent's
// control socket, lock, state, and audit logger have all been closed.
func launchFrozenIntakeRoute(result runner.WorkflowResult, sessionDir string) int {
	if result != runner.ResultSuccess {
		return 0
	}

	routePath, err := filepath.Abs(intakeroute.SidecarPath(sessionDir))
	if err != nil {
		return 0
	}
	sealed, err := intakeroute.LoadStrict(routePath)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: read intake route: %v\n", err)
		return 1
	}
	if sealed.State != intakeroute.Frozen {
		return 0
	}

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: read completed intake state: %v\n", err)
		return 1
	}
	if !state.Completed {
		return 0
	}
	if err := appendRouteLaunchEvent(sessionDir, audit.EventRouteLaunchAttempted, sealed, nil); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: record intake route launch: %v\n", err)
		return 1
	}

	code := execSelfWithEnv([]string{liveRunImmediateAltScreenEnv + "=1"}, "internal", "launch-intake-route", routePath)
	if code != 0 {
		launchErr := fmt.Errorf("exec launch-intake-route exited with code %d", code)
		fmt.Fprintf(os.Stderr, "agent-runner: intake route launch failed: %v\n", launchErr)
		if err := appendRouteLaunchEvent(sessionDir, audit.EventRouteLaunchFailed, sealed, launchErr); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: record failed intake route launch: %v\n", err)
		}
	}
	return code
}

func shouldExitAfterFrozenIntakeRoute(result runner.WorkflowResult, sessionDir string) bool {
	if result != runner.ResultSuccess {
		return false
	}
	// Probe the sidecar first: it is absent for every non-intake run, so the
	// common path costs one failed open rather than a full state.json read.
	sealed, err := intakeroute.LoadStrict(intakeroute.SidecarPath(sessionDir))
	if err != nil || sealed.State != intakeroute.Frozen {
		return false
	}
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	return err == nil && state.Completed
}

func appendRouteLaunchEvent(sessionDir string, eventType audit.EventType, sealed *intakeroute.Sealed, launchErr error) error {
	logger, err := audit.NewLogger(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		return err
	}
	defer logger.Close()
	data := map[string]any{
		"workflow":      sealed.Workflow,
		"source_ref":    sealed.SourceRef,
		"params":        sealed.Params,
		"handoff_bytes": len(sealed.Handoff),
	}
	if launchErr != nil {
		data["error"] = launchErr.Error()
	}
	logger.Emit(audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Type:      eventType,
		Data:      data,
	})
	return nil
}

func envWithOverrides(base []string, overrides ...string) []string {
	if len(overrides) == 0 {
		return base
	}
	remove := make(map[string]bool, len(overrides))
	for _, item := range overrides {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			remove[key] = true
		}
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if ok && remove[key] {
			continue
		}
		out = append(out, item)
	}
	out = append(out, overrides...)
	return out
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
func resolveResumeCLI(cliName string) (resolvedCLI, path string, err error) {
	if cliName == "" {
		cliName = "claude"
	}
	if strings.ContainsAny(cliName, `/\`) || !allowedResumeCLIs[cliName] {
		return cliName, "", fmt.Errorf("refusing to resume: unsupported agent CLI %q", cliName)
	}
	path, err = exec.LookPath(cliName)
	if err != nil {
		return cliName, "", fmt.Errorf("cannot find agent CLI %q in PATH: %w", cliName, err)
	}
	return cliName, path, nil
}

// spawnAgentResume spawns `<cli> --resume <session-id>` as a subprocess and
// waits for it to exit. It does not replace the current process, so the
// caller can re-enter the run view after the CLI exits. A non-zero CLI exit
// code is not treated as an error — the user may have typed /exit or /quit.
// Only spawn failures (binary not found, permission error, etc.) are
// returned as errors.
func spawnAgentResume(cliName, sessionID string) error {
	resolved, path, err := resolveResumeCLI(cliName)
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
	return execRunnerResumeWithProfile(runID, projectDir, "")
}

func execRunnerResumeWithProfile(runID, projectDir, profile string) int {
	if projectDir != "" {
		if err := os.Chdir(projectDir); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: chdir %s: %v\n", projectDir, err)
			return 1
		}
	}
	args := []string{"--resume"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	if runID != "" {
		args = append(args, runID)
		return execSelfWithEnv([]string{liveRunImmediateAltScreenEnv + "=1"}, args...)
	}
	return execSelf(args...)
}

func launchDebugArgs(runID, sessionDir string) []string {
	if sessionDir != "" {
		return []string{"run", "core:debug", "--param", "failed_session_dir=" + sessionDir}
	}
	return []string{"run", "core:debug", "--param", "failed_run_id=" + runID}
}

var execRunnerDebug = func(runID, sessionDir, projectDir string) int {
	if projectDir != "" {
		if err := os.Chdir(projectDir); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: chdir %s: %v\n", projectDir, err)
			return 1
		}
	}
	return execSelfWithEnv([]string{liveRunImmediateAltScreenEnv + "=1"}, launchDebugArgs(runID, sessionDir)...)
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

	return execSelfWithEnv([]string{liveRunImmediateAltScreenEnv + "=1"}, args...)
}

func execStartIntake() int {
	return execSelfWithEnv([]string{liveRunImmediateAltScreenEnv + "=1"}, "-i")
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

	resumeAgentCLI        string
	resumeSessionID       string
	resumeRunID           string
	resumeRunProjectDir   string
	launchDebugRunID      string
	launchDebugSessionDir string
	launchDebugProjectDir string
	resumeListProjectDir  string
	startRunEntry         *discovery.WorkflowEntry
	startRunParams        map[string]string
	startRunReady         bool
	startIntakeReady      bool
	viewErr               string
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
		s.startRunParams = maps.Clone(msg.Params)
		s.startRunReady = false
		if len(msg.Params) > 0 {
			s.startRunReady = true
			return s, tea.Quit
		}
		if len(entry.Params) == 0 {
			s.startRunReady = true
			return s, tea.Quit
		}
		s.returnMode = s.mode
		s.paramform = paramform.New(&entry).WithWidth(s.termWidth)
		s.mode = showingParamForm
		return s, nil

	case discovery.StartIntakeMsg:
		s.startIntakeReady = true
		return s, tea.Quit

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

	case runview.LaunchDebugMsg:
		s.launchDebugRunID = msg.FailedRunID
		s.launchDebugSessionDir = msg.FailedSessionDir
		s.launchDebugProjectDir = msg.FailedProjectDir
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

func handleValidateArgs(args []string, profile ...config.ProfileOverride) int {
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
	override := config.ProfileOverride{}
	if len(profile) > 0 {
		override = profile[0]
	}
	result, err := prevalidate.Pipeline(workflowFile, keyed, prevalidate.Lenient, prevalidate.Options{ProfileOverride: override})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return 1
	}
	for i := range result.DeferredWarnings {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", result.DeferredWarnings[i])
	}
	for _, warning := range result.AgentDeprecations {
		fmt.Fprintf(os.Stderr, "agent-runner: warning: %s\n", warning)
	}
	if result.ResolvedProfile != "" {
		fmt.Printf("workflow is valid (profile set: %s)\n", result.ResolvedProfile)
	} else {
		fmt.Println("workflow is valid")
	}
	return 0
}

func resolveValidateWorkflowArg(arg string) (string, error) {
	if builtinworkflows.IsRef(arg) && workflowcatalog.HasYAMLExtension(arg) {
		if _, err := builtinworkflows.ReadFile(arg); err != nil {
			return "", fmt.Errorf("workflow file %q does not exist", arg)
		}
		return arg, nil
	}
	if workflowcatalog.HasYAMLExtension(arg) {
		if fileExists(arg) {
			return arg, nil
		}
		return "", fmt.Errorf("workflow file %q does not exist", arg)
	}
	return resolveWorkflowArg(arg)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

var (
	workflowNamePattern          = regexp.MustCompile(`^[a-z0-9_-]+(:[a-z0-9_-]+|(/[a-z0-9_-]+)+)?$`)
	uppercaseWorkflowNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(:[A-Za-z0-9_-]+|(/[A-Za-z0-9_-]+)+)?$`)
	versionedLaunchPattern       = regexp.MustCompile(`^(.*)-v\d+\.\d+$`)
	dotlessVersionAttemptPattern = regexp.MustCompile(`^(.*)-v\d+$`)
)

func resolveWorkflowArg(arg string) (string, error) {
	if logicalName, ok := versionFreeLaunchHint(arg); ok {
		return "", fmt.Errorf(
			"invalid workflow name %q for execution; launch logical workflow %q so Agent Runner can select the latest version",
			arg,
			logicalName,
		)
	}
	if !workflowNamePattern.MatchString(arg) {
		if uppercaseWorkflowNamePattern.MatchString(arg) && arg != strings.ToLower(arg) {
			return "", fmt.Errorf("invalid workflow name %q for execution: logical workflow names must be lowercase", arg)
		}
		return "", fmt.Errorf("invalid workflow name %q: use a bare name or path under .agent-runner/workflows/ (e.g., 'myworkflow' or 'team/deploy') or a builtin name like 'core:finalize-pr'", arg)
	}
	if strings.Contains(arg, ":") {
		resolved, err := builtinworkflows.Resolve(arg)
		if err == nil {
			return resolved, nil
		}
		var groupErr *workflowcatalog.GroupError
		if logicalName, ok := dotlessVersionLaunchHint(arg); ok && !errors.As(err, &groupErr) {
			return "", fmt.Errorf("%w; launch logical workflow %q to select its latest version", err, logicalName)
		}
		return "", err
	}

	projectRoot := filepath.Join(".agent-runner", "workflows")
	if resolved, found, err := resolveWorkflowCatalogGroup(projectRoot, arg); err != nil {
		return "", err
	} else if found {
		return resolved, nil
	}

	var globalRoot string
	var homeErr error
	if home, err := userHomeDir(); err == nil {
		globalRoot = filepath.Join(home, ".agent-runner", "workflows")
		if resolved, found, err := resolveWorkflowCatalogGroup(globalRoot, arg); err != nil {
			return "", err
		} else if found {
			return resolved, nil
		}
	} else {
		homeErr = err
	}

	searched := []string{projectRoot}
	if globalRoot != "" {
		searched = append(searched, globalRoot)
	}
	message := fmt.Sprintf("logical workflow %q not found (searched %s)", arg, strings.Join(searched, ", "))
	if homeErr != nil {
		message += "; global workflow directory was unavailable"
	}
	if logicalName, ok := dotlessVersionLaunchHint(arg); ok {
		message += fmt.Sprintf("; launch logical workflow %q to select its latest version", logicalName)
	}
	return "", errors.New(message)
}

func resolveWorkflowCatalogGroup(root, logicalName string) (resolved string, found bool, err error) {
	logicalDir := "."
	if slash := strings.LastIndex(logicalName, "/"); slash >= 0 {
		logicalDir = logicalName[:slash]
	}
	scanRoot := root
	if logicalDir != "." {
		scanRoot = filepath.Join(root, filepath.FromSlash(logicalDir))
	}
	entries, err := os.ReadDir(scanRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read workflow source %s: %w", scanRoot, err)
	}

	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if workflowcatalog.HasYAMLExtension(entry.Name()) {
			candidatePath := entry.Name()
			if logicalDir != "." {
				candidatePath = logicalDir + "/" + candidatePath
			}
			candidates = append(candidates, candidatePath)
		}
	}

	group, found := workflowcatalog.Build(candidates).Lookup(logicalName)
	if !found {
		return "", false, nil
	}
	if group.Err != nil {
		return "", true, qualifyWorkflowGroupError(root, group.Err)
	}
	if group.Selected == nil {
		return "", true, fmt.Errorf("logical workflow %q has no selectable definition in %s", logicalName, root)
	}
	return filepath.Join(root, filepath.FromSlash(group.Selected.Path)), true, nil
}

func qualifyWorkflowGroupError(root string, groupErr *workflowcatalog.GroupError) *workflowcatalog.GroupError {
	qualified := *groupErr
	qualified.InvalidFilenames = append([]workflowcatalog.FilenameError(nil), groupErr.InvalidFilenames...)
	for i := range qualified.InvalidFilenames {
		qualified.InvalidFilenames[i].Path = filepath.Join(root, filepath.FromSlash(qualified.InvalidFilenames[i].Path))
	}
	qualified.DuplicateVersions = append([]workflowcatalog.DuplicateVersionError(nil), groupErr.DuplicateVersions...)
	for i := range qualified.DuplicateVersions {
		qualified.DuplicateVersions[i].Paths = append([]string(nil), qualified.DuplicateVersions[i].Paths...)
		for j := range qualified.DuplicateVersions[i].Paths {
			qualified.DuplicateVersions[i].Paths[j] = filepath.Join(root, filepath.FromSlash(qualified.DuplicateVersions[i].Paths[j]))
		}
	}
	return &qualified
}

func versionFreeLaunchHint(arg string) (string, bool) {
	trimmed := strings.TrimSpace(arg)
	normalized := filepath.ToSlash(trimmed)
	hadYAMLExtension := workflowcatalog.HasYAMLExtension(normalized)
	if hadYAMLExtension {
		normalized = strings.TrimSuffix(normalized, filepath.Ext(normalized))
	}
	matches := versionedLaunchPattern.FindStringSubmatch(normalized)
	if matches == nil {
		return "", false
	}
	logicalName := matches[1]
	if hadYAMLExtension &&
		strings.Contains(logicalName, "/") &&
		isRejectedFilesystemWorkflowPath(trimmed, logicalName) {
		logicalName = logicalNameFromRejectedPath(logicalName)
	}
	return logicalName, logicalName != ""
}

func isRejectedFilesystemWorkflowPath(arg, candidate string) bool {
	if filepath.IsAbs(arg) {
		return true
	}
	if strings.HasPrefix(candidate, "./") || strings.HasPrefix(candidate, "../") || strings.HasPrefix(candidate, "builtin:") {
		return true
	}
	return strings.HasPrefix(candidate, "workflows/") ||
		strings.HasPrefix(candidate, ".agent-runner/workflows/")
}

func logicalNameFromRejectedPath(candidate string) string {
	candidate = strings.TrimPrefix(pathpkg.Clean(candidate), "./")
	if builtinPath := strings.TrimPrefix(candidate, "builtin:"); builtinPath != candidate {
		if namespace, logicalName, ok := strings.Cut(builtinPath, "/"); ok && namespace != "" && logicalName != "" {
			return namespace + ":" + logicalName
		}
	}
	parts := strings.Split(candidate, "/")
	for index := len(parts) - 2; index >= 0; index-- {
		if parts[index] != "workflows" {
			continue
		}
		logicalName := strings.Join(parts[index+1:], "/")
		if logicalName != "" {
			return logicalName
		}
	}
	return pathBase(candidate)
}

func dotlessVersionLaunchHint(arg string) (string, bool) {
	slash := strings.LastIndex(arg, "/")
	prefix, finalName := "", arg
	if slash >= 0 {
		prefix, finalName = arg[:slash+1], arg[slash+1:]
	}
	matches := dotlessVersionAttemptPattern.FindStringSubmatch(finalName)
	if len(matches) < 2 || matches[1] == "" {
		return "", false
	}
	return prefix + matches[1], true
}

func pathBase(value string) string {
	if slash := strings.LastIndex(value, "/"); slash >= 0 {
		return value[slash+1:]
	}
	return value
}

func handleRunWithResult(args []string, liveOpts liveTUIOptions) liveTUIResult {
	return handleRunWithRunOptions(args, &runCommandOptions{liveOpts: liveOpts})
}

type runCommandOptions struct {
	liveOpts        liveTUIOptions
	from            string
	until           string
	profileOverride config.ProfileOverride
	headless        bool
	agentOverride   *model.AgentOverride
}

// freshRunRequest contains everything needed to prepare a new top-level run.
// Both direct CLI launches and sealed intake routes use this exact sequence so
// validation and engine setup cannot drift between the two entry points.
type freshRunRequest struct {
	SourceRef             string
	Positional            []string
	Keyed                 map[string]string
	From                  string
	Until                 string
	AgentOverride         *model.AgentOverride
	ProfileOverride       config.ProfileOverride
	IntakeParentRunID     string
	IntakeHandoffContents string
	Log                   iexec.Logger
}

func prepareFreshRun(req *freshRunRequest) (*runner.RunHandle, error) {
	workflow, err := loader.LoadWorkflow(req.SourceRef, loader.Options{})
	if err != nil {
		return nil, fmt.Errorf("load workflow: %w", err)
	}
	profileStore, err := config.LoadWithProfile(filepath.Join(".agent-runner", "config.yaml"), req.ProfileOverride)
	if err != nil {
		return nil, err
	}
	params, err := matchParamsForLaunch(&workflow, req.Positional, req.Keyed, workflow.Scope == model.ScopeRepositories && len(profileStore.Repositories) == 0)
	if err != nil {
		return nil, err
	}
	if !builtinworkflows.IsRef(req.SourceRef) {
		if _, err := prevalidate.Pipeline(req.SourceRef, params, prevalidate.Strict, prevalidate.Options{ProfileOverride: req.ProfileOverride}); err != nil {
			return nil, err
		}
	}

	var eng engine.Engine
	if workflow.Engine != nil {
		engConfig := map[string]any{"type": workflow.Engine.Type}
		maps.Copy(engConfig, workflow.Engine.Extras)
		eng, err = engine.Create(engConfig)
		if err != nil {
			return nil, fmt.Errorf("create engine: %w", err)
		}
	}
	log := req.Log
	if log == nil {
		log = &realLogger{}
	}
	return runner.PrepareRun(&workflow, params, &runner.Options{
		ProfileOverride:       req.ProfileOverride,
		ProfileStore:          profileStore,
		WorkflowFile:          req.SourceRef,
		From:                  req.From,
		Until:                 req.Until,
		AgentOverride:         req.AgentOverride,
		IntakeParentRunID:     req.IntakeParentRunID,
		IntakeHandoffContents: req.IntakeHandoffContents,
		Engine:                eng,
		ProcessRunner:         &realProcessRunner{},
		GlobExpander:          &realGlobExpander{},
		Log:                   log,
	})
}

func isIntakeWorkflow(workflowFile string) bool {
	return builtinworkflows.IsIntakeRef(workflowFile)
}

func validateIntakeRunInvocation(workflowFile string, headless bool) error {
	if isIntakeWorkflow(workflowFile) && headless {
		return fmt.Errorf("agent-runner: intake requires an interactive terminal and cannot run with --headless")
	}
	return nil
}

func handleRunWithRunOptions(args []string, runOpts *runCommandOptions) liveTUIResult {
	liveOpts := runOpts.liveOpts.withEnv()
	workflowFile := args[0]
	if err := validateIntakeRunInvocation(workflowFile, runOpts.headless); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return liveTUIResult{exitCode: 1}
	}

	positional, keyed, err := parseParams(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return liveTUIResult{exitCode: 1}
	}
	if isIntakeWorkflow(workflowFile) {
		if err := requireIntakeTTY(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return liveTUIResult{exitCode: 1}
		}
	}

	if err := requireTTY(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return liveTUIResult{exitCode: 1}
	}

	prepare := func(log iexec.Logger) (*runner.RunHandle, error) {
		return prepareFreshRun(&freshRunRequest{
			SourceRef: workflowFile, Positional: positional, Keyed: keyed,
			From: runOpts.from, Until: runOpts.until, AgentOverride: runOpts.agentOverride,
			ProfileOverride: runOpts.profileOverride, Log: log,
		})
	}

	if os.Getenv("AGENT_RUNNER_NO_TUI") == "1" {
		h, err := prepare(&realLogger{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			return liveTUIResult{exitCode: 1}
		}
		result := runner.ExecuteFromHandle(h, &runner.Options{
			ProcessRunner: &realProcessRunner{},
			GlobExpander:  &realGlobExpander{},
			Log:           &realLogger{},
		})
		return launchResultAfterRun(liveTUIResult{workflowResult: result, sessionDir: h.SessionDir, exitCode: resultExitCode(result)})
	}

	if code := ensureThemeForTUI(defaultThemeDeps); code != 0 {
		return liveTUIResult{exitCode: code}
	}

	h, err := prepare(&runner.DiscardLogger{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return liveTUIResult{exitCode: 1}
	}

	return launchResultAfterRun(runLiveTUIWithResult(h, liveOpts))
}

func resultExitCode(result runner.WorkflowResult) int {
	if result != runner.ResultSuccess {
		return 1
	}
	return 0
}

func launchResultAfterRun(result liveTUIResult) liveTUIResult {
	if result.workflowResult != runner.ResultSuccess || result.sessionDir == "" {
		return result
	}
	if code := launchFrozenIntakeRoute(result.workflowResult, result.sessionDir); code != 0 {
		result.exitCode = code
		return result
	}
	return result
}

func handleOnboardingFromRun(workflowFile, from string, runOpts *runCommandOptions, args ...string) int {
	runArgs := append([]string{workflowFile}, args...)
	runOpts.from = from
	result := handleRunWithRunOptions(runArgs, runOpts)
	settings, err := usersettings.Load()
	if err != nil {
		settings = usersettings.Settings{}
	}
	if !shouldShowOnboardingFailureModal(result, &settings) {
		return result.exitCode
	}
	reason := runview.FailureReasonForSession(result.sessionDir)
	return handleListAfterFirstRun(
		listview.InitialTabCurrentDir,
		defaultFirstRunDeps,
		[]func(*listview.Model){listview.WithOnboardingFailure(result.sessionDir, reason)},
	)
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
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(theme == usersettings.ThemeDark)
}

type nativeSetupResult int

const (
	nativeSetupCompleted nativeSetupResult = iota
	nativeSetupCancelled
	nativeSetupFailed
	nativeSetupDemo
	nativeSetupExitRequested
)

type firstRunDeps struct {
	load                          func() (usersettings.Settings, error)
	isStdinTTY                    func() bool
	isStdoutTTY                   func() bool
	runNativeSetup                func(onboardingCompleted bool) (nativeSetupResult, error)
	runDemoPrompt                 func() (nativeSetupResult, error)
	runDemoPromptFlow             func() firstRunResult
	runDemoLaunchFlow             func() firstRunResult
	continueAfterNativeSetupError bool
	runWorkflow                   func(ref string) firstRunWorkflowResult
}

type firstRunResult struct {
	exitCode       int
	continueToList bool
	listOptions    []func(*listview.Model)
}

type firstRunWorkflowResult struct {
	exitCode       int
	exitRequested  bool
	workflowResult runner.WorkflowResult
	sessionDir     string
}

func continueToList() firstRunResult {
	return firstRunResult{continueToList: true}
}

func exitFirstRun(code int) firstRunResult {
	return firstRunResult{exitCode: code}
}

var defaultFirstRunDeps = firstRunDeps{
	load:        usersettings.Load,
	isStdinTTY:  func() bool { return isatty.IsTerminal(os.Stdin.Fd()) },
	isStdoutTTY: func() bool { return isatty.IsTerminal(os.Stdout.Fd()) },
	runNativeSetup: func(onboardingCompleted bool) (nativeSetupResult, error) {
		result, err := nativesetup.Run(&nativesetup.Deps{
			OnboardingCompleted: onboardingCompleted,
			Plugin:              nativesetup.DefaultPluginInstaller{},
		})
		return mapSetupResult(result), err
	},
	runDemoPrompt: func() (nativeSetupResult, error) {
		result, err := nativesetup.RunDemoPrompt(&nativesetup.Deps{})
		return mapSetupResult(result), err
	},
	runDemoPromptFlow: runOnboardingDemoPromptFlow,
	runDemoLaunchFlow: runOnboardingDemoLaunchFlow,
	runWorkflow: func(ref string) firstRunWorkflowResult {
		result := handleRunWithResult([]string{ref}, liveTUIOptions{quitOnDone: true, startInAltScreen: true})
		return firstRunWorkflowResult(result)
	},
}

func firstRunDepsWithOnboardingFrom(from string) firstRunDeps {
	from = strings.TrimSpace(from)
	if from == "" {
		return defaultFirstRunDeps
	}
	deps := defaultFirstRunDeps
	deps.runDemoPromptFlow = func() firstRunResult {
		return runOnboardingDemoPromptFlowFrom(from)
	}
	deps.runDemoLaunchFlow = func() firstRunResult {
		return runOnboardingDemoLaunchFlowFrom(from)
	}
	deps.runWorkflow = func(ref string) firstRunWorkflowResult {
		result := handleRunWithRunOptions([]string{ref}, &runCommandOptions{
			liveOpts: liveTUIOptions{quitOnDone: true, startInAltScreen: true},
			from:     from,
		})
		return firstRunWorkflowResult(result)
	}
	return deps
}

func mapSetupResult(result nativesetup.Result) nativeSetupResult {
	switch result {
	case nativesetup.ResultCompleted:
		return nativeSetupCompleted
	case nativesetup.ResultDemo:
		return nativeSetupDemo
	case nativesetup.ResultFailed:
		return nativeSetupFailed
	case nativesetup.ResultExitRequested:
		return nativeSetupExitRequested
	default:
		return nativeSetupCancelled
	}
}

func ensureFirstRunForTUI(deps firstRunDeps) firstRunResult {
	settings, err := deps.load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return exitFirstRun(1)
	}
	if !deps.isStdinTTY() || !deps.isStdoutTTY() {
		return continueToList()
	}

	onboardingDone := settings.Onboarding.CompletedAt != "" || settings.Onboarding.Dismissed != ""

	if settings.Setup.CompletedAt == "" {
		if err := splash.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: splash: %v\n", err)
		}
		result, err := deps.runNativeSetup(onboardingDone)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			if !deps.continueAfterNativeSetupError {
				return exitFirstRun(1)
			}
			return continueToList()
		}
		switch result {
		case nativeSetupCompleted:
			return continueToList()
		case nativeSetupDemo:
			return launchOnboardingDemo(deps)
		case nativeSetupFailed:
			return exitFirstRun(1)
		default:
			// Cancelled, ExitRequested, or any non-completion outcome:
			// setup is the only path into the home TUI, so any exit short of
			// completion exits the program.
			return exitFirstRun(0)
		}
	}

	if !onboardingDone {
		if deps.runDemoPromptFlow != nil {
			return deps.runDemoPromptFlow()
		}
		result, err := deps.runDemoPrompt()
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
			return continueToList()
		}
		if result == nativeSetupDemo {
			return launchOnboardingDemo(deps)
		}
		if result == nativeSetupExitRequested {
			return exitFirstRun(0)
		}
	}
	return continueToList()
}

func launchOnboardingDemo(deps firstRunDeps) firstRunResult {
	if deps.runDemoLaunchFlow != nil {
		return deps.runDemoLaunchFlow()
	}
	ref, err := builtinworkflows.Resolve("onboarding:onboarding")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return exitFirstRun(1)
	}
	result := deps.runWorkflow(ref)
	liveResult := liveTUIResult(result)
	settings, err := deps.load()
	if err != nil {
		settings = usersettings.Settings{}
	}
	if shouldShowOnboardingFailureModal(liveResult, &settings) {
		reason := runview.FailureReasonForSession(result.sessionDir)
		return firstRunResult{
			continueToList: true,
			listOptions: []func(*listview.Model){
				listview.WithOnboardingFailure(result.sessionDir, reason),
			},
		}
	}
	if result.exitRequested || result.exitCode != 0 {
		return exitFirstRun(result.exitCode)
	}
	return continueToList()
}

type onboardingDemoPromptFlowMode int

const (
	onboardingDemoPromptMode onboardingDemoPromptFlowMode = iota
	onboardingDemoRunMode
)

type onboardingDemoPromptFlow struct {
	prompt *nativesetup.Model
	run    *runview.Model
	handle *runner.RunHandle
	ref    string
	from   string
	opts   liveTUIOptions

	program *tea.Program
	mode    onboardingDemoPromptFlowMode

	resultCh      chan runner.WorkflowResult
	exitCode      int
	exitRequested bool
	started       bool
	preparingRun  bool
	loadingPhase  float64
	termWidth     int
	termHeight    int
}

type onboardingDemoPrepareMsg struct {
	handle   *runner.RunHandle
	exitCode int
}

type onboardingDemoLoadingTick struct{}

func runOnboardingDemoPromptFlow() firstRunResult {
	return runOnboardingDemoPromptFlowFrom("")
}

func runOnboardingDemoLaunchFlow() firstRunResult {
	return runOnboardingDemoLaunchFlowFrom("")
}

func runOnboardingDemoPromptFlowFrom(from string) firstRunResult {
	return runOnboardingDemoFlowFrom(from, false)
}

func runOnboardingDemoLaunchFlowFrom(from string) firstRunResult {
	return runOnboardingDemoFlowFrom(from, true)
}

func runOnboardingDemoFlowFrom(from string, preparing bool) firstRunResult {
	ref, err := builtinworkflows.Resolve("onboarding:onboarding")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return exitFirstRun(1)
	}
	m := &onboardingDemoPromptFlow{
		ref:          ref,
		from:         strings.TrimSpace(from),
		opts:         liveTUIOptions{quitOnDone: true, startInAltScreen: true},
		preparingRun: preparing,
	}
	if !preparing {
		m.prompt = nativesetup.NewDemoPromptModel(&nativesetup.Deps{})
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	m.program = p
	final, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return exitFirstRun(1)
	}
	fm, ok := final.(*onboardingDemoPromptFlow)
	if !ok {
		fmt.Fprintf(os.Stderr, "agent-runner: unexpected onboarding demo model %T\n", final)
		return exitFirstRun(1)
	}
	settings, err := usersettings.Load()
	if err != nil {
		settings = usersettings.Settings{}
	}
	return finishOnboardingDemoFlow(fm, &settings)
}

func finishOnboardingDemoFlow(fm *onboardingDemoPromptFlow, settings *usersettings.Settings) firstRunResult {
	var workflowResult runner.WorkflowResult
	if fm.exitRequested || fm.exitCode != 0 {
		return exitFirstRun(fm.exitCode)
	}
	if fm.resultCh != nil {
		workflowResult = <-fm.resultCh
	}
	sessionDir := ""
	if fm.handle != nil {
		sessionDir = fm.handle.SessionDir
	}
	liveResult := liveTUIResult{
		exitCode:       fm.exitCode,
		exitRequested:  fm.exitRequested,
		workflowResult: workflowResult,
		sessionDir:     sessionDir,
	}
	if shouldShowOnboardingFailureModal(liveResult, settings) {
		reason := runview.FailureReasonForSession(sessionDir)
		return firstRunResult{
			continueToList: true,
			listOptions: []func(*listview.Model){
				listview.WithOnboardingFailure(sessionDir, reason),
			},
		}
	}
	return continueToList()
}

func prepareBuiltinOnboardingRun(ref, from string) (handle *runner.RunHandle, exitCode int) {
	from = strings.TrimSpace(from)
	if from == "" {
		if statePath, ok, err := findLatestIncompleteOnboardingRunState(ref); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: find onboarding run: %v\n", err)
			return nil, 1
		} else if ok {
			h, err := runner.PrepareResume(statePath, &runner.Options{
				ProcessRunner: &realProcessRunner{},
				GlobExpander:  &realGlobExpander{},
				Log:           &runner.DiscardLogger{},
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
				return nil, 1
			}
			return h, 0
		}
	}

	workflow, err := loader.LoadWorkflow(ref, loader.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: load workflow: %v\n", err)
		return nil, 1
	}
	sessionDir, err := newOnboardingSessionDir(workflow.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: prepare onboarding run: %v\n", err)
		return nil, 1
	}
	h, err := runner.PrepareRun(&workflow, nil, &runner.Options{
		WorkflowFile:  ref,
		From:          from,
		SessionDir:    sessionDir,
		ProcessRunner: &realProcessRunner{},
		GlobExpander:  &realGlobExpander{},
		Log:           &runner.DiscardLogger{},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		return nil, 1
	}
	return h, 0
}

func findLatestIncompleteOnboardingRunState(ref string) (statePath string, ok bool, err error) {
	runsDir, err := onboardingRunsDir()
	if err != nil {
		return "", false, err
	}
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read runs dir: %w", err)
	}

	type candidate struct {
		statePath string
		sessionID string
		modUnix   int64
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		statePath := filepath.Join(runsDir, entry.Name(), "state.json")
		info, err := os.Stat(statePath)
		if err != nil {
			continue
		}
		state, err := stateio.ReadState(statePath)
		if err != nil {
			continue
		}
		if state.WorkflowFile != ref || state.Completed {
			continue
		}
		stateChanged := false
		if runStateCurrentStepID(&state) == "" {
			inferred := inferOnboardingResumeStepFromAudit(filepath.Dir(statePath))
			if inferred == "" {
				inferred = firstWorkflowStepID(ref)
			}
			if inferred == "" {
				continue
			}
			state.CurrentStep = model.CurrentStep{Nested: &model.NestedStepState{StepID: inferred}}
			stateChanged = true
		}
		if rewindOnboardingGuidedDirectoryGate(&state) {
			stateChanged = true
		}
		if stateChanged {
			if err := stateio.WriteState(&state, filepath.Dir(statePath)); err != nil {
				return "", false, fmt.Errorf("persist repaired onboarding state %s: %w", statePath, err)
			}
		}
		candidates = append(candidates, candidate{
			statePath: statePath,
			sessionID: entry.Name(),
			modUnix:   info.ModTime().UnixNano(),
		})
	}
	if len(candidates) == 0 {
		return "", false, nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modUnix == candidates[j].modUnix {
			return candidates[i].sessionID > candidates[j].sessionID
		}
		return candidates[i].modUnix > candidates[j].modUnix
	})
	return candidates[0].statePath, true, nil
}

func rewindOnboardingGuidedDirectoryGate(state *model.RunState) bool {
	if state == nil || state.CurrentStep.Nested == nil {
		return false
	}
	root := state.CurrentStep.Nested
	if root.StepID != "guided-workflow" || root.Child == nil {
		return false
	}
	if !isOnboardingGuidedDirectoryGateStep(root.Child.StepID) {
		return false
	}
	capturedCWD, ok := root.Child.CapturedVariables["cwd"].StringValue()
	if !ok || strings.TrimSpace(capturedCWD) == "" {
		return false
	}
	cwd, err := os.Getwd()
	if err != nil || sameCleanPath(capturedCWD, cwd) {
		return false
	}

	root.Child = &model.NestedStepState{
		StepID:            "capture-cwd",
		SessionIDs:        maps.Clone(root.Child.SessionIDs),
		SessionProfiles:   maps.Clone(root.Child.SessionProfiles),
		CapturedVariables: map[string]model.CapturedValue{},
		LastSessionStepID: root.Child.LastSessionStepID,
	}
	return true
}

func isOnboardingGuidedDirectoryGateStep(stepID string) bool {
	switch stepID {
	case "capture-cwd",
		"check-existing-project",
		"existing-project-required",
		"require-existing-project",
		"confirm-cwd":
		return true
	default:
		return false
	}
}

func sameCleanPath(a, b string) bool {
	if abs, err := filepath.Abs(a); err == nil {
		a = abs
	}
	if abs, err := filepath.Abs(b); err == nil {
		b = abs
	}
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func inferOnboardingResumeStepFromAudit(sessionDir string) string {
	data, err := os.ReadFile(filepath.Join(sessionDir, "audit.log")) // #nosec G304 -- session dir from internal run state.
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ev, err := runview.ParseLine(strings.TrimSpace(lines[i]))
		if err != nil || ev.Type != string(audit.EventRunStart) {
			continue
		}
		if from, ok := ev.Data["resume_from"].(string); ok && strings.TrimSpace(from) != "" {
			return strings.TrimSpace(from)
		}
	}
	return ""
}

func firstWorkflowStepID(ref string) string {
	workflow, err := loader.LoadWorkflow(ref, loader.Options{})
	if err != nil || len(workflow.Steps) == 0 {
		return ""
	}
	return workflow.Steps[0].ID
}

func runStateCurrentStepID(state *model.RunState) string {
	if state == nil {
		return ""
	}
	if state.CurrentStep.Nested != nil {
		return nestedStepLeafID(state.CurrentStep.Nested)
	}
	return state.CurrentStep.StepID
}

func nestedStepLeafID(n *model.NestedStepState) string {
	if n == nil {
		return ""
	}
	if n.Child != nil {
		return nestedStepLeafID(n.Child)
	}
	return n.StepID
}

func newOnboardingSessionDir(workflowName string) (string, error) {
	runsDir, err := onboardingRunsDir()
	if err != nil {
		return "", err
	}
	safeName := audit.SanitizeWorkflowName(workflowName)
	timestamp := strings.NewReplacer(":", "-", ".", "-").Replace(time.Now().UTC().Format(time.RFC3339Nano))
	return filepath.Join(runsDir, safeName+"-"+timestamp), nil
}

func onboardingRunsDir() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".agent-runner", "onboarding", "runs"), nil
}

func resetOnboardingState() error {
	settings, err := usersettings.Load()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	settings.Onboarding = usersettings.OnboardingSettings{}
	if err := usersettings.Save(settings); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}
	if err := removeAllWritable(filepath.Join(cwd, ".validator")); err != nil {
		return fmt.Errorf("remove project .validator: %w", err)
	}

	runsDir, err := onboardingRunsDir()
	if err != nil {
		return err
	}
	if err := removeAllWritable(runsDir); err != nil {
		return fmt.Errorf("remove onboarding runs: %w", err)
	}
	return nil
}

func removeAllWritable(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return os.Remove(path)
	}
	if !info.IsDir() {
		if err := os.Chmod(path, info.Mode().Perm()|0o600); err != nil {
			return err
		}
		return os.Remove(path)
	}

	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}

	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		mode := info.Mode().Perm()
		if entry.IsDir() {
			return root.Chmod(name, mode|0o700)
		}
		return root.Chmod(name, mode|0o600)
	})
	if err != nil {
		_ = root.Close()
		return err
	}
	if err := root.Close(); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func isTopLevelOnboardingWorkflow(workflowFile string) bool {
	ref, err := builtinworkflows.Resolve("onboarding:onboarding")
	return err == nil && workflowFile == ref
}

func (m *onboardingDemoPromptFlow) Init() tea.Cmd {
	if m.preparingRun {
		return tea.Batch(m.tickOnboardingDemoLoading(), m.prepareOnboardingDemoRun())
	}
	if m.prompt == nil {
		return nil
	}
	return m.prompt.Init()
}

func (m *onboardingDemoPromptFlow) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.termWidth = size.Width
		m.termHeight = size.Height
	}
	switch msg := msg.(type) {
	case runview.ExitMsg:
		if msg.UserRequested {
			m.exitRequested = true
		}
		return m, tea.Quit
	case runview.ResumeMsg:
		m.exitRequested = true
		return m, tea.Quit
	case runview.ResumeListMsg:
		return m, tea.Quit
	case onboardingDemoLoadingTick:
		if !m.preparingRun {
			return m, nil
		}
		m.loadingPhase++
		cmd := m.tickOnboardingDemoLoading()
		return m, cmd
	case onboardingDemoPrepareMsg:
		m.preparingRun = false
		m.loadingPhase = 0
		if msg.exitCode != 0 {
			m.exitCode = msg.exitCode
			return m, tea.Quit
		}
		return m.startPreparedRun(msg.handle)
	}

	if m.mode == onboardingDemoRunMode {
		return m.updateRun(msg)
	}
	return m.updatePrompt(msg)
}

func (m *onboardingDemoPromptFlow) updatePrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.prompt == nil {
		return m, nil
	}
	next, cmd := m.prompt.Update(msg)
	if prompt, ok := next.(*nativesetup.Model); ok {
		m.prompt = prompt
	}
	if !m.prompt.Done() {
		return m, cmd
	}
	switch m.prompt.Result() {
	case nativesetup.ResultDemo:
		m.preparingRun = true
		m.loadingPhase = 0
		return m, tea.Batch(m.tickOnboardingDemoLoading(), m.prepareOnboardingDemoRun())
	case nativesetup.ResultFailed:
		m.exitCode = 1
		return m, tea.Quit
	case nativesetup.ResultExitRequested:
		m.exitRequested = true
		return m, tea.Quit
	case nativesetup.ResultCompleted, nativesetup.ResultCancelled:
		return m, tea.Quit
	default:
		return m, cmd
	}
}

func (m *onboardingDemoPromptFlow) tickOnboardingDemoLoading() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return onboardingDemoLoadingTick{}
	})
}

func (m *onboardingDemoPromptFlow) prepareOnboardingDemoRun() tea.Cmd {
	ref := m.ref
	from := m.from
	return func() tea.Msg {
		handle, exitCode := prepareBuiltinOnboardingRun(ref, from)
		return onboardingDemoPrepareMsg{handle: handle, exitCode: exitCode}
	}
}

func (m *onboardingDemoPromptFlow) startPreparedRun(handle *runner.RunHandle) (tea.Model, tea.Cmd) {
	m.handle = handle
	rv, err := runview.New(m.handle.SessionDir, m.handle.ProjectDir, runview.FromLiveRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		m.exitCode = 1
		return m, tea.Quit
	}
	rv.StartInAltScreen()
	m.run = rv
	m.mode = onboardingDemoRunMode
	m.resultCh = make(chan runner.WorkflowResult, 1)
	m.startRunner()
	cmds := []tea.Cmd{rv.Init()}
	if m.termWidth > 0 && m.termHeight > 0 {
		w, h := m.termWidth, m.termHeight
		cmds = append(cmds, func() tea.Msg {
			return tea.WindowSizeMsg{Width: w, Height: h}
		})
	}
	return m, tea.Batch(cmds...)
}

func (m *onboardingDemoPromptFlow) startRunner() {
	if m.started {
		return
	}
	m.started = true
	coord := liveRunCoordinatorFactory(m.program, m.handle.SessionDir)
	go func() {
		result := runner.ResultFailed
		var runErr error
		defer func() {
			if rec := recover(); rec != nil {
				_ = coord.NotifyDone(string(runner.ResultFailed), fmt.Errorf("panic: %v", rec))
				m.resultCh <- runner.ResultFailed
				return
			}
			notifyErr := coord.NotifyDone(string(result), runErr)
			if notifyErr != nil && result == runner.ResultSuccess {
				m.resultCh <- runner.ResultFailed
			} else {
				m.resultCh <- result
			}
			if m.opts.quitOnDone {
				m.program.Send(runview.ExitMsg{})
			}
		}()
		result = runner.ExecuteFromHandle(m.handle, &runner.Options{
			ProcessRunner:   coord.TUIProcessRunner(&realProcessRunner{}),
			GlobExpander:    &realGlobExpander{},
			Log:             &runner.DiscardLogger{},
			SuspendHook:     coord.BeforeInteractive,
			ResumeHook:      coord.AfterInteractive,
			PrepareStepHook: coord.PrepareForStep,
			UIStepHandler:   coord.HandleUIStep,
		})
	}()
}

func (m *onboardingDemoPromptFlow) updateRun(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.run == nil {
		return m, nil
	}
	next, cmd := m.run.Update(msg)
	if rv, ok := next.(*runview.Model); ok {
		m.run = rv
		if rv.ExitRequested() {
			m.exitRequested = true
		}
	}
	return m, cmd
}

func (m *onboardingDemoPromptFlow) View() string {
	if m.mode == onboardingDemoRunMode {
		if m.run == nil {
			return ""
		}
		return m.run.View()
	}
	if m.preparingRun {
		return renderOnboardingDemoPreparing(m.termWidth, m.termHeight, m.loadingPhase)
	}
	if m.prompt == nil {
		return ""
	}
	return m.prompt.View()
}

func renderOnboardingDemoPreparing(width, height int, phase float64) string {
	content := tuistyle.LabelStyle.Bold(true).Render("Preparing Onboarding Demo") +
		"\n\n" +
		tuistyle.DimStyle.Render(tuistyle.SpinnerGlyph(phase)+" Setting up the demo workflow. This can take a moment.")
	if width >= 40 && height >= 8 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
	}
	return content
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
	return matchParamsForLaunch(workflow, positional, keyed, false)
}

// matchParamsForLaunch keeps the internal implicit repository target out of
// both positional argument accounting and required-parameter prompts. A
// configured workspace deliberately receives no substitution.
func matchParamsForLaunch(workflow *model.Workflow, positional []string, keyed map[string]string, implicitRepository bool) (map[string]string, error) {
	result := make(map[string]string)
	params := workflow.Params
	if implicitRepository && workflow.Scope == model.ScopeRepositories {
		params = make([]model.Param, 0, len(workflow.Params)-1)
		for _, param := range workflow.Params {
			if param.Name == model.RepositoriesParam {
				result[model.RepositoriesParam] = "default"
				continue
			}
			params = append(params, param)
		}
	}

	// Apply positional arguments to workflow params in order.
	if len(positional) > len(params) {
		return nil, fmt.Errorf("too many arguments: expected %d, got %d", len(params), len(positional))
	}

	for i, val := range positional {
		result[params[i].Name] = val
	}

	// Apply key=value overrides.
	for key, val := range keyed {
		found := false
		for _, p := range params {
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
	for _, p := range params {
		required := p.Required == nil || *p.Required
		if required {
			if _, ok := result[p.Name]; !ok {
				return nil, fmt.Errorf("missing required parameter: %q", p.Name)
			}
		}
	}

	return result, nil
}
