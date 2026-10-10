package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func claudeConfigHome(uc UsageContext) (home, config string) {
	for _, entry := range uc.Env {
		if v, ok := strings.CutPrefix(entry, "HOME="); ok {
			home = v
		}
		if v, ok := strings.CutPrefix(entry, "CLAUDE_CONFIG_DIR="); ok {
			config = v
		}
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if config == "" {
		config = filepath.Join(home, ".claude")
	}
	return home, config
}

func ownedClaudePath(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("cannot determine Claude settings ownership")
	}
	return stat.Uid == uint32(os.Getuid()), nil // #nosec G115 -- OS uid is non-negative
}

func claudeSettingsRoot(work, home string, env []string) (string, error) {
	run := func(arg string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "rev-parse", arg) // #nosec G204 -- arg is one of two fixed rev-parse options supplied below
		cmd.Dir = work
		cmd.Env = append(append([]string(nil), env...), "LC_ALL=C")
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	top, err := run("--show-toplevel")
	if err != nil {
		// Only a genuine non-repository falls back. Broken repositories and a
		// missing git executable must not silently choose another user's command.
		dir := work
		for {
			_, statErr := os.Lstat(filepath.Join(dir, ".git"))
			if statErr == nil || !os.IsNotExist(statErr) {
				return "", err
			}
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
			dir = next
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || !strings.Contains(string(exit.Stderr), "not a git repository") {
			return "", err
		}
		return work, nil
	}
	common, err := run("--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(work, common)
	}
	root := top
	if filepath.Clean(common) != filepath.Join(top, ".git") {
		root = filepath.Dir(common)
	}
	if filepath.Clean(root) == filepath.Clean(home) {
		return work, nil
	}
	for _, path := range []string{root, filepath.Join(root, ".git"), filepath.Join(root, ".claude")} {
		owned, err := ownedClaudePath(path)
		if err != nil {
			return "", err
		}
		if !owned {
			return work, nil
		}
	}
	return root, nil
}

func resolveClaudeStatusLine(uc UsageContext) (map[string]any, error) {
	work := uc.Workdir
	if work == "" {
		work, _ = os.Getwd()
	}
	work, err := filepath.Abs(work)
	if err != nil {
		return nil, err
	}
	home, config := claudeConfigHome(uc)
	env := uc.Env
	if env == nil {
		env = os.Environ()
	}
	root, err := claudeSettingsRoot(work, home, env)
	if err != nil {
		return nil, err
	}
	paths := []string{filepath.Join(root, ".claude", "settings.local.json")}
	if root != work {
		paths = append(paths, filepath.Join(work, ".claude", "settings.local.json"))
	}
	paths = append(paths, filepath.Join(work, ".claude", "settings.json"), filepath.Join(config, "settings.json"))
	var selected map[string]any
	for _, path := range paths {
		line, err := readClaudeStatusLine(path)
		if err != nil {
			return nil, err
		}
		if selected == nil {
			selected = line
		}
	}
	return selected, nil
}

func readClaudeStatusLine(path string) (map[string]any, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claude settings %s: %w", path, err)
	}
	defer func() { _ = root.Close() }()
	raw, err := root.ReadFile(filepath.Base(path))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claude settings %s: %w", path, err)
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(raw, &settings) != nil || settings == nil {
		return nil, fmt.Errorf("invalid Claude settings %s", path)
	}
	value, ok := settings["statusLine"]
	if !ok {
		return nil, nil
	}
	var line map[string]any
	if json.Unmarshal(value, &line) != nil || line == nil {
		return nil, fmt.Errorf("invalid Claude statusLine %s", path)
	}
	command, ok := line["command"].(string)
	if !ok || command == "" || line["type"] != "command" {
		return nil, fmt.Errorf("unsupported Claude statusLine %s", path)
	}
	return line, nil
}

// MergeInteractiveUsageSettings replaces only the status-line command in the
// invocation's existing settings JSON. Display fields and Stop hooks survive.
func MergeInteractiveUsageSettings(args []string, p *InteractiveUsagePlan, executable string) []string {
	if !p.ReportEnabled || !filepath.IsAbs(executable) {
		return args
	}
	line := map[string]any{"type": "command"}
	for k, v := range p.StatusLine {
		line[k] = v
	}
	command := shellQuote(executable) + " internal statusline-record --report " + shellQuote(p.ReportPath)
	if delegate, ok := line["command"].(string); ok && delegate != "" {
		command += " --delegate " + shellQuote(delegate)
	}
	line["command"] = command
	settings := map[string]any{}
	result := append([]string(nil), args...)
	for i := 1; i < len(result)-1; i++ {
		if result[i] == "--" {
			break
		}
		if result[i] == "--settings" {
			if json.Unmarshal([]byte(result[i+1]), &settings) != nil {
				return args
			}
			settings["statusLine"] = line
			raw, _ := json.Marshal(settings)
			result[i+1] = string(raw)
			return result
		}
	}
	settings["statusLine"] = line
	raw, _ := json.Marshal(settings)
	for i := 1; i < len(result); i++ {
		if result[i] == "--" {
			tail := append([]string(nil), result[i:]...)
			return append(append(result[:i], "--settings", string(raw)), tail...)
		}
	}
	return append(result, "--settings", string(raw))
}
