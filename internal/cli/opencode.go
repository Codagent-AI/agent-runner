package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"os"
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
//   - Fresh headless:      opencode run --format json --dangerously-skip-permissions [--model <m>] [--variant <e>] <prompt>
//   - Resume headless:     opencode run --format json --dangerously-skip-permissions -s <id> <prompt>
//   - Fresh interactive:   opencode --prompt <prompt> [--model <m>]
//   - Resume interactive:  opencode --prompt <prompt> -s <id>
//
// OpenCode has no native system-prompt or disallowed-tools flags. --variant
// and --dangerously-skip-permissions are run-only, so interactive mode omits
// them. --model and --variant are omitted on resume because a resumed session
// keeps the settings it was started with.
func (a *OpenCodeAdapter) BuildArgs(input *BuildArgsInput) []string {
	var args []string
	context := input.InvocationContext()
	if context.IsHeadless() {
		args = []string{"opencode", "run", "--format", "json", "--dangerously-skip-permissions"}
	} else {
		args = []string{"opencode", "--prompt", input.Prompt}
	}

	resuming := input.Resume && input.SessionID != ""
	if resuming {
		args = append(args, "-s", input.SessionID)
	} else if input.Model != "" {
		args = append(args, "--model", input.Model)
	}

	if context.IsHeadless() && !resuming && input.Effort != "" {
		args = append(args, "--variant", input.Effort)
	}

	if context.IsHeadless() {
		args = append(args, input.Prompt)
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
	return discoverOpenCodeInteractiveSession(opts.SpawnTime)
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
