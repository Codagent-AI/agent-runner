package exec

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/agentcall"
)

const agentCallGitTimeout = 5 * time.Second

func defaultAgentCallGit(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output() // #nosec G204 -- fixed executable; dir is the resolved workdir and args are Runner-built git operations/observed HEADs, passed without a shell.
	return strings.TrimSpace(string(out)), err
}

// captureStartGit shares one agentCallGitTimeout budget across its commands
// and stops early when ctx (the call's context) is canceled.
func (h *AgentCallHandler) captureStartGit(ctx context.Context, dir string) agentcall.GitDelta {
	ctx, cancel := context.WithTimeout(ctx, agentCallGitTimeout)
	defer cancel()
	d := agentcall.GitDelta{State: "unavailable"}
	if _, err := h.options.Git(ctx, dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			d.State = "not_git"
		}
		return d
	}
	head, err := h.options.Git(ctx, dir, "rev-parse", "HEAD")
	if err == nil {
		d.State = "captured"
		d.StartHead = head
	}
	return d
}

// collectGit runs after settlement, when the call's context may already be
// canceled, so it bounds all of its commands by one shared timeout instead.
func (h *AgentCallHandler) collectGit(r *acceptedAgentCall, c *resolvedAgentCall) agentcall.GitDelta {
	ctx, cancel := context.WithTimeout(context.Background(), agentCallGitTimeout)
	defer cancel()
	d := r.startGit
	if d.State == "" {
		d = h.captureStartGit(ctx, c.workdir)
	}
	if d.State != "captured" {
		return d
	}
	end, err := h.options.Git(ctx, c.workdir, "rev-parse", "HEAD")
	if err != nil {
		d.State = "unavailable"
		return d
	}
	d.EndHead = end
	if _, err = h.options.Git(ctx, c.workdir, "merge-base", "--is-ancestor", d.StartHead, end); err != nil {
		d.State = "unavailable"
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			d.State = "non_linear"
		} else {
			return d
		}
	}
	log, err := h.options.Git(ctx, c.workdir, "log", "--format=%h%x00%s", "-n", "51", d.StartHead+".."+end)
	if err != nil {
		d.State = "unavailable"
		return d
	}
	if log != "" {
		for _, line := range strings.Split(log, "\n") {
			sha, subject, ok := strings.Cut(line, "\x00")
			if !ok {
				d.State = "unavailable"
				d.Commits = nil
				return d
			}
			if len(d.Commits) == 50 {
				d.Truncated = true
				break
			}
			runes := []rune(subject)
			if len(runes) > 200 {
				subject = string(runes[:200])
			}
			d.Commits = append(d.Commits, agentcall.GitCommit{SHA: sha, Subject: subject})
		}
	}
	return d
}
func (h *AgentCallHandler) buildDetails(r *acceptedAgentCall, c *resolvedAgentCall) *agentcall.Details {
	h.mu.Lock()
	s := *r.settlement
	native := r.nativeSessionID
	h.mu.Unlock()
	d := &agentcall.Details{Exit: "terminated", Duration: max(s.at.Sub(r.started).Round(time.Second), 0).String(), Session: agentcall.SessionDetails{CLI: c.cliName, Model: c.knownModel, Resumed: c.resume, FollowUp: c.target.Kind != agentcall.TargetSession && native != ""}, Git: h.collectGit(r, c)}
	switch s.kind {
	case settledExited:
		d.Exit = "exited"
		d.ExitCode = s.exitCode
	case settledLaunchFailed:
		d.Exit = "not_launched"
	}
	return d
}
