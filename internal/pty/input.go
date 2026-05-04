package pty

import "strings"

// Escape sequence parser states. Covers all standard ANSI/xterm sequences:
//   - CSI  (\x1b[)  — parameter bytes + final byte (0x40-0x7e)
//   - OSC  (\x1b])  — payload terminated by BEL (0x07) or ST (\x1b\)
//   - DCS  (\x1bP)  — same termination as OSC
//   - PM   (\x1b^)  — same termination as OSC
//   - APC  (\x1b_)  — same termination as OSC
//   - SOS  (\x1bX)  — same termination as OSC
const (
	escNone        = iota
	escSawEsc      // saw 0x1b, waiting for next byte
	escInCSI       // inside CSI, waiting for final byte (0x40-0x7e)
	escInStringSeq // inside OSC/DCS/PM/APC/SOS, waiting for BEL or ST
)

// inputProcessor tracks ANSI escape sequence state and detects continue
// triggers (/next + Enter, Ctrl-], enhanced-keyboard Ctrl-]).
type inputProcessor struct {
	lineBuffer []byte
	escState   int
	escBuf     []byte
}

// processResult holds the outcome of processing an input chunk.
type processResult struct {
	forward   []byte // bytes to write to the PTY
	triggered bool   // true if a continue trigger was detected
}

// process processes a chunk of input bytes, tracking escape sequence state
// and detecting continue triggers. Returns the bytes to forward to the PTY
// and whether a continue trigger was detected.
//
// Bytes are batched to preserve escape sequence integrity — writing byte-by-byte
// would break sequences because the CLI may interpret a lone \x1b as a
// standalone Escape keypress.
func (p *inputProcessor) process(chunk []byte) processResult {
	// Check for enhanced-keyboard Ctrl-] encodings on the whole chunk first.
	text := string(chunk)
	if strings.Contains(text, "\x1b[93;5u") || strings.Contains(text, "\x1b[27;5;93~") {
		return processResult{triggered: true}
	}

	var buf []byte

	for _, b := range chunk {
		// Escape sequence state machine — consume full sequences without
		// touching lineBuffer.
		if p.escState != escNone {
			buf = append(buf, p.processEscapeByte(b)...)
			continue
		}

		// Start of a new escape sequence.
		if b == 0x1b {
			p.escState = escSawEsc
			p.escBuf = append(p.escBuf[:0], b)
			continue
		}

		// Ctrl-] — return bytes accumulated so far, signal continue.
		if b == 0x1d {
			return processResult{forward: buf, triggered: true}
		}

		// Update line buffer and check for /next on Enter.
		switch {
		case b == '\r' || b == '\n':
			if string(p.lineBuffer) == "/next" {
				p.lineBuffer = p.lineBuffer[:0]
				return processResult{forward: buf, triggered: true}
			}
			p.lineBuffer = p.lineBuffer[:0]
		case b == 0x7f || b == 0x08: // backspace / delete
			if len(p.lineBuffer) > 0 {
				p.lineBuffer = p.lineBuffer[:len(p.lineBuffer)-1]
			}
		case b == 0x15: // Ctrl-U (kill line)
			p.lineBuffer = p.lineBuffer[:0]
		case b >= 0x20 && b < 0x7f: // printable ASCII
			p.lineBuffer = append(p.lineBuffer, b)
		}

		buf = append(buf, b)
	}

	return processResult{forward: buf}
}

func (p *inputProcessor) processEscapeByte(b byte) []byte {
	switch p.escState {
	case escSawEsc:
		p.escBuf = append(p.escBuf, b)
		switch b {
		case '[':
			p.escState = escInCSI
			return nil
		case ']', 'P', '^', '_', 'X': // OSC, DCS, PM, APC, SOS
			p.escState = escInStringSeq
		default:
			p.escState = escNone // simple two-byte escape
		}
		out := append([]byte(nil), p.escBuf...)
		p.escBuf = p.escBuf[:0]
		return out
	case escInCSI:
		p.escBuf = append(p.escBuf, b)
		if b < 0x40 || b > 0x7e {
			return nil
		}
		out := []byte(nil)
		if !isSGRMouseInput(p.escBuf) {
			out = append(out, p.escBuf...)
		}
		p.escBuf = p.escBuf[:0]
		p.escState = escNone
		return out
	case escInStringSeq:
		switch b {
		case 0x07: // BEL terminates
			p.escState = escNone
		case 0x1b: // start of ST (\x1b\)
			p.escState = escSawEsc
		}
		return []byte{b}
	default:
		return nil
	}
}

func isSGRMouseInput(seq []byte) bool {
	if len(seq) < 6 {
		return false
	}
	final := seq[len(seq)-1]
	return seq[0] == 0x1b && seq[1] == '[' && seq[2] == '<' && (final == 'M' || final == 'm')
}
