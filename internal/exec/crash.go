package exec

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codagent/agent-runner/internal/model"
)

const stderrMarker = "\n[... stderr truncated ...]\n"

func boundStderr(stderr string) string {
	if len(stderr) <= 4096 {
		return stderr
	}
	remaining := 4096 - len(stderrMarker)
	headEnd := remaining / 2
	for headEnd > 0 && !utf8.RuneStart(stderr[headEnd]) {
		headEnd--
	}
	tailStart := len(stderr) - (remaining - remaining/2)
	for tailStart < len(stderr) && !utf8.RuneStart(stderr[tailStart]) {
		tailStart++
	}
	return stderr[:headEnd] + stderrMarker + stderr[tailStart:]
}

func recordAgentCrash(ctx *model.ExecutionContext, step *model.Step, prefix string, attempt int, invocation *AgentInvocationResult, runErr error) {
	record := model.CrashRecord{StepID: step.ID, Prefix: prefix, Path: stepPath(ctx, step), Attempt: attempt, ExecutionSessionID: ctx.ExecutionSessionID}
	if invocation != nil {
		record.Stderr = boundStderr(invocation.Stderr)
		if invocation.CrashError != "" {
			record.Error = invocation.CrashError
		}
		if invocation.CLILaunched || invocation.ExitCode != 0 {
			exit := invocation.ExitCode
			record.ExitCode = &exit
		}
	}
	if runErr != nil {
		record.Error = runErr.Error()
	}
	ctx.Crashes.Add(&record)
	ctx.StepFailure = model.StepFailure{Kind: model.FailureInfrastructure, Origin: &record}
}

// ClassifyCrash formats the root reason for the crash that ended the run.
func ClassifyCrash(record *model.CrashRecord, repairAttempts int) string {
	if record == nil {
		return ""
	}
	name := record.StepID
	if name == "repair" && len(record.Path) >= 2 {
		name = record.Path[len(record.Path)-2].StepID + " repair"
	}
	message := firstCrashLine(record.Error)
	if message == "" {
		message = firstCrashLine(record.Stderr)
	}
	if message == "" && record.ExitCode != nil {
		message = "exit code " + strconv.Itoa(*record.ExitCode)
	}
	if message == "" {
		message = "agent session did not finish"
	}
	reason := name + " failed (infrastructure): " + message
	if repairAttempts > 0 {
		reason += fmt.Sprintf(" after %d repair attempts", repairAttempts)
	}
	return reason
}

func firstCrashLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
