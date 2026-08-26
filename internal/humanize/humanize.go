// Package humanize formats values for display in a terminal.
package humanize

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RelTime renders a duration since t as a compact relative string: "3m", "2h",
// "5d". Times in the future render as "now".
func RelTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 0:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

// Bytes renders a byte count as a short human-readable size.
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.0f%c", float64(n)/float64(div), "KMGTPE"[exp])
}

// Domain extracts a bare display domain from a URL, dropping any "www."
// prefix. It returns "" when raw is empty or unparseable.
func Domain(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

// Truncate shortens s to at most w display cells, appending an ellipsis when it
// had to cut. It counts runes, so it is safe for non-ASCII text but does not
// account for double-width characters.
func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}
