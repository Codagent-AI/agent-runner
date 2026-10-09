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
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
func (h *AgentCallHandler) git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agentCallGitTimeout)
	defer cancel()
	return h.options.Git(ctx, dir, args...)
}
func (h *AgentCallHandler) captureStartGit(dir string) agentcall.GitDelta {
	d := agentcall.GitDelta{State: "unavailable"}
	if _, err := h.git(dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			d.State = "not_git"
		}
		return d
	}
	head, err := h.git(dir, "rev-parse", "HEAD")
	if err == nil {
		d.State = "captured"
		d.StartHead = head
	}
	return d
}
func (h *AgentCallHandler) collectGit(r *acceptedAgentCall, c *resolvedAgentCall) agentcall.GitDelta {
	d := r.startGit
	if d.State == "" {
		d = h.captureStartGit(c.workdir)
	}
	if d.State != "captured" {
		return d
	}
	end, err := h.git(c.workdir, "rev-parse", "HEAD")
	if err != nil {
		d.State = "unavailable"
		return d
	}
	d.EndHead = end
	if _, err = h.git(c.workdir, "merge-base", "--is-ancestor", d.StartHead, end); err != nil {
		d.State = "unavailable"
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			d.State = "non_linear"
		} else {
			return d
		}
	}
	log, err := h.git(c.workdir, "log", "--format=%h%x00%s", "-n", "51", d.StartHead+".."+end)
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
