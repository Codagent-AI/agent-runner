package exec

import (
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

const pullRequestCaptureName = "pr_url"

// recordPullRequestCapture observes the reserved PR capture without changing
// its ordinary captured-variable behavior. It deliberately never returns an
// error: a PR link is run metadata, not a step outcome.
func recordPullRequestCapture(ctx *model.ExecutionContext, stepID, name string, captured model.CapturedValue) {
	if name != pullRequestCaptureName {
		return
	}
	url, ok := captured.StringValue()
	if !ok {
		return
	}
	url = strings.TrimSpace(url)
	if url == "" || !pullRequestCaptureState(ctx).Mark(url) {
		return
	}
	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Prefix:    audit.BuildPrefix(nestingToAudit(ctx), stepID),
		Type:      audit.EventPullRequestRecorded,
		Data:      map[string]any{"url": url},
	})
}

func pullRequestCaptureState(ctx *model.ExecutionContext) *model.PullRequestCaptureState {
	if ctx.PullRequestCaptureState != nil {
		return ctx.PullRequestCaptureState
	}
	root := ctx
	for root.ParentContext != nil {
		root = root.ParentContext
	}
	if root.PullRequestCaptureState == nil {
		root.PullRequestCaptureState = model.NewPullRequestCaptureState()
	}
	ctx.PullRequestCaptureState = root.PullRequestCaptureState
	return ctx.PullRequestCaptureState
}
