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
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/usersettings"
)

// CursorAdapter constructs invocation args for the Cursor agent CLI.
type CursorAdapter struct {
	runStoreQuery           func(context.Context, string) ([]byte, error)
	prepareCompletionPlugin func(CompletionCommand) (string, error) // test seam; nil uses prepareCursorCompletionPlugin
}

const maxCursorStoreDBSize = 64 * 1024 * 1024

// ExecutableName returns the Cursor agent CLI binary name. The adapter remains
// registered as "cursor" because that is the workflow-facing backend name.
func (a *CursorAdapter) ExecutableName() string {
	return "agent"
}

// BuildArgs constructs Cursor CLI args.
//
// Patterns:
//   - Fresh headless:      agent -p --output-format stream-json --trust [--model <m>] <prompt>
//   - Resume headless:     agent -p --output-format stream-json --trust --resume=<id> <prompt>
//   - Fresh interactive:   agent [--model <m>] <prompt>
//   - Resume interactive:  agent --resume=<id> <prompt>
//
// Cursor has no native system-prompt, effort, or disallowed-tools flags. Those
// inputs are intentionally ignored here. Interactive mode omits headless-only
// output/trust flags because a human supervises permissions at the
// terminal. --model is omitted on resume because a resumed Cursor chat keeps the
// model it was started with.
//
// Autonomous contexts honor autonomous_permission_mode: yolo emits --force in
// both autonomous-headless and autonomous-interactive invocations. Cursor's
// Shell allowlist is prefix-matching, so no safe narrow pre-approval of the
// completion command exists; a conservative autonomous-interactive invocation
// would hang unattended at Cursor's approval prompt and fails early instead.
//
// BuildArgs exists only to satisfy the Adapter interface; callers must use
// BuildInvocationArgs so a construction failure surfaces before spawn.
func (a *CursorAdapter) BuildArgs(input *BuildArgsInput) []string {
	args, err := a.BuildArgsWithError(input)
	if err != nil {
		return nil
	}
	return args
}

// BuildArgsWithError constructs Cursor CLI args, failing when the completion
// plugin — Cursor's only completion integration — cannot be created, or when
// an autonomous-interactive invocation has no approval-free completion path.
func (a *CursorAdapter) BuildArgsWithError(input *BuildArgsInput) ([]string, error) {
	invocationContext := input.InvocationContext()
	mode := usersettings.EffectiveAutonomousPermissionMode(input.PermissionMode)
	if invocationContext == ContextAutonomousInteractive && mode != usersettings.PermissionModeYOLO {
		return nil, fmt.Errorf("cursor: unattended completion requires autonomous_permission_mode: yolo; Cursor's Shell allowlist is prefix-matching, so there is no safe narrow pre-approval for the completion command, and under conservative mode an autonomous-interactive step would hang at Cursor's approval prompt with nobody to answer it")
	}
	args := []string{"agent"}
	if invocationContext.IsHeadless() {
		args = append(args, "-p", "--output-format", "stream-json", "--trust")
	}
	if invocationContext.IsAutonomous() && mode == usersettings.PermissionModeYOLO {
		args = append(args, "--force")
	}

	if input.Resume && input.SessionID != "" {
		args = append(args, "--resume="+input.SessionID)
	} else if input.Model != "" {
		args = append(args, "--model", input.Model)
	}

	args = append(args, input.Prompt)
	if !invocationContext.IsHeadless() && input.CompletionCommand != nil && input.CompletionCommand.Valid() {
		prepare := a.prepareCompletionPlugin
		if prepare == nil {
			prepare = prepareCursorCompletionPlugin
		}
		pluginDir, err := prepare(*input.CompletionCommand)
		if err != nil {
			return nil, fmt.Errorf("cursor: create completion plugin: %w", err)
		}
		args = append([]string{"agent", "--plugin-dir", pluginDir}, args[1:]...)
	}
	return args, nil
}

func prepareCursorCompletionPlugin(command CompletionCommand) (string, error) {
	if !command.Valid() {
		return "", fmt.Errorf("invalid completion command")
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache: %w", err)
	}
	digest := sha256.Sum256([]byte("cursor-v2\x00" + command.ShellCommand()))
	pluginDir := filepath.Join(cacheDir, "agent-runner", "completion-plugins", "cursor-"+hex.EncodeToString(digest[:6]))
	files := map[string]string{
		filepath.Join(".cursor-plugin", "plugin.json"): `{
  "name": "agent-runner-completion",
  "version": "1.0.2",
	"description": "Agent Runner control-channel completion"
}
`,
		filepath.Join("commands", "next.md"): `---
description: Complete the current Agent Runner workflow step
---

Run this exact shell command now, then finish the current response:

` + "`" + command.ShellCommand() + "`" + `
`,
	}
	for relativePath, content := range files {
		path := filepath.Join(pluginDir, relativePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", fmt.Errorf("create Cursor completion plugin: %w", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return "", fmt.Errorf("write Cursor completion plugin: %w", err)
		}
	}
	return pluginDir, nil
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
		var err error
		workdir, err = os.Getwd()
		if err != nil {
			return ""
		}
	}
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return ""
	}
	workdir = filepath.Clean(absWorkdir)

	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return ""
	}
	root := filepath.Join(homeDir, ".cursor", "chats")
	rootDir, err := os.OpenRoot(root)
	if err != nil {
		return ""
	}
	defer func() { _ = rootDir.Close() }()
	rootFS := rootDir.FS()
	if sessionID := discoverCursorMetadataSession(rootFS, spawnTime, workdir); sessionID != "" {
		return sessionID
	}

	workspaceNeedle := []byte("Workspace Path: " + workdir)
	var matches []string
	if err := fs.WalkDir(rootFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "store.db" {
			return nil
		}
		info, statErr := fs.Stat(rootFS, path)
		if statErr != nil || info.ModTime().Before(spawnTime) || info.Size() > maxCursorStoreDBSize {
			return nil
		}
		found, readErr := cursorStoreContains(rootFS, path, workspaceNeedle)
		if readErr != nil {
			return nil
		}
		if !found {
			walPath := path + "-wal"
			walInfo, statErr := fs.Stat(rootFS, walPath)
			if statErr != nil || walInfo.Size() > maxCursorStoreDBSize {
				return nil
			}
			found, readErr = cursorStoreContains(rootFS, walPath, workspaceNeedle)
		}
		if readErr != nil || !found {
			return nil
		}
		chatID := filepath.Base(filepath.Dir(path))
		if chatID != "" {
			matches = append(matches, chatID)
			if len(matches) > 1 {
				return fs.SkipAll
			}
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

func discoverCursorMetadataSession(rootFS fs.FS, spawnTime time.Time, workdir string) string {
	type cursorChatMetadata struct {
		CreatedAtMS int64  `json:"createdAtMs"`
		CWD         string `json:"cwd"`
	}
	var matches []string
	err := fs.WalkDir(rootFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || entry.Name() != "meta.json" {
			return nil
		}
		data, readErr := fs.ReadFile(rootFS, path)
		if readErr != nil || len(data) > 1024*1024 {
			return nil
		}
		var metadata cursorChatMetadata
		if json.Unmarshal(data, &metadata) != nil || metadata.CreatedAtMS == 0 {
			return nil
		}
		metadataWorkdir, absErr := filepath.Abs(metadata.CWD)
		if absErr != nil || filepath.Clean(metadataWorkdir) != workdir {
			return nil
		}
		// Cursor records millisecond timestamps while the runner keeps
		// nanoseconds. Allow one second for truncation and filesystem clock skew.
		if time.UnixMilli(metadata.CreatedAtMS).Before(spawnTime.Add(-time.Second)) {
			return nil
		}
		chatID := filepath.Base(filepath.Dir(path))
		if chatID != "" {
			matches = append(matches, chatID)
			if len(matches) > 1 {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil || len(matches) != 1 {
		return ""
	}
	return matches[0]
}

func cursorStoreContains(rootFS fs.FS, path string, needle []byte) (bool, error) {
	file, err := rootFS.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	return readerContains(file, needle)
}

func readerContains(r io.Reader, needle []byte) (bool, error) {
	if len(needle) == 0 {
		return true, nil
	}
	buf := make([]byte, 32*1024)
	carry := make([]byte, 0, len(needle)-1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			window := make([]byte, len(carry)+n)
			copy(window, carry)
			copy(window[len(carry):], buf[:n])
			if bytes.Contains(window, needle) {
				return true, nil
			}
			keep := min(len(needle)-1, len(window))
			carry = append(carry[:0], window[len(window)-keep:]...)
		}
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
}
