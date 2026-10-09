package exec

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/agentcall"
	"github.com/codagent/agent-runner/internal/cli"
)

type settlementKind int

const (
	settledExited settlementKind = iota
	settledLaunchFailed
	settledTimedOut
	settledCanceled
	settledAborted
)

type callSettlement struct {
	kind     settlementKind
	at       time.Time
	exitCode *int
}

var errAgentCallTimedOut = errors.New("agent call timed out")
var errAgentCallCanceled = errors.New("agent call canceled")

func (h *AgentCallHandler) trySettle(r *acceptedAgentCall, kind settlementKind, exit *int) bool {
	h.mu.Lock()
	if r.settlement != nil {
		h.mu.Unlock()
		return false
	}
	var code *int
	if exit != nil {
		v := *exit
		code = &v
	}
	r.settlement = &callSettlement{kind: kind, at: h.options.Now(), exitCode: code}
	h.mu.Unlock()
	if h.options.afterSettle != nil {
		h.options.afterSettle()
	}
	return true
}

func (h *AgentCallHandler) applySettlement(r *acceptedAgentCall, c *resolvedAgentCall, e *agentCallExecution) {
	h.mu.Lock()
	s := *r.settlement
	h.mu.Unlock()
	code, message := "", ""
	switch s.kind {
	case settledTimedOut:
		code = agentcall.CodeTimedOut
		message = fmt.Sprintf("called agent exceeded timeout %s", *c.request.Timeout)
		e.invocation.Outcome = OutcomeFailed
	case settledCanceled, settledAborted:
		code = agentcall.CodeCallCanceled
		message = "called agent was canceled"
		e.invocation.Outcome = OutcomeAborted
	case settledExited:
		if *s.exitCode == 0 && e.invocation.Outcome == OutcomeSuccess {
			e.invocation.Outcome = OutcomeSuccess
			e.response = agentcall.Response{Result: &agentcall.Result{Target: c.target, Response: e.invocation.Response}}
			e.errorMessage = ""
		} else {
			e.invocation.Outcome = OutcomeFailed
			code = agentcall.CodeExecutionFailed
			message = fmt.Sprintf("called agent failed with exit code %d", *s.exitCode)
		}
	case settledLaunchFailed:
		e.invocation.Outcome = OutcomeFailed
		if e.response.Error == nil {
			code = agentcall.CodeExecutionFailed
			message = "called agent did not launch"
		}
	}
	if code != "" {
		e.response = acceptedFailure(r, code, message, c.target)
		e.errorMessage = message
	}
	e.invocation.FinishedAt = s.at
	e.invocation.Duration = s.at.Sub(r.started)
}

func appliesResumeModel(a cli.Adapter) bool {
	v, ok := a.(cli.ResumeModelApplier)
	return ok && v.AppliesResumeModel()
}

func (h *AgentCallHandler) resolveFollowUp(request *agentcall.Request) (*resolvedAgentCall, *agentcall.Error) {
	target := request.Target()
	fail := func(code, message string) (*resolvedAgentCall, *agentcall.Error) {
		return nil, callFailure(code, message, target)
	}
	h.mu.Lock()
	source := h.byCallID[target.Name]
	if source == nil {
		h.mu.Unlock()
		return fail(agentcall.CodeUnknownCall, "follow_up does not belong to this parent attempt")
	}
	if !agentcall.IsTerminalStatus(source.status) || source == h.active {
		elapsed := h.elapsedLocked(source)
		id := source.callID
		h.mu.Unlock()
		return nil, &agentcall.Error{Code: agentcall.CodeCallInProgress, Message: "follow_up source is still running", CallID: id, Elapsed: elapsed, Target: &target}
	}
	if source.target.Kind == agentcall.TargetSession {
		h.mu.Unlock()
		return fail(agentcall.CodeInvalidTarget, "named-session calls cannot be followed up; target the session by name")
	}
	if source.nativeSessionID == "" {
		h.mu.Unlock()
		return fail(agentcall.CodeNotResumable, "source has no native session")
	}
	c := *source.resolved
	c.sessionID = source.nativeSessionID
	c.knownModel = source.knownModel
	h.mu.Unlock()
	c.request = *request
	c.target = target
	c.resume = true
	profile := *c.profile
	c.profile = &profile
	if request.Model != nil {
		requested := strings.TrimSpace(*request.Model)
		if requested != c.model && !appliesResumeModel(c.adapter) {
			return fail(agentcall.CodeInvalidModel, "adapter does not apply model overrides on resume")
		}
		if _, err := c.adapter.ProbeModel(requested, c.profile.Effort); err != nil {
			return fail(agentcall.CodeInvalidModel, err.Error())
		}
		c.model = requested
		c.profile.Model = requested
	}
	if appliesResumeModel(c.adapter) {
		c.knownModel = c.model
	}
	if request.Workdir != nil {
		dir, err := resolveAgentCallWorkdir(h.options.Parent.Worktree, h.options.Parent.Workdir, *request.Workdir)
		if err != nil {
			return fail(agentcall.CodeInvalidWorkdir, err.Error())
		}
		c.workdir = dir
	}
	if c.cliName == h.options.Parent.CLI && c.sessionID == h.activeParentSessionID() {
		return fail(agentcall.CodeSelfSession, "follow_up references the parent's active session")
	}
	return &c, nil
}
