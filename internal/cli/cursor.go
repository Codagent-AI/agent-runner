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
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/usersettings"
)

// CursorAdapter constructs invocation args for the Cursor agent CLI.
type CursorAdapter struct {
	runStoreQuery           func(context.Context, string) ([]byte, error)
	prepareCompletionPlugin func(CompletionCommand) (string, error)              // test seam; nil uses prepareCursorCompletionPlugin
	prepareConfig           func(CompletionCommand) (cursorPrivateConfig, error) // test seam; nil uses prepareCursorPrivateConfig
}

// cursorPrivateConfig describes the materialized per-invocation configuration
// directory. DenyRuleBlockingCompletion carries the first user deny rule that
// covers the completion command; deny rules take precedence over allow rules
// in Cursor, so such a rule means the pre-approval cannot take effect.
type cursorPrivateConfig struct {
	Dir                        string
	DenyRuleBlockingCompletion string
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
// both autonomous-headless and autonomous-interactive invocations. Interactive
// backends additionally receive a private configuration directory (via
// SpawnEnv) whose permissions allow exactly the completion command —
// Shell(<abs>:step complete) matches the command and argument patterns
// separately, so the completion client runs without an approval prompt while
// every other command keeps Cursor's normal permission behavior.
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
// plugin cannot be created.
func (a *CursorAdapter) BuildArgsWithError(input *BuildArgsInput) ([]string, error) {
	invocationContext := input.InvocationContext()
	mode := usersettings.EffectiveAutonomousPermissionMode(input.PermissionMode)
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

// SpawnEnv implements SpawnEnvContributor: interactive-backend invocations
// with a completion command run against a private, per-invocation
// configuration directory whose permissions pre-approve exactly the
// completion command. The user's own configuration files are never modified.
func (a *CursorAdapter) SpawnEnv(input *BuildArgsInput) ([]string, error) {
	invocationContext := input.InvocationContext()
	if invocationContext.IsHeadless() || input.CompletionCommand == nil || !input.CompletionCommand.Valid() {
		return nil, nil
	}
	prepare := a.prepareConfig
	if prepare == nil {
		prepare = prepareCursorPrivateConfig
	}
	config, err := prepare(*input.CompletionCommand)
	if err != nil {
		return nil, fmt.Errorf("cursor: prepare private config: %w", err)
	}
	if invocationContext == ContextAutonomousInteractive &&
		usersettings.EffectiveAutonomousPermissionMode(input.PermissionMode) != usersettings.PermissionModeYOLO {
		if config.DenyRuleBlockingCompletion != "" {
			return nil, cursorDenyBlockedError(fmt.Sprintf("your Cursor deny rule %q", config.DenyRuleBlockingCompletion))
		}
		// Project-level permissions are read by Cursor straight from the
		// project directory — CURSOR_CONFIG_DIR does not redirect them — so a
		// blocking deny there survives the private config copy and must fail
		// the unattended run early instead of hanging on a prompt.
		rule, path, err := cursorProjectDenyBlockingCompletion(input.Workdir, *input.CompletionCommand)
		if err != nil {
			return nil, fmt.Errorf("cursor: project permissions: %w", err)
		}
		if rule != "" {
			return nil, cursorDenyBlockedError(fmt.Sprintf("the project-level Cursor deny rule %q in %s", rule, path))
		}
	}
	return []string{"CURSOR_CONFIG_DIR=" + config.Dir}, nil
}

// cursorDenyBlockedError formats the unattended-run failure for a deny rule
// that covers the completion command; source names the rule and where it
// lives, so the remediation advice stays identical for every deny location.
func cursorDenyBlockedError(source string) error {
	return fmt.Errorf("cursor: unattended completion is blocked by %s, which takes precedence over the completion pre-approval; remove that deny rule or set autonomous_permission_mode: yolo", source)
}

// cursorProjectDenyBlockingCompletion reports the first deny rule in the
// project-level Cursor permissions file (<workdir>/.cursor/cli.json) that
// covers the completion command, along with the file's path. A missing file
// is not an error; an unreadable or unparsable one is, because deny detection
// cannot be trusted without it.
func cursorProjectDenyBlockingCompletion(workdir string, command CompletionCommand) (rule, path string, err error) {
	if workdir == "" {
		workdir, err = os.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("resolve step working directory: %w", err)
		}
	}
	path = filepath.Join(workdir, ".cursor", "cli.json")
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed name under the step's own working directory
	if os.IsNotExist(err) {
		return "", path, nil
	}
	if err != nil {
		return "", path, fmt.Errorf("read %s: %w", path, err)
	}
	var config struct {
		Permissions struct {
			Deny []any `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return "", path, fmt.Errorf("parse %s: %w", path, err)
	}
	return firstBlockingDenyRule(config.Permissions.Deny, command), path, nil
}

// firstBlockingDenyRule returns the first deny-list entry covering the
// completion command. The user-level and project-level scans both go through
// it so deny detection cannot drift between the two.
func firstBlockingDenyRule(deny []any, command CompletionCommand) string {
	for _, entry := range deny {
		if rule, ok := entry.(string); ok && cursorShellPermissionMatches(rule, command) {
			return rule
		}
	}
	return ""
}

// cursorConfigSourceDir resolves the configuration directory the Cursor CLI
// would use for this process: an inherited CURSOR_CONFIG_DIR, else (on
// Linux/BSD, per Cursor's configuration reference) $XDG_CONFIG_HOME/cursor
// when it holds a cli-config.json, else ~/.cursor. The result is always
// absolute — it becomes a symlink target resolved from the symlink's own
// directory, where a relative path would point elsewhere.
func cursorConfigSourceDir() (string, error) {
	return cursorConfigSourceDirFor(runtime.GOOS)
}

func cursorConfigSourceDirFor(goos string) (string, error) {
	if dir := os.Getenv("CURSOR_CONFIG_DIR"); dir != "" {
		return absCursorConfigDir(dir)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" && goos != "darwin" && goos != "windows" {
		candidate := filepath.Join(xdg, "cursor")
		// #nosec G703 -- XDG_CONFIG_HOME is the user's own documented Cursor config location; the stat only selects the source dir Cursor itself would read
		if _, err := os.Stat(filepath.Join(candidate, "cli-config.json")); err == nil {
			return absCursorConfigDir(candidate)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("locate cursor config dir: %w", err)
	}
	return filepath.Join(home, ".cursor"), nil
}

func absCursorConfigDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve cursor config dir %q: %w", dir, err)
	}
	return abs, nil
}

// cursorChatsRoot resolves the chat-store root for session discovery and
// durability inspection. The private per-invocation config dir symlinks its
// chats entry here, so spawned sessions always land in this shared store.
func cursorChatsRoot() (string, error) {
	source, err := cursorConfigSourceDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(source, "chats"), nil
}

// cursorCompletionPermission renders the narrow pre-approval rule: Cursor
// matches the command and argument patterns separately, so this covers
// exactly the absolute-path completion client with its fixed arguments —
// never other arguments, chaining, or other agent-runner subcommands. A
// command containing Cursor permission metacharacters is rejected: wildcards
// would silently broaden the rule to other executables and delimiters would
// malform it, and Cursor documents no literal-escaping mechanism.
func cursorCompletionPermission(command CompletionCommand) (string, error) {
	for _, part := range append([]string{command.Executable}, command.Args...) {
		if strings.ContainsAny(part, "*?:()") {
			return "", fmt.Errorf("completion command part %q contains Cursor permission metacharacters; a safe narrow pre-approval cannot be expressed", part)
		}
		for _, r := range part {
			if r < 0x20 || r == 0x7f {
				return "", fmt.Errorf("completion command part %q contains control characters; a safe narrow pre-approval cannot be expressed", part)
			}
		}
	}
	return "Shell(" + command.Executable + ":" + strings.Join(command.Args, " ") + ")", nil
}

func prepareCursorPrivateConfig(command CompletionCommand) (cursorPrivateConfig, error) {
	if !command.Valid() {
		return cursorPrivateConfig{}, fmt.Errorf("invalid completion command")
	}
	source, err := cursorConfigSourceDir()
	if err != nil {
		return cursorPrivateConfig{}, err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return cursorPrivateConfig{}, fmt.Errorf("locate user cache: %w", err)
	}
	return prepareCursorPrivateConfigAt(source, filepath.Join(cacheDir, "agent-runner"), command)
}

// prepareCursorPrivateConfigAt materializes the private configuration
// directory: the user's cli-config.json with the completion pre-approval
// appended to permissions.allow (deny rules are preserved and keep
// precedence, per Cursor's semantics), plus a chats symlink back to the
// user's real session store so resume, discovery, and durability keep
// operating on the shared store.
func prepareCursorPrivateConfigAt(sourceDir, cacheRoot string, command CompletionCommand) (cursorPrivateConfig, error) {
	rule, err := cursorCompletionPermission(command)
	if err != nil {
		return cursorPrivateConfig{}, err
	}
	sourceConfig := filepath.Join(sourceDir, "cli-config.json")
	raw, err := os.ReadFile(sourceConfig) // #nosec G304 -- the user's own Cursor config location
	switch {
	case os.IsNotExist(err):
		raw = []byte(`{"version": 1, "permissions": {"allow": [], "deny": []}}`)
	case err != nil:
		return cursorPrivateConfig{}, fmt.Errorf("read cursor config %s: %w", sourceConfig, err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return cursorPrivateConfig{}, fmt.Errorf("parse cursor config %s: %w", sourceConfig, err)
	}
	permissions, ok := config["permissions"].(map[string]any)
	if !ok {
		permissions = map[string]any{}
		config["permissions"] = permissions
	}
	allow, _ := permissions["allow"].([]any)
	present := false
	for _, entry := range allow {
		if entry == rule {
			present = true
			break
		}
	}
	if !present {
		permissions["allow"] = append(allow, rule)
	}
	deny, _ := permissions["deny"].([]any)
	blockingDeny := firstBlockingDenyRule(deny, command)
	rendered, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return cursorPrivateConfig{}, fmt.Errorf("render cursor config: %w", err)
	}

	// The source directory participates in the digest because the chats
	// symlink below is tied to it: identical configs from different sources
	// must not share a private dir.
	digest := sha256.Sum256([]byte("cursor-config-v2\x00" + sourceDir + "\x00" + rule + "\x00" + string(raw)))
	dir := filepath.Join(cacheRoot, "cursor-config", hex.EncodeToString(digest[:6]))
	if err := os.MkdirAll(dir, 0o700); err != nil { // #nosec G703 -- dir combines the local user's cache root with a content digest; creating it is this function's purpose
		return cursorPrivateConfig{}, fmt.Errorf("create cursor private config dir: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "cli-config.json"), rendered); err != nil {
		return cursorPrivateConfig{}, fmt.Errorf("write cursor private config: %w", err)
	}
	evictStaleCursorConfigs(filepath.Join(cacheRoot, "cursor-config"), dir)
	result := cursorPrivateConfig{Dir: dir, DenyRuleBlockingCompletion: blockingDeny}

	// The chat store follows CURSOR_CONFIG_DIR, so without this link every
	// spawned session would be stranded in the private dir — breaking resume,
	// discovery, and the durability probe.
	chatsTarget := filepath.Join(sourceDir, "chats")
	if err := os.MkdirAll(chatsTarget, 0o700); err != nil {
		return cursorPrivateConfig{}, fmt.Errorf("create cursor chats dir: %w", err)
	}
	chatsLink := filepath.Join(dir, "chats")
	if existing, err := os.Readlink(chatsLink); err == nil {
		if existing == chatsTarget {
			return result, nil
		}
		return cursorPrivateConfig{}, fmt.Errorf("cursor private config %s: chats links to %s, want %s", dir, existing, chatsTarget)
	} else if !os.IsNotExist(err) {
		return cursorPrivateConfig{}, fmt.Errorf("cursor private config %s: chats exists and is not a symlink", dir)
	}
	if err := os.Symlink(chatsTarget, chatsLink); err != nil {
		// A concurrent invocation with identical content may have linked first.
		if existing, readErr := os.Readlink(chatsLink); readErr == nil && existing == chatsTarget {
			return result, nil
		}
		return cursorPrivateConfig{}, fmt.Errorf("link cursor chats store: %w", err)
	}
	return result, nil
}

// cursorConfigCacheTTL bounds the private-config cache. Every invocation
// rewrites its own cli-config.json, so that file's mtime marks the digest
// dir's last use; dirs untouched for the TTL are removed. Digests change with
// the runner executable path and the user's config contents (test-built
// binaries mint a new dir per build), and Cursor writes sizable state files
// into active dirs, so without eviction the cache grows without bound.
const cursorConfigCacheTTL = 14 * 24 * time.Hour

// evictStaleCursorConfigs is best-effort: eviction failures never fail the
// spawn, and a dir serving a live session is always younger than the TTL.
func evictStaleCursorConfigs(root, current string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-cursorConfigCacheTTL)
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		if dir == current || !entry.IsDir() {
			continue
		}
		var lastUse time.Time
		if info, err := os.Stat(filepath.Join(dir, "cli-config.json")); err == nil {
			lastUse = info.ModTime()
		} else if info, err := entry.Info(); err == nil {
			lastUse = info.ModTime()
		} else {
			continue
		}
		if lastUse.Before(cutoff) {
			_ = os.RemoveAll(dir)
		}
	}
}

// cursorShellPermissionMatches reports whether a Shell(...) permission entry
// covers the completion command. The colon form matches the command and
// argument patterns separately with *, ** and ? wildcards. The legacy
// no-colon form is a prefix pattern over the full command line: it matches
// the whole command or any command beginning with the pattern plus a space.
func cursorShellPermissionMatches(entry string, command CompletionCommand) bool {
	inner, ok := strings.CutPrefix(entry, "Shell(")
	if !ok || !strings.HasSuffix(inner, ")") {
		return false
	}
	inner = strings.TrimSuffix(inner, ")")
	commandPattern, argsPattern, hasArgs := strings.Cut(inner, ":")
	if !hasArgs {
		full := command.Executable + " " + strings.Join(command.Args, " ")
		return cursorGlobPrefixMatch(commandPattern, full)
	}
	return cursorGlobMatch(commandPattern, command.Executable) &&
		cursorGlobMatch(argsPattern, strings.Join(command.Args, " "))
}

func cursorGlobMatch(pattern, value string) bool {
	matched, err := regexp.MatchString("^"+cursorGlobRegexp(pattern)+"$", value)
	return err == nil && matched
}

// cursorGlobPrefixMatch applies the legacy allowlist semantics: the pattern
// matches the value exactly or as a leading word-boundary prefix.
func cursorGlobPrefixMatch(pattern, value string) bool {
	matched, err := regexp.MatchString("^"+cursorGlobRegexp(pattern)+"( .*)?$", value)
	return err == nil && matched
}

func cursorGlobRegexp(pattern string) string {
	var expr strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			expr.WriteString(".*")
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
			}
		case '?':
			expr.WriteString(".")
		default:
			expr.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	return expr.String()
}

// writeFileAtomic writes via a temporary file and rename so concurrent
// invocations sharing a digest directory never expose a truncated config to
// a Cursor process that is starting up.
//
// #nosec G703 -- path is the runner-built private config file under the local
// user's cache directory, and the temp name comes from os.CreateTemp in that
// same directory; writing there is this helper's purpose.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
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

	root, err := cursorChatsRoot()
	if err != nil {
		return ""
	}
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
