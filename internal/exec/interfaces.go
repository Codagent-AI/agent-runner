package exec

// StepOutcome represents the result of executing a step.
type StepOutcome string

// Step outcome constants.
const (
	OutcomeSuccess   StepOutcome = "success"
	OutcomeFailed    StepOutcome = "failed"
	OutcomeAborted   StepOutcome = "aborted"
	OutcomeExhausted StepOutcome = "exhausted"
)

// ProcessResult holds the outcome of a spawned process.
type ProcessResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// AgentProcess represents a running agent process that can be waited on or killed.
type AgentProcess interface {
	Wait() (ProcessResult, error)
	Kill() error
}

// ProcessRunner abstracts process spawning for testability.
type ProcessRunner interface {
	RunShell(cmd string, captureStdout bool) (ProcessResult, error)
	RunAgent(args []string) (ProcessResult, error)
	StartAgent(args []string) (AgentProcess, error)
}

// GlobExpander abstracts file globbing for testability.
type GlobExpander interface {
	Expand(pattern string) ([]string, error)
}

// Logger abstracts console output for testability.
type Logger interface {
	Println(args ...any)
	Printf(format string, args ...any)
	Errorf(format string, args ...any)
}

// LoopResult holds the outcome of a loop execution.
type LoopResult struct {
	Outcome       StepOutcome
	LastIteration int
}
