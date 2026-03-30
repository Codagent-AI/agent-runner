package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const (
	signalFile  = ".agent-runner-signal"
	debugLog    = "pty-poc.log"
	defaultTerm = "xterm-256color"
)

// Escape sequence parser states.  Covers all standard ANSI/xterm sequences:
//   - CSI  (\x1b[)  — parameter bytes + final byte (0x40-0x7e)
//   - OSC  (\x1b])  — payload terminated by BEL (0x07) or ST (\x1b\)
//   - DCS  (\x1bP)  — same termination as OSC
//   - PM   (\x1b^)  — same termination as OSC
//   - APC  (\x1b_)  — same termination as OSC
//   - SOS  (\x1bX)  — same termination as OSC
const (
	escNone      = iota
	escSawEsc    // saw 0x1b, waiting for next byte
	escInCSI     // inside CSI, waiting for final byte (0x40-0x7e)
	escInStringSeq // inside OSC/DCS/PM/APC/SOS, waiting for BEL or ST
)

var (
	activePTY       *os.File
	activeCmd       *exec.Cmd
	mode            = "home"
	shuttingDown    bool
	pendingContinue bool
	mu              sync.Mutex
	logFile         *os.File
	lineBuffer      []byte
	escState        int
)

func resolveCodexPath() (string, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf("could not resolve \"codex\" on PATH: %w", err)
	}
	return path, nil
}

func resolveClaudePath() (string, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("could not resolve \"claude\" on PATH: %w", err)
	}
	return path, nil
}

func clearScreen() {
	fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
}

func restoreTerminalModes() {
	fmt.Fprint(os.Stdout, "\x1b[<u")
	fmt.Fprint(os.Stdout, "\x1b[>4;0m")
	fmt.Fprint(os.Stdout, "\x1b[?1004l")
	fmt.Fprint(os.Stdout, "\x1b[?2004l")
	fmt.Fprint(os.Stdout, "\x1b[?25h")
}

func ensureTermEnv(env []string) []string {
	for _, entry := range env {
		if strings.HasPrefix(entry, "TERM=") {
			return env
		}
	}
	return append(env, "TERM="+defaultTerm)
}

func resetDebugLog() {
	var err error
	logFile, err = os.Create(debugLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create debug log: %v\n", err)
	}
}

func logDebug(message string) {
	if logFile == nil {
		return
	}
	fmt.Fprintf(logFile, "[%s] %s\n", time.Now().Format(time.RFC3339Nano), message)
}

func describeChunk(data []byte) string {
	hexParts := make([]string, len(data))
	for i, b := range data {
		hexParts[i] = fmt.Sprintf("%02x", b)
	}
	jsonBytes, _ := json.Marshal(string(data))
	return fmt.Sprintf("hex=%s text=%s", strings.Join(hexParts, " "), string(jsonBytes))
}

func renderHome() {
	mu.Lock()
	mode = "home"
	mu.Unlock()

	clearScreen()
	fmt.Fprintln(os.Stdout, "Agent Runner PTY POC")
	fmt.Fprintln(os.Stdout)
	cwd, _ := os.Getwd()
	fmt.Fprintf(os.Stdout, "cwd: %s\n\n", cwd)
	fmt.Fprintln(os.Stdout, "  c        launch Claude")
	fmt.Fprintln(os.Stdout, "  x        launch Codex")
	fmt.Fprintln(os.Stdout, "  esc      exit")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Inside an agent")
	fmt.Fprintln(os.Stdout, "  /next    continue back to this screen")
	fmt.Fprintln(os.Stdout, "  ctrl-]   continue back to this screen")
	fmt.Fprintf(os.Stdout, "\nDebug log: %s\n", debugLog)
}

func removeSignalFile() {
	os.Remove(signalFile) // #nosec G104 -- best-effort cleanup
}

func writeContinueSignal() {
	data, _ := json.Marshal(map[string]string{"action": "continue"})
	os.WriteFile(signalFile, data, 0o600) // #nosec G104 -- best-effort signal file write
}

// processAgentInput processes a stdin chunk, forwarding bytes to the PTY
// while tracking the current input line.  Returns true if a continue trigger
// was detected (Ctrl-], enhanced-keyboard Ctrl-], or "/next" followed by Enter).
//
// Bytes are accumulated and flushed to the PTY in batches to preserve the
// original chunk boundaries.  Writing byte-by-byte would break escape
// sequences because the receiving application may interpret a lone \x1b as
// a standalone Escape keypress.
func processAgentInput(chunk []byte) bool {
	// Check for enhanced-keyboard Ctrl-] encodings on the whole chunk first.
	text := string(chunk)
	if strings.Contains(text, "\x1b[93;5u") || strings.Contains(text, "\x1b[27;5;93~") {
		logDebug("matched continue shortcut (enhanced keyboard)")
		return true
	}

	mu.Lock()
	ptmx := activePTY
	mu.Unlock()

	flush := func(data []byte) {
		if ptmx != nil && len(data) > 0 {
			ptmx.Write(data) // #nosec G104 -- best-effort PTY write
		}
	}

	var buf []byte

	for _, b := range chunk {
		// Escape sequence state machine — consume full sequences without
		// touching lineBuffer.
		switch escState {
		case escSawEsc:
			switch b {
			case '[':
				escState = escInCSI
			case ']', 'P', '^', '_', 'X': // OSC, DCS, PM, APC, SOS
				escState = escInStringSeq
			default:
				escState = escNone // simple two-byte escape
			}
			buf = append(buf, b)
			continue
		case escInCSI:
			if b >= 0x40 && b <= 0x7e { // final byte
				escState = escNone
			}
			buf = append(buf, b)
			continue
		case escInStringSeq:
			if b == 0x07 { // BEL terminates
				escState = escNone
			} else if b == 0x1b { // start of ST (\x1b\)
				escState = escSawEsc
			}
			buf = append(buf, b)
			continue
		}

		// Start of a new escape sequence.
		if b == 0x1b {
			escState = escSawEsc
			buf = append(buf, b)
			continue
		}

		// Ctrl-] — flush what we have, then signal continue.
		if b == 0x1d {
			flush(buf)
			logDebug("matched continue shortcut (ctrl-])")
			return true
		}

		// Update line buffer and check for /next on Enter.
		switch {
		case b == '\r' || b == '\n':
			logDebug(fmt.Sprintf("enter pressed, lineBuffer=%q", string(lineBuffer)))
			if string(lineBuffer) == "/next" {
				// Flush everything before this Enter, but not the Enter itself.
				flush(buf)
				logDebug("matched /next command")
				lineBuffer = lineBuffer[:0]
				return true
			}
			lineBuffer = lineBuffer[:0]
		case b == 0x7f || b == 0x08: // backspace / delete
			if len(lineBuffer) > 0 {
				lineBuffer = lineBuffer[:len(lineBuffer)-1]
			}
		case b == 0x15: // Ctrl-U (kill line)
			lineBuffer = lineBuffer[:0]
		case b >= 0x20 && b < 0x7f: // printable ASCII
			lineBuffer = append(lineBuffer, b)
		}

		buf = append(buf, b)
	}

	flush(buf)
	return false
}

func continueOutOfAgent() {
	mu.Lock()
	if activePTY == nil || activeCmd == nil {
		mu.Unlock()
		return
	}
	pendingContinue = true
	cmd := activeCmd
	mu.Unlock()

	writeContinueSignal()
	logDebug("continue shortcut intercepted, sending SIGTERM to child")

	cmd.Process.Signal(syscall.SIGTERM) // #nosec G104 -- best-effort signal
}

func syncPTYSize() {
	mu.Lock()
	ptmx := activePTY
	mu.Unlock()
	if ptmx == nil {
		return
	}

	size, err := pty.GetsizeFull(os.Stdin)
	if err != nil {
		logDebug(fmt.Sprintf("failed to read stdin size: %v", err))
		return
	}
	if err := pty.Setsize(ptmx, size); err != nil {
		logDebug(fmt.Sprintf("failed to resize pty: %v", err))
	}
}

func startCodex() {
	removeSignalFile()
	mu.Lock()
	mode = "codex"
	pendingContinue = false
	mu.Unlock()
	lineBuffer = lineBuffer[:0]
	escState = escNone

	clearScreen()
	logDebug("launching codex")

	codexPath, err := resolveCodexPath()
	if err != nil {
		fmt.Fprintf(os.Stdout, "[agent-runner] failed to launch Codex: %v\r\n", err)
		renderHome()
		return
	}

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		prompt = "You are running inside an Agent Runner PTY proof of concept. Keep responses short."
	}

	cmd := exec.Command(codexPath, "--no-alt-screen", prompt) // #nosec G204,G702 -- PTY POC launches codex with user-provided prompt
	cmd.Env = ensureTermEnv(os.Environ())

	size, sizeErr := pty.GetsizeFull(os.Stdin)
	var ptmx *os.File
	if sizeErr == nil {
		ptmx, err = pty.StartWithSize(cmd, size)
	} else {
		logDebug(fmt.Sprintf("failed to read initial stdin size: %v", sizeErr))
		ptmx, err = pty.Start(cmd)
	}
	if err != nil {
		fmt.Fprintf(os.Stdout, "[agent-runner] failed to launch Codex: %v\r\n", err)
		renderHome()
		return
	}

	mu.Lock()
	activePTY = ptmx
	activeCmd = cmd
	mu.Unlock()

	// Read PTY output and forward to stdout.
	go func() {
		io.Copy(os.Stdout, ptmx) // #nosec G104 -- best-effort PTY→stdout copy
	}()

	// Wait for process exit.
	go func() {
		cmd.Wait() // #nosec G104 -- exit status handled via process state
		logDebug("codex pty exited")

		mu.Lock()
		activePTY = nil
		activeCmd = nil
		wasPending := pendingContinue
		pendingContinue = false
		isShuttingDown := shuttingDown
		mu.Unlock()

		if wasPending {
			restoreTerminalModes()
			renderHome()
		} else if !isShuttingDown {
			cleanupAndExit(0)
		}
	}()
}

func startClaude() {
	removeSignalFile()
	mu.Lock()
	mode = "claude"
	pendingContinue = false
	mu.Unlock()
	lineBuffer = lineBuffer[:0]
	escState = escNone

	clearScreen()
	logDebug("launching claude")

	claudePath, err := resolveClaudePath()
	if err != nil {
		fmt.Fprintf(os.Stdout, "[agent-runner] failed to launch Claude: %v\r\n", err)
		renderHome()
		return
	}

	cmd := exec.Command(claudePath) // #nosec G204 -- PTY POC launches claude interactively
	cmd.Env = ensureTermEnv(os.Environ())

	size, sizeErr := pty.GetsizeFull(os.Stdin)
	var ptmx *os.File
	if sizeErr == nil {
		ptmx, err = pty.StartWithSize(cmd, size)
	} else {
		logDebug(fmt.Sprintf("failed to read initial stdin size: %v", sizeErr))
		ptmx, err = pty.Start(cmd)
	}
	if err != nil {
		fmt.Fprintf(os.Stdout, "[agent-runner] failed to launch Claude: %v\r\n", err)
		renderHome()
		return
	}

	mu.Lock()
	activePTY = ptmx
	activeCmd = cmd
	mu.Unlock()

	// Read PTY output and forward to stdout.
	go func() {
		io.Copy(os.Stdout, ptmx) // #nosec G104 -- best-effort PTY→stdout copy
	}()

	// Wait for process exit.
	go func() {
		cmd.Wait() // #nosec G104 -- exit status handled via process state
		logDebug("claude pty exited")

		mu.Lock()
		activePTY = nil
		activeCmd = nil
		wasPending := pendingContinue
		pendingContinue = false
		isShuttingDown := shuttingDown
		mu.Unlock()

		if wasPending {
			restoreTerminalModes()
			renderHome()
		} else if !isShuttingDown {
			cleanupAndExit(0)
		}
	}()
}

func cleanupAndExit(code int) {
	mu.Lock()
	shuttingDown = true
	if activeCmd != nil {
		activeCmd.Process.Signal(syscall.SIGTERM) // #nosec G104 -- best-effort signal on shutdown
	}
	if activePTY != nil {
		activePTY.Close() // #nosec G104 -- best-effort PTY cleanup on exit
	}
	activePTY = nil
	activeCmd = nil
	mu.Unlock()

	restoreTerminalModes()
	if originalTermState != nil {
		restoreTerminal(os.Stdin.Fd(), originalTermState)
	}

	if logFile != nil {
		logFile.Close() // #nosec G104 -- best-effort cleanup on exit
	}

	fmt.Fprintln(os.Stdout)
	os.Exit(code)
}

func main() {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		fmt.Fprintln(os.Stderr, "This POC requires an interactive TTY.")
		os.Exit(1)
	}

	resetDebugLog()
	logDebug("poc started")

	// Set terminal to raw mode.
	oldState, err := makeRaw(os.Stdin.Fd())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to set raw mode: %v\n", err)
		os.Exit(1)
	}
	originalTermState = oldState
	defer restoreTerminal(os.Stdin.Fd(), oldState)

	// Handle signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		if sig == syscall.SIGINT {
			cleanupAndExit(130)
		}
		cleanupAndExit(143)
	}()

	resizeCh := make(chan os.Signal, 1)
	signal.Notify(resizeCh, syscall.SIGWINCH)
	go func() {
		for range resizeCh {
			syncPTYSize()
		}
	}()

	renderHome()

	// Read stdin.
	buf := make([]byte, 1024)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			cleanupAndExit(0)
		}
		chunk := buf[:n]

		logDebug(fmt.Sprintf("stdin mode=%s %s", mode, describeChunk(chunk)))

		mu.Lock()
		currentMode := mode
		mu.Unlock()

		if currentMode == "home" {
			for _, b := range chunk {
				if b == 0x03 { // Ctrl-C
					cleanupAndExit(130)
				}
				if b == 0x1b { // Esc
					cleanupAndExit(0)
				}
				if b == 'c' || b == 'C' {
					startClaude()
					break
				}
				if b == 'x' || b == 'X' {
					startCodex()
					break
				}
			}
		} else {
			logDebug(fmt.Sprintf("%s stdin %s", currentMode, describeChunk(chunk)))

			if processAgentInput(chunk) {
				continueOutOfAgent()
			}
		}
	}
}
