package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CodexAdapter constructs invocation args for the Codex CLI.
type CodexAdapter struct{}

// BuildArgs constructs Codex CLI args.
//
// Patterns:
//   - Fresh interactive:  codex --no-alt-screen <prompt>
//   - Fresh headless:     codex --sandbox workspace-write exec --json <prompt>
//   - Resume interactive: codex resume --no-alt-screen <uuid> <prompt>
//   - Resume headless:    codex --sandbox workspace-write exec resume --json <uuid> <prompt>
//   - Model override:     appends -m <m>
//   - Effort override:    appends -c model_reasoning_effort="<effort>"
//
// -m is passed on resume as well as fresh sessions. Codex warns and resumes
// with its current default model when the flag is omitted, even if the thread
// was recorded with a different model. Passing the resolved model preserves
// the model selected by the workflow/profile across session resume.
func (a *CodexAdapter) BuildArgs(input *BuildArgsInput) []string {
	args := []string{"codex"}
	context := input.InvocationContext()

	sessionID := normalizeCodexSessionID(input.SessionID)
	resuming := sessionID != ""

	if context.IsAutonomous() {
		args = append(args, "--sandbox", "workspace-write")
	}

	if context.IsHeadless() {
		args = append(args, "exec")
		if resuming {
			args = append(args, "resume", "--json")
		} else {
			args = append(args, "--json")
		}
	} else {
		if resuming {
			args = append(args, "resume", "--no-alt-screen")
		} else {
			args = append(args, "--no-alt-screen")
		}
	}

	if input.Model != "" {
		args = append(args, "-m", input.Model)
	}
	if input.Effort != "" {
		args = append(args, "-c", `model_reasoning_effort="`+input.Effort+`"`)
	}

	if resuming {
		args = append(args, sessionID)
	}
	args = append(args, input.Prompt)
	return args
}

// SupportsSystemPrompt returns false — Codex CLI has no native system prompt flag.
func (a *CodexAdapter) SupportsSystemPrompt() bool {
	return false
}

func (a *CodexAdapter) ProbeModel(model, effort string) (ProbeStrength, error) {
	return BinaryOnly, nil
}

// FilterOutput extracts the final assistant text from Codex JSONL output.
func (a *CodexAdapter) FilterOutput(stdout string) string {
	return extractCodexAgentMessages(stdout)
}

// WrapStdout parses Codex JSONL and forwards only assistant text to the TUI.
func (a *CodexAdapter) WrapStdout(downstream io.Writer) io.Writer {
	return newCodexStreamFilter(downstream)
}

// WrapStderr suppresses Codex's persistent-exec rollout bookkeeping warning
// from live display. The post-run result filter still decides whether the
// process failed based on exit status, stdout, and raw stderr.
func (a *CodexAdapter) WrapStderr(downstream io.Writer) io.Writer {
	return &codexStderrFilter{downstream: downstream}
}

// FilterHeadlessResult suppresses known non-fatal Codex diagnostics emitted
// after a completed turn. Other non-zero exits and stderr are preserved.
func (a *CodexAdapter) FilterHeadlessResult(exitCode int, stdout, stderr string) (filteredExitCode int, filteredStderr string) {
	turnCompleted := codexTurnCompleted(stdout)
	filteredStderr = filterCodexStderr(stderr, exitCode == 0 || turnCompleted)
	if exitCode != 0 && turnCompleted && strings.TrimSpace(filteredStderr) == "" {
		return 0, ""
	}
	return exitCode, filteredStderr
}

// DiscoverSessionID returns the session ID after a Codex process exits.
// For headless mode, it parses the thread_id from the thread.started JSONL event.
// For interactive mode, it scans ~/.codex/sessions/ for the most recent session file.
func (a *CodexAdapter) DiscoverSessionID(opts *DiscoverOptions) string {
	if opts.Headless {
		if id := discoverCodexHeadlessSession(opts.ProcessOutput); id != "" {
			return id
		}
		return normalizeCodexSessionID(opts.PresetID)
	}
	if opts.PresetID != "" {
		return normalizeCodexSessionID(opts.PresetID)
	}
	return discoverCodexInteractiveSession(opts.SpawnTime)
}

// discoverCodexHeadlessSession parses the thread_id from the first JSONL event
// with type "thread.started".
func discoverCodexHeadlessSession(output string) string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Type == "thread.started" && event.ThreadID != "" {
			return event.ThreadID
		}
	}
	return ""
}

type codexStreamFilter struct {
	lineBufferedWriter
	wrote    bool
	lastText string
}

func newCodexStreamFilter(d io.Writer) *codexStreamFilter {
	f := &codexStreamFilter{}
	f.downstream = d
	f.onLine = f.processLine
	return f
}

func (f *codexStreamFilter) processLine(line []byte) error {
	text := codexDisplayText(line)
	if text == "" || text == f.lastText {
		return nil
	}
	if f.wrote {
		if err := f.writeDownstream([]byte("\n")); err != nil {
			return err
		}
	}
	if err := f.writeDownstream([]byte(text)); err != nil {
		return err
	}
	f.wrote = true
	f.lastText = text
	return nil
}

type codexStderrFilter struct {
	downstream io.Writer
	buf        []byte
	err        error
	state      codexIgnoredStderrState
}

func (f *codexStderrFilter) Write(p []byte) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	n := len(p)
	f.buf = append(f.buf, p...)
	for {
		idx := bytes.IndexByte(f.buf, '\n')
		if idx < 0 {
			break
		}
		line := f.buf[:idx]
		f.buf = f.buf[idx+1:]
		if err := f.processLine(line, true); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (f *codexStderrFilter) Close() error {
	if f.err != nil {
		return f.err
	}
	if len(f.buf) > 0 {
		if err := f.processLine(f.buf, false); err != nil {
			return err
		}
		f.buf = nil
	}
	return nil
}

func (f *codexStderrFilter) processLine(line []byte, addNewline bool) error {
	if isCodexIgnoredStderrLine(string(line), &f.state) {
		return nil
	}
	out := line
	if addNewline {
		out = append(append([]byte(nil), line...), '\n')
	}
	return f.writeDownstream(out)
}

func (f *codexStderrFilter) writeDownstream(p []byte) error {
	n, err := f.downstream.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		f.err = err
	}
	return err
}

func extractCodexAgentMessages(output string) string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var messages []string
	for scanner.Scan() {
		if text := codexDisplayText(scanner.Bytes()); text != "" {
			if len(messages) > 0 && messages[len(messages)-1] == text {
				continue
			}
			messages = append(messages, text)
		}
	}
	return strings.Join(messages, "\n")
}

func codexDisplayText(line []byte) string {
	var event struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Item    *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return ""
	}
	if event.Type == "item.completed" && event.Item != nil && event.Item.Type == "agent_message" {
		return event.Item.Text
	}
	if event.Type == "error" {
		return event.Message
	}
	if event.Type == "turn.failed" && event.Error != nil {
		return event.Error.Message
	}
	return ""
}

func codexTurnCompleted(output string) bool {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Type == "turn.completed" {
			return true
		}
	}
	return false
}

func filterCodexStderr(stderr string, suppressApplyPatch bool) string {
	scanner := bufio.NewScanner(strings.NewReader(stderr))
	var lines []string
	state := codexIgnoredStderrState{suppressApplyPatch: suppressApplyPatch}
	for scanner.Scan() {
		line := scanner.Text()
		if isCodexIgnoredStderrLine(line, &state) {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

type codexIgnoredStderrState struct {
	skippedRollout     bool
	skippedApplyPatch  bool
	suppressApplyPatch bool
}

// TODO: If more recoverable Codex diagnostics need filtering, replace this
// ad hoc state machine with named rules that declare start matching,
// continuation matching, and when the diagnostic may be suppressed.
func isCodexIgnoredStderrLine(line string, state *codexIgnoredStderrState) bool {
	lower := strings.ToLower(line)
	trimmed := strings.TrimSpace(lower)
	if state.skippedApplyPatch {
		rawTrimmed := strings.TrimSpace(line)
		if rawTrimmed == "" || rawTrimmed == "}" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			return true
		}
		state.skippedApplyPatch = false
	}
	if strings.TrimSpace(lower) == "reading additional input from stdin..." {
		state.skippedRollout = false
		return true
	}
	if strings.Contains(lower, "failed to record rollout items") {
		state.skippedRollout = true
		return true
	}
	if state.skippedRollout && strings.HasPrefix(trimmed, "thread ") && strings.HasSuffix(trimmed, " not found") {
		state.skippedRollout = false
		return true
	}
	if state.suppressApplyPatch &&
		strings.Contains(lower, "codex_core::tools::router") &&
		strings.Contains(lower, "apply_patch verification failed") {
		state.skippedApplyPatch = true
		return true
	}
	// Reset skippedRollout on any non-matching line. The rollout error often
	// arrives as a single self-contained line ("...failed to record rollout
	// items: thread <uuid> not found"), which leaves skippedRollout=true even
	// though there is no continuation to suppress; clearing here keeps a
	// stale flag from accidentally swallowing an unrelated future line.
	state.skippedRollout = false
	return false
}

// discoverCodexInteractiveSession scans ~/.codex/sessions/YYYY/MM/DD/ for the
// most recent .jsonl file created after spawn time, matching CWD from the
// session_meta payload.
func discoverCodexInteractiveSession(spawnTime time.Time) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	sessionsDir := filepath.Join(home, ".codex", "sessions")

	type candidate struct {
		path    string
		modTime time.Time
	}
	var candidates []candidate

	_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if info.ModTime().Before(spawnTime) {
			return nil
		}
		candidates = append(candidates, candidate{path: path, modTime: info.ModTime()})
		return nil
	})

	// Sort most recent first.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	for _, c := range candidates {
		if matchesSessionCwd(c.path, cwd) {
			if id := codexSessionID(c.path); id != "" {
				return id
			}
			return normalizeCodexSessionID(strings.TrimSuffix(filepath.Base(c.path), ".jsonl"))
		}
	}

	return ""
}

func normalizeCodexSessionID(id string) string {
	if !strings.HasPrefix(id, "rollout-") || len(id) < 36 {
		return id
	}
	suffix := id[len(id)-36:]
	if looksLikeUUID(suffix) {
		return suffix
	}
	return id
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
				return false
			}
		}
	}
	return true
}

func codexSessionID(sessionFile string) string {
	f, err := os.Open(sessionFile) // #nosec G304 -- scanning known Codex session directory
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var meta struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Payload struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
			continue
		}
		if meta.Type == "session_meta" {
			if meta.Payload.ID != "" {
				return meta.Payload.ID
			}
			return meta.ID
		}
	}
	return ""
}

// matchesSessionCwd checks whether a Codex session file's session_meta payload
// matches the given working directory.
func matchesSessionCwd(sessionFile, cwd string) bool {
	f, err := os.Open(sessionFile) // #nosec G304 -- scanning known Codex session directory
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var meta struct {
			Type    string `json:"type"`
			Cwd     string `json:"cwd"`
			Payload struct {
				Cwd string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
			continue
		}
		if meta.Type == "session_meta" {
			return meta.Cwd == cwd || meta.Payload.Cwd == cwd
		}
	}
	return false
}
