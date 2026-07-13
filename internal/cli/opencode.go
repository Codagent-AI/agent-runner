package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OpenCodeAdapter constructs invocation args for the OpenCode CLI.
type OpenCodeAdapter struct{}

// BuildArgs constructs OpenCode CLI args.
//
// Patterns:
//   - Fresh headless:      opencode run --format json [--model <m>] [--variant <e>] <prompt>
//   - Resume headless:     opencode run --format json -s <id> <prompt>
//   - Fresh interactive:   opencode --prompt <prompt> [--model <m>]
//   - Resume interactive:  opencode -s <id> --prompt <prompt>
//
// OpenCode has no native system-prompt or disallowed-tools flags. --variant
// is run-only, so interactive mode omits it. --model and --variant are omitted on resume because a resumed session
// keeps the settings it was started with.
func (a *OpenCodeAdapter) BuildArgs(input *BuildArgsInput) []string {
	context := input.InvocationContext()
	resuming := input.Resume && input.SessionID != ""
	if context.IsHeadless() {
		args := []string{"opencode", "run", "--format", "json"}
		if resuming {
			args = append(args, "-s", input.SessionID)
		} else {
			if input.Model != "" {
				args = append(args, "--model", input.Model)
			}
			if input.Effort != "" {
				args = append(args, "--variant", input.Effort)
			}
		}
		return append(args, input.Prompt)
	}

	args := []string{"opencode"}
	if resuming {
		// OpenCode 1.17 only auto-submits --prompt to a resumed TUI when
		// --session precedes --prompt.
		args = append(args, "-s", input.SessionID)
	}
	args = append(args, "--prompt", input.Prompt)
	if !resuming && input.Model != "" {
		args = append(args, "--model", input.Model)
	}
	return args
}

func (a *OpenCodeAdapter) SupportsSystemPrompt() bool {
	return false
}

func (a *OpenCodeAdapter) ProbeModel(model, effort string) (ProbeStrength, error) {
	return BinaryOnly, nil
}

func (a *OpenCodeAdapter) DiscoverSessionID(opts *DiscoverOptions) string {
	if opts.Headless {
		return discoverOpenCodeHeadlessSession(opts.ProcessOutput)
	}
	if id := discoverOpenCodeInteractiveSession(opts.SpawnTime); id != "" {
		return id
	}
	if id := discoverOpenCodeDatabaseSession(opts.SpawnTime, opts.Workdir, func(query string) ([]byte, error) {
		return exec.Command("opencode", "db", query, "--format", "json").Output() // #nosec G204 -- fixed executable; query is one argv value, not shell-expanded
	}); id != "" {
		return id
	}
	return ""
}

func (a *OpenCodeAdapter) FilterOutput(stdout string) string {
	return extractOpenCodeText(stdout)
}

func (a *OpenCodeAdapter) WrapStdout(downstream io.Writer) io.Writer {
	return newOpenCodeStreamFilter(downstream)
}

func discoverOpenCodeHeadlessSession(output string) string {
	reader := bufio.NewReader(strings.NewReader(output))
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var event struct {
				SessionID string `json:"sessionID"`
			}
			if jsonErr := json.Unmarshal(line, &event); jsonErr == nil && event.SessionID != "" {
				return event.SessionID
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("opencode: failed to read opencode session output: %v", err)
			}
			return ""
		}
	}
}

func discoverOpenCodeDatabaseSession(spawnTime time.Time, workdir string, runQuery func(string) ([]byte, error)) string {
	if workdir == "" {
		var err error
		workdir, err = os.Getwd()
		if err != nil {
			return ""
		}
	}

	escapedWorkdir := strings.ReplaceAll(workdir, "'", "''")
	query := fmt.Sprintf(
		"SELECT id, time_created FROM session WHERE time_created >= %d AND directory = '%s' ORDER BY time_created DESC",
		spawnTime.UnixMilli(), escapedWorkdir,
	)
	output, err := runQuery(query)
	if err != nil {
		return ""
	}
	var candidates []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(output, &candidates); err != nil {
		return ""
	}
	if len(candidates) > 1 {
		log.Printf("opencode: %d database sessions match spawn time and workdir; refusing to guess", len(candidates))
		return ""
	}
	if len(candidates) == 1 {
		return candidates[0].ID
	}
	return ""
}

func discoverOpenCodeInteractiveSession(spawnTime time.Time) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	pattern := filepath.Join(home, ".local", "share", "opencode", "storage", "session_diff", "ses_*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return ""
	}

	type candidate struct {
		id      string
		modTime time.Time
	}
	var candidates []candidate
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.ModTime().Before(spawnTime) {
			continue
		}
		base := filepath.Base(path)
		candidates = append(candidates, candidate{
			id:      strings.TrimSuffix(base, ".json"),
			modTime: info.ModTime(),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	if len(candidates) > 1 {
		log.Printf("opencode: %d session candidates match spawn time; refusing to guess", len(candidates))
		return ""
	}
	if len(candidates) == 1 {
		return candidates[0].id
	}
	return ""
}

func extractOpenCodeText(output string) string {
	reader := bufio.NewReader(strings.NewReader(output))
	var text strings.Builder
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			text.WriteString(openCodeTextFromLine(line))
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("opencode: failed to read opencode output: %v", err)
			}
			break
		}
	}
	return text.String()
}

func openCodeTextFromLine(line []byte) string {
	var event struct {
		Type string `json:"type"`
		Part *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"part"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return ""
	}
	if event.Type != "text" || event.Part == nil || event.Part.Type != "text" {
		return ""
	}
	return event.Part.Text
}

type openCodeStreamFilter struct {
	lineBufferedWriter
}

func newOpenCodeStreamFilter(d io.Writer) *openCodeStreamFilter {
	f := &openCodeStreamFilter{}
	f.downstream = d
	f.onLine = f.processLine
	return f
}

func (f *openCodeStreamFilter) processLine(line []byte) error {
	text := openCodeTextFromLine(line)
	if text == "" {
		return nil
	}
	return f.writeDownstream([]byte(text))
}
