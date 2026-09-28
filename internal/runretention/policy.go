// Package runretention performs conservative, opportunistic cleanup of old runs.
package runretention

import (
	"time"

	"github.com/codagent/agent-runner/internal/usersettings"
)

type Policy struct {
	Enabled           bool
	MaxAge            time.Duration
	ResumableMaxAge   time.Duration
	MaxRunsPerProject int
}

func PolicyFromSettings(s usersettings.RunRetention) Policy {
	p := Policy{Enabled: true, MaxAge: 30 * 24 * time.Hour, ResumableMaxAge: 90 * 24 * time.Hour, MaxRunsPerProject: 100}
	if s.Enabled != nil {
		p.Enabled = *s.Enabled
	}
	if s.MaxAgeDays != nil {
		p.MaxAge = time.Duration(*s.MaxAgeDays) * 24 * time.Hour
	}
	if s.ResumableMaxAgeDays != nil {
		p.ResumableMaxAge = time.Duration(*s.ResumableMaxAgeDays) * 24 * time.Hour
	}
	if s.MaxRunsPerProject != nil {
		p.MaxRunsPerProject = *s.MaxRunsPerProject
	}
	return p
}
