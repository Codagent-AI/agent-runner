package pty

import "bytes"

const (
	sentinelPayload = "999;signal-continuation"
	textSentinel    = "AGENT_RUNNER_CONTINUE"

	// maxEscBuf caps the escape/OSC buffer to prevent unbounded memory growth
	// when the PTY stream contains an unterminated OSC sequence (e.g., a
	// runaway agent that emits \x1b] and never sends BEL or ST). When the
	// limit is exceeded the accumulated bytes are flushed as normal output.
	maxEscBuf = 8 * 1024
)

// outOSCSawEsc is an additional parser state for outputProcessor only:
// inside an OSC sequence (\x1b]...), a \x1b byte was seen — it may be the
// start of a string terminator (ST = \x1b\).
const outOSCSawEsc = 10

// outputProcessor tracks ANSI escape sequence state and detects continuation
// sentinels in the output stream. The OSC sentinel is kept for compatibility;
// the text sentinel is the default prompt path because it does not require the
// hosted agent CLI to run a shell command.
//
// Sentinels are stripped from output (never forwarded to the terminal). All
// other bytes, including non-matching OSC sequences, are forwarded as-is. State
// persists across process() calls so sentinels split across PTY read chunk
// boundaries are detected correctly.
type outputProcessor struct {
	escState   int
	escBuf     []byte // bytes accumulated for the current escape sequence
	oscPayload []byte // OSC payload bytes (between \x1b] and BEL/ST)
	textBuf    []byte // possible prefix of textSentinel in normal output
	textOnLine bool   // normal text has been seen since the last line break
}

// outputResult holds the outcome of processing an output chunk.
type outputResult struct {
	forward   []byte // bytes to write to os.Stdout
	triggered bool   // true if the sentinel was detected
}

// process processes a chunk of output bytes, detecting and stripping
// continuation sentinels. Returns the bytes to forward and whether a sentinel
// was detected. Processing continues after sentinel detection so that bytes
// surrounding the sentinel in the same chunk are forwarded normally.
func (p *outputProcessor) process(chunk []byte) outputResult {
	var fwd []byte
	triggered := false

	for _, b := range chunk {
		switch p.escState {
		case escSawEsc:
			p.escBuf = append(p.escBuf, b)
			switch b {
			case ']':
				p.escState = escInStringSeq
				p.oscPayload = p.oscPayload[:0]
			case '[':
				p.escState = escInCSI
			default:
				// Simple two-byte escape or unrecognised — flush and reset.
				fwd = append(fwd, p.escBuf...)
				p.escBuf = p.escBuf[:0]
				p.escState = escNone
			}
			continue

		case escInCSI:
			p.escBuf = append(p.escBuf, b)
			if b >= 0x40 && b <= 0x7e { // final byte
				fwd = append(fwd, p.escBuf...)
				p.escBuf = p.escBuf[:0]
				p.escState = escNone
			}
			continue

		case escInStringSeq:
			switch b {
			case 0x07: // BEL terminates OSC
				if string(p.oscPayload) == sentinelPayload {
					// Sentinel matched — strip (don't forward), signal trigger.
					p.escBuf = p.escBuf[:0]
					p.oscPayload = p.oscPayload[:0]
					p.escState = escNone
					triggered = true
				} else {
					// Non-matching OSC — flush the buffered bytes.
					p.escBuf = append(p.escBuf, b)
					fwd = append(fwd, p.escBuf...)
					p.escBuf = p.escBuf[:0]
					p.oscPayload = p.oscPayload[:0]
					p.escState = escNone
				}
			case 0x1b: // potential start of ST (\x1b\)
				p.escBuf = append(p.escBuf, b)
				fwd = p.flushIfOverflow(fwd)
				if p.escState == escInStringSeq { // not flushed
					p.escState = outOSCSawEsc
				}
			default:
				p.escBuf = append(p.escBuf, b)
				p.oscPayload = append(p.oscPayload, b)
				fwd = p.flushIfOverflow(fwd)
			}
			continue

		case outOSCSawEsc:
			p.escBuf = append(p.escBuf, b)
			if b == '\\' {
				// ST terminator — flush (our sentinel uses BEL, not ST).
				fwd = append(fwd, p.escBuf...)
				p.escBuf = p.escBuf[:0]
				p.oscPayload = p.oscPayload[:0]
				p.escState = escNone
			} else {
				// Not ST — treat the buffered \x1b and this byte as OSC payload.
				p.oscPayload = append(p.oscPayload, 0x1b, b)
				p.escState = escInStringSeq
				fwd = p.flushIfOverflow(fwd)
			}
			continue
		}

		// escNone: normal byte.
		if b == 0x1b {
			fwd = p.flushText(fwd)
			p.escBuf = append(p.escBuf[:0], b)
			p.escState = escSawEsc
		} else {
			var textTriggered bool
			fwd, textTriggered = p.processTextByte(b, fwd)
			if textTriggered {
				triggered = true
			}
		}
	}

	return outputResult{forward: fwd, triggered: triggered}
}

func (p *outputProcessor) processTextByte(b byte, fwd []byte) ([]byte, bool) {
	if isLineTerminator(b) {
		if isTextMarkerLine(p.textBuf) {
			p.textBuf = p.textBuf[:0]
			p.textOnLine = false
			return fwd, true
		}
		fwd = append(fwd, p.textBuf...)
		fwd = append(fwd, b)
		p.textBuf = p.textBuf[:0]
		p.textOnLine = false
		return fwd, false
	}

	p.textBuf = append(p.textBuf, b)
	if p.textOnLine || !isTextMarkerLinePrefix(p.textBuf) {
		fwd = append(fwd, p.textBuf...)
		p.textBuf = p.textBuf[:0]
		p.textOnLine = true
		return fwd, false
	}
	return fwd, false
}

func (p *outputProcessor) flushText(fwd []byte) []byte {
	if len(p.textBuf) == 0 {
		return fwd
	}
	fwd = append(fwd, p.textBuf...)
	p.textOnLine = true
	p.textBuf = p.textBuf[:0]
	return fwd
}

func isLineTerminator(b byte) bool {
	return b == '\n' || b == '\r'
}

func isTextMarkerLine(buf []byte) bool {
	line := bytes.TrimSpace(buf)
	if bytes.Equal(line, []byte(textSentinel)) {
		return true
	}
	if rest, ok := bytes.CutPrefix(line, []byte("•")); ok {
		return bytes.Equal(bytes.TrimSpace(rest), []byte(textSentinel))
	}
	return false
}

func isTextMarkerLinePrefix(buf []byte) bool {
	line := bytes.TrimLeft(buf, " \t")
	if len(line) == 0 {
		return true
	}
	for _, marker := range [][]byte{
		[]byte(textSentinel),
		[]byte("• " + textSentinel),
	} {
		if bytes.HasPrefix(marker, line) {
			return true
		}
	}
	return false
}

// flushIfOverflow checks whether escBuf exceeds maxEscBuf. If so, it appends
// the buffered bytes to fwd, resets the parser, and returns the updated slice.
// This centralises the overflow guard so the main process loop stays compact.
func (p *outputProcessor) flushIfOverflow(fwd []byte) []byte {
	if len(p.escBuf) <= maxEscBuf {
		return fwd
	}
	fwd = append(fwd, p.escBuf...)
	p.escBuf = p.escBuf[:0]
	p.oscPayload = p.oscPayload[:0]
	p.escState = escNone
	return fwd
}

// flush returns any bytes buffered in a partial escape sequence as normal
// output. Call this when the process exits to avoid dropping partial sequences.
func (p *outputProcessor) flush() []byte {
	if len(p.escBuf) == 0 && len(p.textBuf) == 0 {
		return nil
	}
	out := append([]byte(nil), p.escBuf...)
	out = append(out, p.textBuf...)
	p.escBuf = p.escBuf[:0]
	p.oscPayload = p.oscPayload[:0]
	p.textBuf = p.textBuf[:0]
	if len(out) > 0 {
		p.textOnLine = true
	}
	p.escState = escNone
	return out
}
