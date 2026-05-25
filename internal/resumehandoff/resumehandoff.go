package resumehandoff

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const markerFileName = "resume-target"

func MarkerPath(sessionDir string) string {
	return filepath.Join(sessionDir, markerFileName)
}

func Read(sessionDir string) (runID string, ok bool, err error) {
	data, err := os.ReadFile(MarkerPath(sessionDir)) // #nosec G304 -- marker path is derived from the current run session dir.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	runID = strings.TrimSpace(string(data))
	if runID == "" {
		return "", false, nil
	}
	return runID, true, nil
}
