package externaluser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type Event struct {
	Timestamp         string   `json:"timestamp"`
	Type              string   `json:"type"`
	Identity          Identity `json:"identity"`
	Request           *Request `json:"request,omitempty"`
	Reply             *Reply   `json:"reply,omitempty"`
	CompletionCommand string   `json:"completion_command,omitempty"`
}

func RecordPath(sessionDir string) string {
	return filepath.Join(sessionDir, "external-user", "exchanges.jsonl")
}
func Append(sessionDir string, event *Event) error {
	event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	path := RecordPath(sessionDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := truncateInterruptedRecord(f); err != nil {
		return err
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(body, '\n')); err != nil {
		return err
	}
	return f.Sync()
}
func Last(sessionDir, stepKey string) (Event, error) {
	f, err := os.Open(RecordPath(sessionDir))
	if os.IsNotExist(err) {
		return Event{}, nil
	}
	if err != nil {
		return Event{}, err
	}
	defer func() { _ = f.Close() }()
	var last Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 8*1024*1024)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if end := bytes.IndexByte(data, '\n'); end >= 0 {
			return end + 1, data[:end], nil
		}
		if atEOF {
			return len(data), nil, nil
		}
		return 0, nil, nil
	})
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return Event{}, fmt.Errorf("read exchange record: %w", err)
		}
		if event.Identity.StepKey == stepKey {
			last = event
		}
	}
	return last, scanner.Err()
}

// The run lock serializes writers. A crash can leave an unpublished partial
// last line, which is discarded before the next write-ahead event is appended.
func truncateInterruptedRecord(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	end := info.Size()
	buffer := make([]byte, 4096)
	for end > 0 {
		start := max(int64(0), end-int64(len(buffer)))
		chunk := buffer[:end-start]
		if _, err := f.ReadAt(chunk, start); err != nil && err != io.EOF {
			return err
		}
		if newline := bytes.LastIndexByte(chunk, '\n'); newline >= 0 {
			return f.Truncate(start + int64(newline) + 1)
		}
		end = start
	}
	return f.Truncate(0)
}
