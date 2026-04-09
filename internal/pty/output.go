package pty

const sentinelPayload = "999;red-slippers"

// outOSCSawEsc is an additional parser state for outputProcessor only:
// inside an OSC sequence (\x1b]...), a \x1b byte was seen — it may be the
// start of a string terminator (ST = \x1b\).
const outOSCSawEsc = 10

// outputProcessor tracks ANSI escape sequence state and detects the sentinel
// OSC sequence \x1b]999;red-slippers\x07 in the output stream.
//
// The sentinel is stripped from output (never forwarded to the terminal).
// All other bytes, including non-matching OSC sequences, are forwarded as-is.
// State persists across process() calls so sentinels split across PTY read
// chunk boundaries are detected correctly.
type outputProcessor struct {
	escState   int
	escBuf     []byte // bytes accumulated for the current escape sequence
	oscPayload []byte // OSC payload bytes (between \x1b] and BEL/ST)
}

// outputResult holds the outcome of processing an output chunk.
type outputResult struct {
	forward   []byte // bytes to write to os.Stdout
	triggered bool   // true if the sentinel was detected
}

// process processes a chunk of output bytes, detecting and stripping the
// sentinel OSC sequence. Returns the bytes to forward and whether the sentinel
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
				p.escState = outOSCSawEsc
			default:
				p.escBuf = append(p.escBuf, b)
				p.oscPayload = append(p.oscPayload, b)
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
			}
			continue
		}

		// escNone: normal byte.
		if b == 0x1b {
			p.escBuf = append(p.escBuf[:0], b)
			p.escState = escSawEsc
		} else {
			fwd = append(fwd, b)
		}
	}

	return outputResult{forward: fwd, triggered: triggered}
}

// flush returns any bytes buffered in a partial escape sequence as normal
// output. Call this when the process exits to avoid dropping partial sequences.
func (p *outputProcessor) flush() []byte {
	if len(p.escBuf) == 0 {
		return nil
	}
	out := append([]byte(nil), p.escBuf...)
	p.escBuf = p.escBuf[:0]
	p.oscPayload = p.oscPayload[:0]
	p.escState = escNone
	return out
}
