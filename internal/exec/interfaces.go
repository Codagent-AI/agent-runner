package exec

import (
	"context"
	"io"
	"strings"
	"time"
)

// StepOutcome represents the result of executing a step.
type StepOutcome string

// Step outcome constants.
const (
	OutcomeSuccess   StepOutcome = "success"
	OutcomeFailed    StepOutcome = "failed"
	OutcomeAborted   StepOutcome = "aborted"
	OutcomeExhausted StepOutcome = "exhausted"
	OutcomeSkipped   StepOutcome = "skipped"
)

// ProcessResult holds the outcome of a spawned process.
type ProcessResult struct {
	Started  bool
	ExitCode int
	Stdout   string
	Stderr   string
}

// AgentProcessOptions contains all transient process state for one agent
// invocation. Process runners must not retain or mutate these values as
// current-step state: parent and called-agent invocations may overlap.
type AgentProcessOptions struct {
	Context       context.Context
	Args          []string
	CaptureStdout bool
	Env           []string
	DropEnv       []string
	Workdir       string
	Prefix        string
	StdoutWrapper func(io.Writer) io.Writer
	StderrWrapper func(io.Writer) io.Writer
	Supervision   AgentProcessSupervision
}

// AgentProcessSupervision describes invocation-scoped process cleanup.
type AgentProcessSupervision struct {
	ProcessGroup     bool
	TerminationGrace time.Duration
}

// BuildAgentEnvironment applies invocation-local removals and additions to a
// base environment. Additions replace inherited values with the same name.
func BuildAgentEnvironment(base, drop, additions []string) []string {
	removed := make(map[string]struct{}, len(drop)+len(additions))
	for _, name := range drop {
		removed[name] = struct{}{}
	}
	for _, entry := range additions {
		name, _, _ := strings.Cut(entry, "=")
		removed[name] = struct{}{}
	}
	result := make([]string, 0, len(base)+len(additions))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if _, skip := removed[name]; !skip {
			result = append(result, entry)
		}
	}
	return append(result, additions...)
}

// ProcessRunner abstracts process spawning for testability.
type ProcessRunner interface {
	RunShell(cmd string, captureStdout bool, workdir string) (ProcessResult, error)
	RunAgent(options *AgentProcessOptions) (ProcessResult, error)
	RunScript(path string, stdin []byte, captureStdout bool, workdir string) (ProcessResult, error)
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
