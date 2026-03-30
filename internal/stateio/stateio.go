// Package stateio handles reading and writing workflow execution state files.
package stateio

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codagent/agent-runner/internal/model"
)

const stateFileName = "state.json"

// WriteState writes the run state to a JSON file in the given directory.
func WriteState(state *model.RunState, dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, stateFileName), data, 0o600)
}

// ReadState reads a run state from a JSON file.
func ReadState(filePath string) (model.RunState, error) {
	data, err := os.ReadFile(filePath) // #nosec G304 -- state file path is from internal state tracking
	if err != nil {
		if os.IsNotExist(err) {
			return model.RunState{}, fmt.Errorf("state file not found: %s", filePath)
		}
		return model.RunState{}, fmt.Errorf("read state: %w", err)
	}
	var state model.RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return model.RunState{}, fmt.Errorf("invalid state file (malformed JSON): %s", filePath)
	}
	return state, nil
}

// DeleteState removes the state file from the given directory.
func DeleteState(dir string) {
	_ = os.Remove(filepath.Join(dir, stateFileName))
}

// ComputeWorkflowHash returns a hex-encoded SHA-256 hash of the content.
func ComputeWorkflowHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:])
}
