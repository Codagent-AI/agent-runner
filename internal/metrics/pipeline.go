package metrics

import "github.com/codagent/agent-runner/internal/audit"

// Pipeline normalizes events through the collector before forwarding them to
// the optional downstream audit sink.
type Pipeline struct {
	collector *Collector
	sink      audit.EventLogger
}

func NewPipeline(collector *Collector, sink audit.EventLogger) *Pipeline {
	return &Pipeline{collector: collector, sink: sink}
}

func (p *Pipeline) Emit(event audit.Event) {
	event = p.collector.Process(event)
	if p.sink != nil {
		p.sink.Emit(event)
	}
}
