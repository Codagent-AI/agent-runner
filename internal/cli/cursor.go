package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CursorAdapter constructs invocation args for the Cursor agent CLI.
type CursorAdapter struct{}

// ExecutableName returns the Cursor agent CLI binary name. The adapter remains
// registered as "cursor" because that is the workflow-facing backend name.
func (a *CursorAdapter) ExecutableName() string {
	return "agent"
}

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
// session_id field. Interactive mode scans Cursor's local chat store for a
// single chat written after spawn for the current workspace.
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
	return newCursorStreamFilter(downstream)
}

type cursorStreamFilter struct {
	lineBufferedWriter
	lastLen int // length of text written so far from flush events
}

func newCursorStreamFilter(d io.Writer) *cursorStreamFilter {
	f := &cursorStreamFilter{}
	f.downstream = d
	f.onLine = f.processLine
	return f
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

func discoverCursorInteractiveSession(spawnTime time.Time, workdir string) string {
	if workdir == "" {
		return ""
	}
	root := filepath.Join(os.Getenv("HOME"), ".cursor", "chats")
	rootDir, err := os.OpenRoot(root)
	if err != nil {
		return ""
	}
	defer func() { _ = rootDir.Close() }()
	rootFS := rootDir.FS()

	workspaceNeedle := []byte("Workspace Path: " + workdir)
	var matches []string
	if err := fs.WalkDir(rootFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "store.db" {
			return nil
		}
		info, statErr := fs.Stat(rootFS, path)
		if statErr != nil || info.ModTime().Before(spawnTime) {
			return nil
		}
		data, readErr := fs.ReadFile(rootFS, path)
		if readErr != nil || !bytes.Contains(data, workspaceNeedle) {
			return nil
		}
		chatID := filepath.Base(filepath.Dir(path))
		if chatID != "" {
			matches = append(matches, chatID)
		}
		return nil
	}); err != nil {
		return ""
	}
	if len(matches) != 1 {
		return ""
	}
	return matches[0]
}
