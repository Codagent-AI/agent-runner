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
	debugLog    = "codex-pty-poc.log"
	defaultTerm = "xterm-256color"
)

var (
	activePTY       *os.File
	activeCmd       *exec.Cmd
	mode            = "home"
	shuttingDown    bool
	pendingContinue bool
	mu              sync.Mutex
	logFile         *os.File
)

func resolveCodexPath() (string, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf("could not resolve \"codex\" on PATH: %w", err)
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
	fmt.Fprintln(os.Stdout, "Agent Runner PTY Codex POC")
	fmt.Fprintln(os.Stdout)
	cwd, _ := os.Getwd()
	fmt.Fprintf(os.Stdout, "cwd: %s\n\n", cwd)
	fmt.Fprintln(os.Stdout, "Controls")
	fmt.Fprintln(os.Stdout, "  space  open Codex in a PTY")
	fmt.Fprintln(os.Stdout, "  esc    exit this POC")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Inside Codex")
	fmt.Fprintln(os.Stdout, "  ctrl-] continue back to this screen and write .agent-runner-signal")
	fmt.Fprintf(os.Stdout, "\nDebug log: %s\n", debugLog)
}

func removeSignalFile() {
	os.Remove(signalFile) // #nosec G104 -- best-effort cleanup
}

func writeContinueSignal() {
	data, _ := json.Marshal(map[string]string{"action": "continue"})
	os.WriteFile(signalFile, data, 0o600) // #nosec G104 -- best-effort signal file write
}

func isContinueShortcut(data []byte) bool {
	for _, b := range data {
		if b == 0x1d { // Ctrl-]
			return true
		}
	}
	text := string(data)
	return strings.Contains(text, "\x1b[93;5u") || strings.Contains(text, "\x1b[27;5;93~")
}

func requestGracefulExitFromCodex() {
	mu.Lock()
	ptmx := activePTY
	mu.Unlock()
	if ptmx == nil {
		return
	}

	logDebug("sending graceful codex exit sequence")

	// Esc interrupts an active run back to the prompt.
	ptmx.Write([]byte("\x1b")) // #nosec G104 -- best-effort PTY write

	// Ctrl-U clears any partially typed command so Ctrl-D sees an empty prompt.
	time.AfterFunc(75*time.Millisecond, func() {
		mu.Lock()
		p := activePTY
		mu.Unlock()
		if p != nil {
			p.Write([]byte("\x15")) // #nosec G104 -- best-effort PTY write
		}
	})

	// Ctrl-D at an empty prompt exits Codex cleanly.
	time.AfterFunc(150*time.Millisecond, func() {
		mu.Lock()
		p := activePTY
		mu.Unlock()
		if p != nil {
			p.Write([]byte("\x04")) // #nosec G104 -- best-effort PTY write
		}
	})

	// Timeout warning.
	time.AfterFunc(2*time.Second, func() {
		mu.Lock()
		p := activePTY
		mu.Unlock()
		if p != nil {
			logDebug("graceful exit timeout expired")
			fmt.Fprint(os.Stdout, "\r\n[agent-runner] Codex did not exit yet. Press Ctrl-] again or exit Codex manually.\r\n")
		}
	})
}

func continueOutOfCodex() {
	mu.Lock()
	if activePTY == nil {
		mu.Unlock()
		return
	}
	mu.Unlock()

	writeContinueSignal()
	logDebug("continue shortcut intercepted")

	mu.Lock()
	pendingContinue = true
	mu.Unlock()

	fmt.Fprint(os.Stdout, "\r\n[agent-runner] continue intercepted, asking Codex to exit...\r\n")
	requestGracefulExitFromCodex()
}

func startCodex() {
	removeSignalFile()
	mu.Lock()
	mode = "codex"
	pendingContinue = false
	mu.Unlock()

	clearScreen()
	fmt.Fprint(os.Stdout, "[agent-runner] launching Codex in PTY...\r\n")
	fmt.Fprint(os.Stdout, "[agent-runner] press Ctrl-] to continue back to Agent Runner\r\n\r\n")
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
	cmd.Env = os.Environ()

	ptmx, err := pty.Start(cmd)
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

		if wasPending || !isShuttingDown {
			restoreTerminalModes()
			renderHome()
		}
	}()
}

func cleanupAndExit(code int) {
	mu.Lock()
	shuttingDown = true
	if activeCmd != nil {
		activeCmd.Process.Signal(syscall.SIGTERM) // #nosec G104 -- best-effort signal on shutdown
		activePTY = nil
		activeCmd = nil
	}
	mu.Unlock()

	restoreTerminalModes()

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

		// Ctrl-C always exits.
		for _, b := range chunk {
			if b == 0x03 {
				cleanupAndExit(130)
			}
		}

		mu.Lock()
		currentMode := mode
		mu.Unlock()

		if currentMode == "home" {
			for _, b := range chunk {
				if b == 0x1b { // Esc
					cleanupAndExit(0)
				}
				if b == 0x20 { // Space
					startCodex()
					break
				}
			}
		} else {
			logDebug(fmt.Sprintf("codex stdin %s", describeChunk(chunk)))

			if isContinueShortcut(chunk) {
				logDebug("matched continue shortcut")
				continueOutOfCodex()
				continue
			}

			mu.Lock()
			ptmx := activePTY
			mu.Unlock()
			if ptmx != nil {
				ptmx.Write(chunk) // #nosec G104 -- best-effort PTY write
			}
		}
	}
}
