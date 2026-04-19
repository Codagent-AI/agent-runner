package listview

import (
	"fmt"

	"github.com/codagent/agent-runner/internal/runs"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

// Shared helpers are re-exposed under their pre-extraction names so the list
// TUI continues to read unchanged.
var (
	fitCell      = tuistyle.FitCell
	fitCellLeft  = tuistyle.FitCellLeft
	adjustOffset = tuistyle.AdjustOffset
	shortenPath  = tuistyle.ShortenPath
	formatTime   = tuistyle.FormatTime
	sanitize     = tuistyle.Sanitize
)

func runSummary(runList []runs.RunInfo) string {
	total := len(runList)
	if total == 0 {
		return "no runs"
	}

	active := 0
	for i := range runList {
		if runList[i].Status == runs.StatusActive {
			active++
		}
	}

	label := "runs"
	if total == 1 {
		label = "run"
	}

	if active > 0 {
		return fmt.Sprintf("%d %s  ● %d active", total, label, active)
	}
	return fmt.Sprintf("%d %s", total, label)
}
