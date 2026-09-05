package metrics

import (
	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

// Pipeline normalizes events through the collector before forwarding them to
// the optional downstream audit sink.
type Pipeline struct {
	collector          *Collector
	sink               audit.EventLogger
	executionSessionID string
	checkpoints        *audit.CheckpointLogger
}

func NewPipeline(collector *Collector, sink audit.EventLogger) *Pipeline {
	return &Pipeline{collector: collector, sink: sink}
}

// NewExecutionPipeline adds one durable Runner invocation identity to every
// event and records centralized Git evidence before it reaches durable sinks.
func NewExecutionPipeline(collector *Collector, sink audit.EventLogger, projectRoot, executionSessionID string) *Pipeline {
	return &Pipeline{
		collector: collector, sink: sink, executionSessionID: executionSessionID,
		checkpoints: audit.NewCheckpointLogger(nil, projectRoot, executionSessionID),
	}
}

func (p *Pipeline) Emit(event audit.Event) {
	if event.Data == nil {
		event.Data = map[string]any{}
	}
	if p.executionSessionID != "" {
		event.Data["execution_session_id"] = p.executionSessionID
	}
	if p.checkpoints != nil {
		event = p.checkpoints.Decorate(event)
	}
	if identity, ok := event.Data[DataIdentity].(model.ExecutionIdentity); ok && identity.ExecutionSessionID == "" {
		if executionSessionID, ok := event.Data["execution_session_id"].(string); ok {
			identity.ExecutionSessionID = executionSessionID
			event.Data[DataIdentity] = identity
		}
	}
	event = p.collector.Process(event)
	if p.sink != nil {
		p.sink.Emit(event)
	}
}
