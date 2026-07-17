package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const durabilityPollInterval = 20 * time.Millisecond

func (a *ClaudeAdapter) Checkpoint(sessionID string) (Checkpoint, error) {
	path, err := claudeSessionPath(sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	return fileCheckpoint(path)
}

func (a *ClaudeAdapter) WaitForCommittedTurn(ctx context.Context, sessionID string, after Checkpoint) error {
	path, err := claudeSessionPath(sessionID)
	if err != nil {
		return err
	}
	return waitForSemanticRecord(ctx, after, path, func(line []byte) bool {
		var record struct {
			Type    string `json:"type"`
			Message struct {
				Role       string `json:"role"`
				StopReason string `json:"stop_reason"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &record) != nil {
			return false
		}
		return record.Type == "assistant" && record.Message.Role == "assistant" &&
			record.Message.StopReason != "" && record.Message.StopReason != "tool_use" &&
			record.Message.StopReason != "pause_turn"
	})
}

func (a *CodexAdapter) Checkpoint(sessionID string) (Checkpoint, error) {
	path, err := codexSessionPath(sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	return fileCheckpoint(path)
}

func (a *CodexAdapter) WaitForCommittedTurn(ctx context.Context, sessionID string, after Checkpoint) error {
	path, err := codexSessionPath(sessionID)
	if err != nil {
		return err
	}
	return waitForSemanticRecord(ctx, after, path, func(line []byte) bool {
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
			} `json:"payload"`
		}
		return json.Unmarshal(line, &record) == nil && record.Type == "event_msg" && record.Payload.Type == "task_complete"
	})
}

func (a *CopilotAdapter) Checkpoint(sessionID string) (Checkpoint, error) {
	path, err := copilotEventsPath(sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	return fileCheckpoint(path)
}

func (a *CopilotAdapter) WaitForCommittedTurn(ctx context.Context, sessionID string, after Checkpoint) error {
	path, err := copilotEventsPath(sessionID)
	if err != nil {
		return err
	}
	return waitForSemanticRecord(ctx, after, path, func(line []byte) bool {
		var record struct {
			Type string `json:"type"`
			Data struct {
				TurnID any `json:"turnId"`
			} `json:"data"`
		}
		return json.Unmarshal(line, &record) == nil && record.Type == "assistant.turn_end" && record.Data.TurnID != nil
	})
}

func (a *CursorAdapter) Checkpoint(sessionID string) (Checkpoint, error) {
	path, err := cursorStorePath(sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- resolved beneath Cursor's native chat store
	if err != nil {
		return Checkpoint{}, fmt.Errorf("read Cursor chat store %s: %w", path, err)
	}
	return Checkpoint{Artifact: path, Marker: strings.Join(cursorAssistantDigests(data), "\n")}, nil
}

func (a *CursorAdapter) WaitForCommittedTurn(ctx context.Context, sessionID string, after Checkpoint) error {
	path, err := cursorStorePath(sessionID)
	if err != nil {
		return err
	}
	if err := validateCheckpoint(after, path); err != nil {
		return err
	}
	baseline := stringSet(after.Marker)
	return pollForCommittedTurn(ctx, path, func() (bool, error) {
		data, err := os.ReadFile(path) // #nosec G304 -- resolved beneath Cursor's native chat store
		if err != nil {
			return false, err
		}
		for _, digest := range cursorAssistantDigests(data) {
			if _, ok := baseline[digest]; !ok {
				return true, nil
			}
		}
		return false, nil
	})
}

func (a *OpenCodeAdapter) Checkpoint(sessionID string) (Checkpoint, error) {
	if err := validateSessionID(sessionID); err != nil {
		return Checkpoint{}, err
	}
	record, err := a.latestCompletedOpenCodeAssistant(sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	return Checkpoint{Artifact: "opencode db message table for session " + sessionID, Marker: record.marker()}, nil
}

func (a *OpenCodeAdapter) WaitForCommittedTurn(ctx context.Context, sessionID string, after Checkpoint) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	wantArtifact := "opencode db message table for session " + sessionID
	if err := validateCheckpoint(after, wantArtifact); err != nil {
		return err
	}
	return pollForCommittedTurn(ctx, after.Artifact, func() (bool, error) {
		record, err := a.latestCompletedOpenCodeAssistant(sessionID)
		if err != nil {
			return false, err
		}
		return record.marker() != "" && record.marker() != after.Marker, nil
	})
}

type openCodeCompletedMessage struct {
	ID        string `json:"id"`
	Completed int64  `json:"completed"`
	Finish    string `json:"finish"`
}

func (r openCodeCompletedMessage) marker() string {
	if r.ID == "" || r.Completed == 0 || r.Finish == "" || r.Finish == "tool-calls" {
		return ""
	}
	return r.ID + ":" + strconv.FormatInt(r.Completed, 10)
}

func (a *OpenCodeAdapter) latestCompletedOpenCodeAssistant(sessionID string) (openCodeCompletedMessage, error) {
	escapedID := strings.ReplaceAll(sessionID, "'", "''")
	query := "SELECT id, json_extract(data, '$.time.completed') AS completed, " +
		"json_extract(data, '$.finish') AS finish FROM message " +
		"WHERE session_id = '" + escapedID + "' AND json_extract(data, '$.role') = 'assistant' " +
		"AND json_extract(data, '$.time.completed') IS NOT NULL ORDER BY completed DESC LIMIT 1"
	output, err := a.queryOpenCodeDB(query)
	if err != nil {
		return openCodeCompletedMessage{}, fmt.Errorf("query OpenCode message store: %w", err)
	}
	var records []openCodeCompletedMessage
	if err := json.Unmarshal(output, &records); err != nil {
		return openCodeCompletedMessage{}, fmt.Errorf("decode OpenCode message store: %w", err)
	}
	if len(records) == 0 {
		return openCodeCompletedMessage{}, nil
	}
	return records[0], nil
}

func (a *OpenCodeAdapter) queryOpenCodeDB(query string) ([]byte, error) {
	if a.runDBQuery != nil {
		return a.runDBQuery(query)
	}
	return exec.Command("opencode", "db", query, "--format", "json").Output() // #nosec G204 -- fixed executable; query is one argv value
}

func claudeSessionPath(sessionID string) (string, error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	pattern := filepath.Join(home, ".claude", "projects", "*", sessionID+".jsonl")
	paths, err := filepath.Glob(pattern)
	return exactlyOneSessionPath("Claude", sessionID, paths, err)
}

func codexSessionPath(sessionID string) (string, error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, ".codex", "sessions")
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		if normalizeCodexSessionID(strings.TrimSuffix(entry.Name(), ".jsonl")) == sessionID || codexSessionID(path) == sessionID {
			paths = append(paths, path)
		}
		return nil
	})
	return exactlyOneSessionPath("Codex", sessionID, paths, err)
}

func copilotEventsPath(sessionID string) (string, error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".copilot", "session-state", sessionID, "events.jsonl")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("locate Copilot session %q: %w", sessionID, err)
	}
	return path, nil
}

func cursorStorePath(sessionID string) (string, error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	pattern := filepath.Join(home, ".cursor", "chats", "*", sessionID, "store.db")
	paths, err := filepath.Glob(pattern)
	return exactlyOneSessionPath("Cursor", sessionID, paths, err)
}

func exactlyOneSessionPath(cliName, sessionID string, paths []string, err error) (string, error) {
	if err != nil {
		return "", fmt.Errorf("locate %s session %q: %w", cliName, sessionID, err)
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("locate %s session %q: native session store not found", cliName, sessionID)
	}
	if len(paths) > 1 {
		return "", fmt.Errorf("locate %s session %q: %d native session stores found", cliName, sessionID, len(paths))
	}
	return paths[0], nil
}

func validateSessionID(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session ID is required for durability confirmation")
	}
	if filepath.Base(sessionID) != sessionID || sessionID == "." || sessionID == ".." {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	return nil
}

func fileCheckpoint(path string) (Checkpoint, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("checkpoint native session store %s: %w", path, err)
	}
	if info.IsDir() {
		return Checkpoint{}, fmt.Errorf("checkpoint native session store %s: is a directory", path)
	}
	return Checkpoint{Artifact: path, Offset: info.Size()}, nil
}

func waitForSemanticRecord(ctx context.Context, after Checkpoint, path string, semantic func([]byte) bool) error {
	if err := validateCheckpoint(after, path); err != nil {
		return err
	}
	return pollForCommittedTurn(ctx, path, func() (bool, error) {
		file, err := os.Open(path) // #nosec G304 -- adapter resolved the native session path
		if err != nil {
			return false, err
		}
		defer func() { _ = file.Close() }()
		if _, err := file.Seek(after.Offset, io.SeekStart); err != nil {
			return false, err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			if semantic(scanner.Bytes()) {
				return true, nil
			}
		}
		return false, scanner.Err()
	})
}

func validateCheckpoint(after Checkpoint, artifact string) error {
	if after.Artifact == "" {
		return fmt.Errorf("durability checkpoint has no inspected artifact")
	}
	if after.Artifact != artifact {
		return fmt.Errorf("durability checkpoint artifact changed from %q to %q", after.Artifact, artifact)
	}
	return nil
}

func pollForCommittedTurn(ctx context.Context, artifact string, inspect func() (bool, error)) error {
	ticker := time.NewTicker(durabilityPollInterval)
	defer ticker.Stop()
	for {
		committed, err := inspect()
		if err != nil {
			return fmt.Errorf("inspect committed turn in %s: %w", artifact, err)
		}
		if committed {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for committed turn in %s: %w", artifact, ctx.Err())
		case <-ticker.C:
		}
	}
}

func cursorAssistantDigests(data []byte) []string {
	needle := []byte(`{"role":"assistant"`)
	var digests []string
	for start := 0; ; {
		index := bytes.Index(data[start:], needle)
		if index < 0 {
			break
		}
		index += start
		decoder := json.NewDecoder(bytes.NewReader(data[index:]))
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err == nil {
			var message struct {
				Role    string            `json:"role"`
				Content []json.RawMessage `json:"content"`
			}
			if json.Unmarshal(raw, &message) == nil && message.Role == "assistant" && len(message.Content) > 0 {
				compact := &bytes.Buffer{}
				if json.Compact(compact, raw) == nil {
					sum := sha256.Sum256(compact.Bytes())
					digests = append(digests, hex.EncodeToString(sum[:]))
				}
			}
		}
		start = index + len(needle)
	}
	sort.Strings(digests)
	return digests
}

func stringSet(joined string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, value := range strings.Split(joined, "\n") {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}
