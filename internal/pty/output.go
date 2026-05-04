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
	escState          int
	escBuf            []byte // bytes accumulated for the current escape sequence
	oscPayload        []byte // OSC payload bytes (between \x1b] and BEL/ST)
	textBuf           []byte // possible prefix of a text continuation marker in normal output
	textStartBoundary bool   // textBuf started at a visible token boundary
	textSawVisible    bool   // normal visible text has been seen since the last line break
	textPrevBoundary  bool   // the previous normal visible byte was a token boundary
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
			p.processEscByte(b, &fwd)
			continue

		case escInCSI:
			p.processCSIByte(b, &fwd)
			continue

		case escInStringSeq:
			if p.processStringSeqByte(b, &fwd) {
				triggered = true
			}
			continue

		case outOSCSawEsc:
			p.processOSCSawEscByte(b, &fwd)
			continue
		}

		if p.processNormalByte(b, &fwd) {
			triggered = true
		}
	}

	return outputResult{forward: fwd, triggered: triggered}
}

func (p *outputProcessor) processEscByte(b byte, fwd *[]byte) {
	p.escBuf = append(p.escBuf, b)
	switch b {
	case ']':
		p.escState = escInStringSeq
		p.oscPayload = p.oscPayload[:0]
	case '[':
		p.escState = escInCSI
	default:
		// Simple two-byte escape or unrecognised — flush and reset.
		*fwd = append(*fwd, p.escBuf...)
		p.escBuf = p.escBuf[:0]
		p.escState = escNone
	}
}

func (p *outputProcessor) processCSIByte(b byte, fwd *[]byte) {
	p.escBuf = append(p.escBuf, b)
	if b < 0x40 || b > 0x7e {
		return
	}
	*fwd = append(*fwd, p.escBuf...)
	if len(p.textBuf) == 0 && csiStartsTextCell(b) {
		p.markTextBoundary()
	}
	p.escBuf = p.escBuf[:0]
	p.escState = escNone
}

func (p *outputProcessor) processStringSeqByte(b byte, fwd *[]byte) bool {
	switch b {
	case 0x07: // BEL terminates OSC
		return p.finishOSCSequence(fwd)
	case 0x1b: // potential start of ST (\x1b\)
		p.escBuf = append(p.escBuf, b)
		*fwd = p.flushIfOverflow(*fwd)
		if p.escState == escInStringSeq { // not flushed
			p.escState = outOSCSawEsc
		}
	default:
		p.escBuf = append(p.escBuf, b)
		p.oscPayload = append(p.oscPayload, b)
		*fwd = p.flushIfOverflow(*fwd)
	}
	return false
}

func (p *outputProcessor) finishOSCSequence(fwd *[]byte) bool {
	if string(p.oscPayload) == sentinelPayload {
		// Sentinel matched — strip (don't forward), signal trigger.
		p.escBuf = p.escBuf[:0]
		p.oscPayload = p.oscPayload[:0]
		p.escState = escNone
		return true
	}
	// Non-matching OSC — flush the buffered bytes.
	p.escBuf = append(p.escBuf, 0x07)
	*fwd = append(*fwd, p.escBuf...)
	p.escBuf = p.escBuf[:0]
	p.oscPayload = p.oscPayload[:0]
	p.escState = escNone
	return false
}

func (p *outputProcessor) processOSCSawEscByte(b byte, fwd *[]byte) {
	p.escBuf = append(p.escBuf, b)
	if b == '\\' {
		// ST terminator — flush (our sentinel uses BEL, not ST).
		*fwd = append(*fwd, p.escBuf...)
		p.escBuf = p.escBuf[:0]
		p.oscPayload = p.oscPayload[:0]
		p.escState = escNone
		return
	}
	// Not ST — treat the buffered \x1b and this byte as OSC payload.
	p.oscPayload = append(p.oscPayload, 0x1b, b)
	p.escState = escInStringSeq
	*fwd = p.flushIfOverflow(*fwd)
}

func (p *outputProcessor) processNormalByte(b byte, fwd *[]byte) bool {
	if b == 0x1b {
		return p.startEscape(fwd)
	}
	var textTriggered bool
	*fwd, textTriggered = p.processTextByte(b, *fwd)
	return textTriggered
}

func (p *outputProcessor) startEscape(fwd *[]byte) bool {
	triggered := false
	switch {
	case isTextMarkerComplete(p.textBuf):
		p.textBuf = p.textBuf[:0]
		p.textStartBoundary = false
		p.textSawVisible = false
		p.textPrevBoundary = true
		triggered = true
	case len(p.textBuf) == 0 || isTextMarkerPrefix(p.textBuf, p.textStartBoundary):
		// Keep a potential marker buffered across styling/cursor sequences.
	default:
		*fwd = p.flushText(*fwd)
	}
	p.escBuf = append(p.escBuf[:0], 0x1b)
	p.escState = escSawEsc
	return triggered
}

func (p *outputProcessor) processTextByte(b byte, fwd []byte) ([]byte, bool) {
	if isLineTerminator(b) {
		if isTextMarkerComplete(p.textBuf) {
			p.textBuf = p.textBuf[:0]
			p.textStartBoundary = false
			p.textSawVisible = false
			p.textPrevBoundary = true
			return fwd, true
		}
		fwd = p.flushText(fwd)
		fwd = append(fwd, b)
		p.textSawVisible = false
		p.textPrevBoundary = true
		return fwd, false
	}

	if len(p.textBuf) == 0 {
		p.textStartBoundary = !p.textSawVisible || p.textPrevBoundary
	}
	p.textBuf = append(p.textBuf, b)

	for len(p.textBuf) > 0 {
		if suffix, ok := textMarkerTriggerSuffix(p.textBuf, p.textStartBoundary); ok {
			p.textBuf = p.textBuf[:0]
			p.textStartBoundary = false
			if len(suffix) > 0 {
				fwd = append(fwd, suffix...)
				p.recordVisibleBytes(suffix)
			} else {
				p.textSawVisible = false
				p.textPrevBoundary = true
			}
			return fwd, true
		}
		if isTextMarkerPrefix(p.textBuf, p.textStartBoundary) {
			return fwd, false
		}

		fwd = append(fwd, p.textBuf[0])
		p.recordVisibleByte(p.textBuf[0])
		p.textBuf = p.textBuf[1:]
		p.textStartBoundary = p.textPrevBoundary
	}
	return fwd, false
}

func (p *outputProcessor) flushText(fwd []byte) []byte {
	if len(p.textBuf) == 0 {
		return fwd
	}
	fwd = append(fwd, p.textBuf...)
	p.recordVisibleBytes(p.textBuf)
	p.textBuf = p.textBuf[:0]
	p.textStartBoundary = false
	return fwd
}

func (p *outputProcessor) finish() outputResult {
	if len(p.escBuf) == 0 && len(p.textBuf) == 0 {
		return outputResult{}
	}
	if len(p.escBuf) == 0 && isTextMarkerLine(p.textBuf) {
		p.textBuf = p.textBuf[:0]
		p.textOnLine = false
		p.escState = escNone
		return outputResult{triggered: true}
	}
	return outputResult{forward: p.flush()}
}

func isLineTerminator(b byte) bool {
	return b == '\n' || b == '\r'
}

func (p *outputProcessor) recordVisibleBytes(buf []byte) {
	for _, b := range buf {
		p.recordVisibleByte(b)
	}
}

func (p *outputProcessor) recordVisibleByte(b byte) {
	p.textSawVisible = true
	p.textPrevBoundary = isTextMarkerBoundaryByte(b)
}

func (p *outputProcessor) markTextBoundary() {
	p.textSawVisible = false
	p.textPrevBoundary = true
	p.textStartBoundary = false
}

func csiStartsTextCell(final byte) bool {
	switch final {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'J', 'K', 'f':
		return true
	default:
		return false
	}
}

func isTextMarkerComplete(buf []byte) bool {
	for _, marker := range textMarkerForms() {
		if bytes.HasPrefix(buf, marker) {
			return len(bytes.Trim(buf[len(marker):], " \t")) == 0
		}
	}
	return false
}

func isTextMarkerPrefix(buf []byte, startBoundary bool) bool {
	if len(buf) == 0 {
		return true
	}
	if !startBoundary {
		return false
	}
	for _, marker := range textMarkerForms() {
		if bytes.HasPrefix(marker, buf) {
			return true
		}
		if bytes.HasPrefix(buf, marker) {
			return len(bytes.Trim(buf[len(marker):], " \t")) == 0
		}
	}
	return false
}

func textMarkerTriggerSuffix(buf []byte, startBoundary bool) ([]byte, bool) {
	if !startBoundary {
		return nil, false
	}
	for _, marker := range textMarkerForms() {
		if !bytes.HasPrefix(buf, marker) {
			continue
		}
		rest := buf[len(marker):]
		trimmed := bytes.TrimLeft(rest, " \t")
		if len(trimmed) == 0 {
			return nil, false
		}
		if isTextMarkerBoundaryByte(trimmed[0]) {
			return trimmed, true
		}
	}
	return nil, false
}

func textMarkerForms() [][]byte {
	return [][]byte{
		[]byte(textSentinel),
		[]byte("• " + textSentinel),
	}
}

func isTextMarkerBoundaryByte(b byte) bool {
	return b != '_' && (b < '0' || b > '9') && (b < 'A' || b > 'Z') && (b < 'a' || b > 'z')
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
	p.textStartBoundary = false
	if len(out) > 0 {
		p.recordVisibleBytes(out)
	}
	p.escState = escNone
	return out
}
