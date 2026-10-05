package externaluser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const SchemaVersion = 1

type Identity struct {
	StepKey string `json:"step_key"`
	Attempt int    `json:"attempt"`
	Turn    int    `json:"turn"`
}

func (id Identity) Stem() string        { return fmt.Sprintf("%s-%d-%d", id.StepKey, id.Attempt, id.Turn) }
func (id Identity) RequestName() string { return id.Stem() + ".request.json" }
func (id Identity) ReplyName() string   { return id.Stem() + ".reply.json" }

func StepKey(path string) string {
	var b strings.Builder
	for _, r := range path {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune(".-", r) {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, "_%x_", r)
		}
	}
	return b.String()
}

type Request struct {
	SchemaVersion int    `json:"schema_version"`
	RunID         string `json:"run_id"`
	Step          string `json:"step"`
	StepID        string `json:"step_id"`
	Attempt       int    `json:"attempt"`
	Turn          int    `json:"turn"`
	CLI           string `json:"cli"`
	SessionID     string `json:"session_id"`
	AgentMessage  string `json:"agent_message"`
	EmptyTurn     bool   `json:"empty_turn"`
}

type Reply struct {
	SchemaVersion int    `json:"schema_version"`
	Text          string `json:"text,omitempty"`
	Action        string `json:"action,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (r Reply) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", r.SchemaVersion)
	}
	if r.Action == "abort" {
		return nil
	}
	if r.Action != "" {
		return fmt.Errorf("unknown action %q", r.Action)
	}
	if r.Text == "" {
		return fmt.Errorf("text must be non-empty")
	}
	return nil
}

func AtomicWrite(path string, body []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".exchange-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func WaitReply(ctx context.Context, path string, timeout time.Duration) (Reply, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return Reply{}, fmt.Errorf("waiting for reply %q: %w", path, err)
		}
		body, err := os.ReadFile(path)
		if err == nil {
			return decodeReply(path, body)
		}
		if !os.IsNotExist(err) {
			return Reply{}, fmt.Errorf("reply %q: %w", path, err)
		}
		select {
		case <-ctx.Done():
			if body, err := os.ReadFile(path); ctx.Err() == context.DeadlineExceeded && err == nil {
				return decodeReply(path, body)
			}
			return Reply{}, fmt.Errorf("waiting for reply %q: %w", path, ctx.Err())
		case <-ticker.C:
		}
	}
}

func decodeReply(path string, body []byte) (Reply, error) {
	var reply Reply
	if err := json.Unmarshal(body, &reply); err != nil {
		return reply, fmt.Errorf("reply %q: %w", path, err)
	}
	if err := reply.Validate(); err != nil {
		return reply, fmt.Errorf("reply %q: %w", path, err)
	}
	return reply, nil
}
