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

func TestRate(t *testing.T) {
	cases := map[float64]string{
		0:          "0B/s",
		512:        "512B/s",
		1024:       "1.0K/s",
		1536:       "1.5K/s",
		20 * 1024:  "20K/s",
		1258291:    "1.2M/s",
		1073741824: "1.0G/s",
	}
	for in, want := range cases {
		if got := Rate(in); got != want {
			t.Errorf("Rate(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestDuration(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Second:              "45s",
		90 * time.Second:              "1m 30s",
		23*time.Hour + 49*time.Minute: "23h 49m",
		6*24*time.Hour + 4*time.Hour:  "6d 4h",
		48 * time.Hour:                "2d 0h",
		0:                             "0s",
		-time.Second:                  "0s",
	}
	for in, want := range cases {
		if got := Duration(in); got != want {
			t.Errorf("Duration(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestSize(t *testing.T) {
	cases := map[int64]string{
		0:           "0B",
		512:         "512B",
		1024:        "1.0K",
		16 << 30:    "16.0G",
		17394617548: "16.2G",
		24 << 30:    "24.0G",
		3_500_000:   "3.3M",
	}
	for in, want := range cases {
		if got := Size(in); got != want {
			t.Errorf("Size(%d) = %q, want %q", in, got, want)
		}
	}
}

// Size and Bytes are both kept on purpose: one is for prose, one is for a
// column where every cell has to be the same width.
func TestSizeAndBytesDiffer(t *testing.T) {
	const n = 17394617548
	if Size(n) == Bytes(n) {
		t.Errorf("Size and Bytes both render %d as %q", n, Size(n))
	}
}
