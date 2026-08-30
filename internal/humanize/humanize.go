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

// Size renders a byte count with a decimal: "16.2G", "512B".
//
// Bytes is the compact form, for a table column where every cell has to be
// the same handful of characters wide. Size is for status lines and prose,
// where "16.2G" tells the reader something that "16G" does not — and where
// two values shown side by side, used against total, want the same precision.
func Size(n int64) string { return scaled(float64(n), "") }

// Rate renders bytes per second, e.g. "1.2M/s".
//
// It keeps a decimal only below ten units. Throughput is noisier than a
// standing size, and a digit that changes every tick without changing what
// the reader does is churn rather than precision.
func Rate(bytesPerSec float64) string { return scaledRate(bytesPerSec) }

func scaledRate(v float64) string {
	const unit = 1024
	exp := 0
	for v >= unit && exp < 5 {
		v, exp = v/unit, exp+1
	}
	suffix := "B/s"
	if exp > 0 {
		suffix = string("KMGTPE"[exp-1]) + "/s"
	}
	if v < 10 && exp > 0 {
		return fmt.Sprintf("%.1f%s", v, suffix)
	}
	return fmt.Sprintf("%.0f%s", v, suffix)
}

// scaled divides down to the largest unit the value fills, keeping a decimal
// for everything above bytes. Whole bytes never get one: "512.0B" is noise.
func scaled(v float64, suffix string) string {
	const unit = 1024
	exp := 0
	for v >= unit && exp < 5 {
		v, exp = v/unit, exp+1
	}
	if exp == 0 {
		return fmt.Sprintf("%.0fB%s", v, suffix)
	}
	return fmt.Sprintf("%.1f%c%s", v, "KMGTPE"[exp-1], suffix)
}

// Duration renders a span as its two largest non-zero units: "6d 4h",
// "23h 49m", "12m 30s", "45s".
//
// RelTime's single unit is right for "how long ago", where the reader wants a
// rough answer. An uptime or an elapsed time is read more closely than that,
// and "6d" alone throws away most of what the reader asked for.
func Duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}

	units := []struct {
		size time.Duration
		name string
	}{
		{24 * time.Hour, "d"},
		{time.Hour, "h"},
		{time.Minute, "m"},
		{time.Second, "s"},
	}

	var parts []string
	for i, u := range units {
		n := d / u.size
		if n == 0 && len(parts) == 0 && i < len(units)-1 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d%s", n, u.name))
		if len(parts) == 2 {
			break
		}
		d -= n * u.size
	}
	return strings.Join(parts, " ")
}
