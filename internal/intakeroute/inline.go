package intakeroute

import (
	"bytes"
	"fmt"
)

// MaxInlineHandoffBytes bounds the handoff text interpolated into a launched
// run's prompt through {{intake_handoff}}.
//
// This is deliberately far below MaxHandoffBytes. That bound governs what a run
// may durably retain on disk; this one governs what is pasted into an agent's
// context window, where a megabyte would crowd out the conversation the handoff
// exists to inform. 8 KiB is roughly 2,000 tokens, which comfortably holds the
// agreed goal, constraints, and recommendation an intake session produces.
const MaxInlineHandoffBytes = 8 << 10

// InlineHandoffValue renders sealed handoff bytes as the value of the
// {{intake_handoff}} built-in.
//
// Contents within the bound are returned verbatim. A larger handoff degrades
// the prompt rather than failing the run: the leading portion is kept, cut at a
// line boundary so the agent never sees a sentence sheared mid-word, and a
// marker naming handoffPath tells the agent where the untruncated text lives.
func InlineHandoffValue(raw []byte, handoffPath string) string {
	if len(raw) <= MaxInlineHandoffBytes {
		return string(raw)
	}

	kept := raw[:MaxInlineHandoffBytes]
	if index := bytes.LastIndexByte(kept, '\n'); index >= 0 {
		kept = kept[:index]
	}
	return fmt.Sprintf("%s\n[handoff truncated at %d bytes; full handoff at %s]", kept, MaxInlineHandoffBytes, handoffPath)
}
