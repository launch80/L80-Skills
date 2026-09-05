// Package output renders CLI results for two audiences: a human reading a
// terminal, and a model reading stdout. Both need the credential to stay hidden.
package output

import (
	"fmt"
	"io"
	"strings"
)

// Mask reduces a secret to a recognisable but unusable fragment.
//
// The CLI must never print a token in full -- not under --verbose, not in an
// error dump. A transcript pasted into a bug report (or sitting in a model's
// context) should never carry a working credential.
func Mask(secret string) string {
	if secret == "" {
		return "(none)"
	}
	if len(secret) <= 8 {
		return strings.Repeat("*", len(secret))
	}
	return secret[:min(8, len(secret))] + "..." + secret[len(secret)-4:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Successf writes a human-readable success line to stdout.
func Successf(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, format+"\n", args...)
}

// Detailf writes an indented supporting line.
func Detailf(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "  "+format+"\n", args...)
}
