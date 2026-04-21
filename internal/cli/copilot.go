package cli

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// CopilotAdapter constructs invocation args for the GitHub Copilot CLI.
type CopilotAdapter struct{}

// BuildArgs constructs Copilot CLI args for headless mode.
//
// Patterns:
//   - Fresh headless:  copilot -p <prompt> --allow-all --autopilot -s [--model <m>] [--reasoning-effort <e>]
//   - Resume headless: copilot -p <prompt> --allow-all --autopilot -s --resume=<id>
//
// --allow-all grants tool, file-path, and URL permissions required for autonomous
// operation. --autopilot keeps the agent running until the task is complete.
// -s outputs only the agent response text, matching Claude's plain-text output.
// --model and --reasoning-effort are omitted on resume: a resumed copilot thread
// keeps the model and effort it was started with.
func (a *CopilotAdapter) BuildArgs(input *BuildArgsInput) []string {
	args := []string{"copilot", "-p", input.Prompt, "--allow-all", "--autopilot", "-s"}

	resuming := input.Resume

	if !resuming {
		if input.Model != "" {
			args = append(args, "--model", input.Model)
		}
		if input.Effort != "" {
			args = append(args, "--reasoning-effort", input.Effort)
		}
	}

	if resuming && input.SessionID != "" {
		args = append(args, "--resume="+input.SessionID)
	}

	if slices.Contains(input.DisallowedTools, "AskUserQuestion") {
		args = append(args, "--no-ask-user")
	}

	return args
}

// SupportsSystemPrompt returns false — Copilot CLI has no native system prompt flag.
func (a *CopilotAdapter) SupportsSystemPrompt() bool {
	return false
}

// DiscoverSessionID returns the session ID after a copilot process exits by
// scanning ~/.copilot/session-state/ for the most recently modified directory
// created after spawn time, matching on CWD from workspace.yaml.
func (a *CopilotAdapter) DiscoverSessionID(opts *DiscoverOptions) string {
	return discoverCopilotSession(opts.SpawnTime, opts.Workdir)
}

// InteractiveModeError returns an error indicating interactive mode is not supported.
// This implements the optional cli.InteractiveRejector interface.
func (a *CopilotAdapter) InteractiveModeError() error {
	return fmt.Errorf("interactive mode is not supported for the copilot CLI")
}

// discoverCopilotSession scans ~/.copilot/session-state/ for the most recently
// modified session directory created after spawnTime, matching on CWD from workspace.yaml.
// workdir is the effective CWD of the Copilot process; when empty, os.Getwd() is used.
func discoverCopilotSession(spawnTime time.Time, workdir string) string {
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

	sessionStateDir := filepath.Join(home, ".copilot", "session-state")

	entries, err := os.ReadDir(sessionStateDir)
	if err != nil {
		return ""
	}

	type candidate struct {
		id      string
		modTime time.Time
	}
	var candidates []candidate

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(spawnTime) {
			continue
		}
		candidates = append(candidates, candidate{id: entry.Name(), modTime: info.ModTime()})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	var matched []string
	for _, c := range candidates {
		if matchesCopilotSessionCwd(filepath.Join(sessionStateDir, c.id), cwd) {
			matched = append(matched, c.id)
		}
	}
	if len(matched) > 1 {
		log.Printf("copilot: %d session candidates match cwd %s; using most recent — misattribution possible if concurrent sessions share this directory", len(matched), cwd)
	}
	if len(matched) > 0 {
		return matched[0]
	}

	return ""
}

type copilotWorkspace struct {
	Cwd string `yaml:"cwd"`
}

// matchesCopilotSessionCwd checks whether a copilot session directory's workspace.yaml
// matches the given working directory. Both paths are canonicalized via
// filepath.EvalSymlinks before comparison to handle symlinked directories (e.g.
// /var → /private/var on macOS).
func matchesCopilotSessionCwd(sessionDir, cwd string) bool {
	data, err := os.ReadFile(filepath.Join(sessionDir, "workspace.yaml")) // #nosec G304
	if err != nil {
		return false
	}
	var ws copilotWorkspace
	if err := yaml.Unmarshal(data, &ws); err != nil {
		return false
	}
	return canonicalize(ws.Cwd) == canonicalize(cwd)
}

// canonicalize resolves symlinks in p, falling back to filepath.Clean on error.
func canonicalize(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}
