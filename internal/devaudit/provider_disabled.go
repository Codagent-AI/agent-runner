//go:build !dev_audit

package devaudit

import "io"

// HandleCommand is inert in production builds. Keeping this tiny provider lets
// the CLI keep one integration boundary without exposing a command or asset.
func HandleCommand(_ []string, _ io.Writer, _ io.Writer) (bool, int) { return false, 0 }

// Enabled reports whether this binary contains the private audit capability.
func Enabled() bool { return false }
