//go:build !darwin && !linux

package interactive

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
)

const DefaultTerminationGrace = 3 * time.Second

// ProcessMetadata identifies a supervised child attempt for crash cleanup.
type ProcessMetadata struct {
	ChildPID  int    `json:"child_pid"`
	PGID      int    `json:"pgid"`
	StartTime string `json:"start_time"`
	Socket    string `json:"socket_path"`
}

type DirectOptions struct {
	Args             []string
	Env              []string
	DropEnv          []string
	Workdir          string
	StepID           string
	SessionID        string
	CLI              string
	Control          *ControlServer
	Probe            cli.TurnDurabilityProbe
	ResolveSessionID func() string
	Before           func() error
	After            func() error
	Stdin            io.Reader
	Stdout           io.Writer
	Stderr           io.Writer
	TTY              *os.File
	Foreground       bool

	DurabilityTimeout  time.Duration
	TerminationGrace   time.Duration
	WatchdogExecutable string
	Persist            func(*ProcessMetadata)
	Logger             audit.EventLogger
	Prefix             string
}

type DirectResult struct {
	ExitCode         int
	Completed        bool
	Started          bool
	DurabilityFailed bool
	DurabilityError  error
}

type DirectRunner struct{ options *DirectOptions }

func NewDirectRunner(options *DirectOptions) *DirectRunner { return &DirectRunner{options: options} }

func (r *DirectRunner) Run(context.Context) (DirectResult, error) {
	return DirectResult{}, interactivePlatformError()
}

type TerminalOptions struct {
	Args    []string
	Env     []string
	DropEnv []string
	Workdir string

	Before func() error
	After  func() error

	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	TTY        *os.File
	Foreground bool

	TerminationGrace   time.Duration
	WatchdogExecutable string
	Persist            func(*ProcessMetadata)
	Logger             audit.EventLogger
	Prefix             string
}

type TerminalResult struct {
	ExitCode int
}

func RunTerminal(context.Context, *TerminalOptions) (TerminalResult, error) {
	return TerminalResult{}, interactivePlatformError()
}

func RunWatchdog(io.Reader, ProcessMetadata, time.Duration) error {
	return interactivePlatformError()
}

func CleanupProcess(ProcessMetadata, time.Duration) error {
	return interactivePlatformError()
}
