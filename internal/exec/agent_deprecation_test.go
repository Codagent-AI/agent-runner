package exec

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/model"
)

type synchronizedDeprecationLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *synchronizedDeprecationLogger) Println(args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprint(args...))
}

func (l *synchronizedDeprecationLogger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *synchronizedDeprecationLogger) Errorf(format string, args ...any) {
	l.Printf(format, args...)
}

func (l *synchronizedDeprecationLogger) output() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func TestEmitAgentDeprecationsDeduplicatesConcurrentChildContexts(t *testing.T) {
	root := model.NewRootContext(&model.RootContextOptions{})
	children := []*model.ExecutionContext{
		model.NewLoopIterationContext(root, model.LoopIterationOptions{StepID: "loop", Iteration: 0}),
		model.NewSubWorkflowContext(root, &model.SubWorkflowContextOptions{StepID: "sub", WorkflowFile: "sub.yaml"}),
	}
	log := &synchronizedDeprecationLogger{}
	warning := []config.Deprecation{{Alias: "planner", Canonical: "lead"}}

	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 100; i++ {
		workers.Add(1)
		go func(ctx *model.ExecutionContext) {
			defer workers.Done()
			<-start
			EmitAgentDeprecations(ctx, log, warning)
		}(children[i%len(children)])
	}
	close(start)
	workers.Wait()

	const message = `agent profile "planner" is deprecated; use "lead"`
	if count := strings.Count(log.output(), message); count != 1 {
		t.Fatalf("deprecation warning count = %d, want 1; output=%q", count, log.output())
	}
}
