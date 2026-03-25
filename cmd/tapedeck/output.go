package main

import (
	"fmt"
	"io"
)

// printf writes formatted output, discarding write errors (which are
// non-actionable for CLI stdout/stderr output).
func printf(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, format, args...) //nolint:errcheck // CLI output write errors are non-actionable
}

// println writes a line, discarding write errors.
func println(w io.Writer, args ...any) {
	fmt.Fprintln(w, args...) //nolint:errcheck // CLI output write errors are non-actionable
}
