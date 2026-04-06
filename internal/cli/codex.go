package cli

import (
	"bufio"
	"encoding/json"
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
//   - Fresh headless:     codex exec --json <prompt>
//   - Resume interactive: codex resume --no-alt-screen <uuid> <prompt>
//   - Resume headless:    codex exec resume <uuid> <prompt>
//   - Model override:     appends -m <m>
func (a *CodexAdapter) BuildArgs(input BuildArgsInput) []string {
	args := []string{"codex"}

	if input.Headless {
		args = append(args, "exec")
		if input.SessionID != "" {
			args = append(args, "resume", input.SessionID)
		} else {
			args = append(args, "--json")
		}
	} else {
		if input.SessionID != "" {
			args = append(args, "resume", "--no-alt-screen", input.SessionID)
		} else {
			args = append(args, "--no-alt-screen")
		}
	}

	if input.Model != "" {
		args = append(args, "-m", input.Model)
	}

	args = append(args, input.Prompt)
	return args
}

// SupportsSystemPrompt returns false — Codex CLI has no native system prompt flag.
func (a *CodexAdapter) SupportsSystemPrompt() bool {
	return false
}

// DiscoverSessionID returns the session ID after a Codex process exits.
// For headless mode, it parses the thread_id from the thread.started JSONL event.
// For interactive mode, it scans ~/.codex/sessions/ for the most recent session file.
func (a *CodexAdapter) DiscoverSessionID(opts DiscoverOptions) string {
	if opts.PresetID != "" {
		return opts.PresetID
	}
	if opts.Headless {
		return discoverCodexHeadlessSession(opts.ProcessOutput)
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
			base := filepath.Base(c.path)
			return strings.TrimSuffix(base, ".jsonl")
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
			Type string `json:"type"`
			Cwd  string `json:"cwd"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
			continue
		}
		if meta.Type == "session_meta" {
			return meta.Cwd == cwd
		}
	}
	return false
}
