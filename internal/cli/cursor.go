package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CursorAdapter constructs invocation args for the Cursor agent CLI.
type CursorAdapter struct{}

// BuildArgs constructs Cursor CLI args.
//
// Patterns:
//   - Fresh headless:      agent -p --output-format stream-json --force --trust [--model <m>] <prompt>
//   - Resume headless:     agent -p --output-format stream-json --force --trust --resume=<id> <prompt>
//   - Fresh interactive:   agent [--model <m>] <prompt>
//   - Resume interactive:  agent --resume=<id> <prompt>
//
// Cursor has no native system-prompt, effort, or disallowed-tools flags. Those
// inputs are intentionally ignored here. Interactive mode omits headless-only
// output/trust flags and --force because a human supervises permissions at the
// terminal. --model is omitted on resume because a resumed Cursor chat keeps the
// model it was started with.
func (a *CursorAdapter) BuildArgs(input *BuildArgsInput) []string {
	args := []string{"agent"}
	if input.Headless {
		args = append(args, "-p", "--output-format", "stream-json", "--force", "--trust")
	}

	if input.Resume && input.SessionID != "" {
		args = append(args, "--resume="+input.SessionID)
	} else if input.Model != "" {
		args = append(args, "--model", input.Model)
	}

	args = append(args, input.Prompt)
	return args
}

// SupportsSystemPrompt returns false — Cursor CLI has no native system prompt flag.
func (a *CursorAdapter) SupportsSystemPrompt() bool {
	return false
}

func (a *CursorAdapter) ProbeModel(model, effort string) (ProbeStrength, error) {
	return BinaryOnly, nil
}

// DiscoverSessionID returns the session ID after a Cursor process exits.
// Headless mode parses stream-json output for the first event containing a
// session_id field. Interactive mode scans Cursor's chat store by mtime.
func (a *CursorAdapter) DiscoverSessionID(opts *DiscoverOptions) string {
	if opts.PresetID != "" {
		return opts.PresetID
	}
	if !opts.Headless {
		return discoverCursorInteractiveSession(opts.SpawnTime, opts.Workdir)
	}
	return discoverCursorSessionID(opts.ProcessOutput)
}

// FilterOutput extracts the plain-text response from cursor's stream-json
// output by finding the result event's "result" field.
func (a *CursorAdapter) FilterOutput(stdout string) string {
	return extractCursorResult(stdout)
}

// WrapStdout returns a writer that parses cursor stream-json lines and
// forwards only assistant text content to downstream.
func (a *CursorAdapter) WrapStdout(downstream io.Writer) io.Writer {
	return &cursorStreamFilter{downstream: downstream}
}

// cursorStreamFilter is a line-buffering io.Writer that parses cursor
// stream-json JSONL and writes only assistant text to the downstream writer.
type cursorStreamFilter struct {
	downstream io.Writer
	buf        []byte
	lastLen    int // length of text written so far from flush events
	err        error
}

func (f *cursorStreamFilter) Write(p []byte) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	n := len(p)
	f.buf = append(f.buf, p...)

	for {
		idx := indexOf(f.buf, '\n')
		if idx < 0 {
			break
		}
		line := f.buf[:idx]
		f.buf = f.buf[idx+1:]
		if err := f.processLine(line); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (f *cursorStreamFilter) Close() error {
	if f.err != nil {
		return f.err
	}
	if len(f.buf) > 0 {
		if err := f.processLine(f.buf); err != nil {
			return err
		}
		f.buf = nil
	}
	return nil
}

func (f *cursorStreamFilter) processLine(line []byte) error {
	var event struct {
		Type    string `json:"type"`
		Result  string `json:"result"`
		Message *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &event) != nil {
		return nil
	}

	switch event.Type {
	case "assistant":
		if event.Message == nil {
			return nil
		}
		var textBuilder strings.Builder
		for _, c := range event.Message.Content {
			if c.Type == "text" {
				textBuilder.WriteString(c.Text)
			}
		}
		text := textBuilder.String()
		if len(text) > f.lastLen {
			if err := f.writeDownstream([]byte(text[f.lastLen:])); err != nil {
				return err
			}
			f.lastLen = len(text)
		}
	case "result":
		// Cursor's result is a superset of the final assistant text; lastLen avoids re-sending.
		if event.Result != "" && len(event.Result) > f.lastLen {
			if err := f.writeDownstream([]byte(event.Result[f.lastLen:])); err != nil {
				return err
			}
			f.lastLen = len(event.Result)
		}
	}
	return nil
}

func (f *cursorStreamFilter) writeDownstream(p []byte) error {
	n, err := f.downstream.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		f.err = err
	}
	return err
}

func indexOf(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

func extractCursorResult(output string) string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			Type   string `json:"type"`
			Result string `json:"result"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Type == "result" && event.Result != "" {
			return event.Result
		}
	}
	return ""
}

func discoverCursorSessionID(output string) string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.SessionID != "" {
			return event.SessionID
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("cursor: failed to scan cursor session output: %v", err)
		return ""
	}
	return ""
}

// discoverCursorInteractiveSession scans ~/.cursor/chats/*/<chat-uuid>/store.db
// files and returns the chat UUID when exactly one post-spawn store matches the
// effective workdir. If the evidence is ambiguous, it declines to persist a
// session ID rather than risking a resume into an unrelated chat.
func discoverCursorInteractiveSession(spawnTime time.Time, workdir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	cwd := workdir
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return ""
		}
	}

	chatsDir := filepath.Join(home, ".cursor", "chats")
	type candidate struct {
		id      string
		modTime time.Time
	}
	var candidates []candidate

	_ = filepath.WalkDir(chatsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Base(path) != "store.db" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().Before(spawnTime) {
			return nil
		}
		if !matchesCursorStoreWorkdir(path, cwd) {
			return nil
		}
		candidates = append(candidates, candidate{
			id:      filepath.Base(filepath.Dir(path)),
			modTime: info.ModTime(),
		})
		return nil
	})

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	if len(candidates) > 1 {
		log.Printf("cursor: %d interactive chat candidates match workdir %s; session ID is ambiguous and will not be persisted", len(candidates), cwd)
		return ""
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].id
}

func matchesCursorStoreWorkdir(storeDBPath, workdir string) bool {
	data, err := os.ReadFile(storeDBPath) // #nosec G304 -- scanning known Cursor chat store path
	if err != nil {
		return false
	}

	canonWorkdir := canonicalize(workdir)
	markers := [][]byte{
		[]byte("Workspace Path: " + workdir),
		[]byte("Workspace Path: " + canonWorkdir),
	}
	for _, marker := range markers {
		if bytes.Contains(data, marker) {
			return true
		}
	}
	return false
}
