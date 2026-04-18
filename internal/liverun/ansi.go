package liverun

import "io"

// ANSI escape sequence states.
const (
	ansiNormal    = iota
	ansiEscape    // saw ESC
	ansiEscapeInt // saw ESC + intermediate byte (e.g. '(' or ')'); consuming one final byte
	ansiCSI       // saw ESC [; consuming CSI param/intermediate bytes until final
	ansiOSC       // saw ESC ]; consuming until BEL or ESC \
	ansiOSCEsc    // ESC seen inside OSC (checking for \)
)

// ANSIStripper is an io.Writer that strips ANSI escape sequences from the
// byte stream before forwarding the cleaned text to dst. The state machine
// handles partial sequences across Write boundaries so no garbage passes
// through when a sequence is split between two calls.
type ANSIStripper struct {
	dst   io.Writer
	state int
}

// NewANSIStripper wraps dst, stripping ANSI CSI/OSC/SGR sequences.
func NewANSIStripper(dst io.Writer) *ANSIStripper {
	return &ANSIStripper{dst: dst}
}

// Write processes p, forwarding only non-escape bytes to the downstream writer.
// Always returns len(p), nil to satisfy io.Writer even if the downstream write
// returns a short count or error; errors in TUI delivery are non-fatal.
func (s *ANSIStripper) Write(p []byte) (int, error) {
	// We collect clean runs of bytes and batch-write them to avoid excessive
	// small calls to the downstream writer.
	start := -1 // start of current clean run; -1 = no pending run

	flush := func(end int) {
		if start >= 0 && end > start {
			_, _ = s.dst.Write(p[start:end])
		}
		start = -1
	}

	for i, b := range p {
		switch s.state {
		case ansiNormal:
			if b == 0x1B { // ESC
				flush(i)
				s.state = ansiEscape
			} else if start < 0 {
				start = i
			}

		case ansiEscape:
			switch {
			case b == '[':
				s.state = ansiCSI
			case b == ']':
				s.state = ansiOSC
			case b >= 0x20 && b <= 0x2F:
				// Intermediate byte (0x20-0x2F, e.g. '(' ')' '*' '+'). The next
				// byte is the final byte of a three-char sequence like
				// ESC ( B (charset designation) — consume it and return to normal.
				s.state = ansiEscapeInt
			default:
				// Fe/Fs/Fp two-character escape sequences (e.g. ESC M, ESC =,
				// ESC >, ESC 7, ESC 8): drop both the ESC and this byte.
				s.state = ansiNormal
			}

		case ansiEscapeInt:
			// Consume exactly one final byte and return to normal.
			s.state = ansiNormal

		case ansiCSI:
			// CSI final byte: 0x40–0x7E
			if b >= 0x40 && b <= 0x7E {
				s.state = ansiNormal
			}
			// Else: parameter or intermediate byte — keep consuming.

		case ansiOSC:
			switch b {
			case 0x07: // BEL terminates OSC
				s.state = ansiNormal
			case 0x1B: // ESC inside OSC — could be ST (ESC \)
				s.state = ansiOSCEsc
			}
			// Other bytes: keep consuming (part of OSC payload).

		case ansiOSCEsc:
			if b == '\\' {
				s.state = ansiNormal // ESC \ = ST
			} else {
				s.state = ansiOSC // Not ST; continue consuming OSC.
			}
		}
	}

	// Flush any trailing clean run.
	flush(len(p))

	return len(p), nil
}
