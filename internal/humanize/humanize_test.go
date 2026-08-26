package humanize

import (
	"testing"
	"time"
)

func TestRelTime(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{"future", now.Add(time.Hour), "now"},
		{"seconds", now.Add(-30 * time.Second), "30s"},
		{"minutes", now.Add(-5 * time.Minute), "5m"},
		{"hours", now.Add(-3 * time.Hour), "3h"},
		{"days", now.Add(-48 * time.Hour), "2d"},
		{"years", now.Add(-800 * 24 * time.Hour), "2y"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RelTime(tc.when); got != tc.want {
				t.Errorf("RelTime = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDomain(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://www.example.com/a/b", "example.com"},
		{"https://blog.example.co.uk/post", "blog.example.co.uk"},
		{"", ""},
		{"not a url", ""},
		{"https://example.com", "example.com"},
	}
	for _, tc := range tests {
		if got := Domain(tc.in); got != tc.want {
			t.Errorf("Domain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		w    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 4, "hel…"},
		{"hello", 1, "…"},
		{"hello", 0, ""},
		{"hello", -1, ""},
		{"héllo wörld", 6, "héllo…"},
	}
	for _, tc := range tests {
		if got := Truncate(tc.in, tc.w); got != tc.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
		}
	}
}

func TestBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{512, "512B"},
		{1024, "1K"},
		{1536, "2K"},
		{1048576, "1M"},
	}
	for _, tc := range tests {
		if got := Bytes(tc.in); got != tc.want {
			t.Errorf("Bytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
