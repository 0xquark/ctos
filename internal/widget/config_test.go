package widget

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type testConfig struct {
	Limit   int    `yaml:"limit"`
	Refresh string `yaml:"refresh"`
	Title   string `yaml:"title"`
	private int    //nolint:unused // guards that unexported fields stay hidden
}

// ctxFor builds a Context around a widget's YAML block, the way the dashboard
// loader does.
func ctxFor(t *testing.T, src string) Context {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return Context{Name: "hn", Type: "hackernews", Node: doc.Content[0]}
}

func TestDecodeKeepsDefaultsForAbsentKeys(t *testing.T) {
	cfg := testConfig{Limit: 20, Title: "hacker news"}
	if err := ctxFor(t, "type: hackernews\nlimit: 5\n").Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Limit != 5 {
		t.Errorf("limit = %d, want 5", cfg.Limit)
	}
	if cfg.Title != "hacker news" {
		t.Errorf("title = %q, want the default to survive", cfg.Title)
	}
}

// "type:" belongs to the dashboard loader, not to the widget, so it must not
// be reported as an unknown key.
func TestDecodeIgnoresShellKeys(t *testing.T) {
	var cfg testConfig
	if err := ctxFor(t, "type: hackernews\n").Decode(&cfg); err != nil {
		t.Fatalf("type: was treated as a widget key: %v", err)
	}
}

// The whole point of the strict decode: a typo must not be silently ignored.
func TestDecodeRejectsUnknownKeys(t *testing.T) {
	var cfg testConfig
	err := ctxFor(t, "type: hackernews\nlimt: 5\n").Decode(&cfg)
	if err == nil {
		t.Fatal("a misspelled key was accepted")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error does not point at the offending line: %v", err)
	}

	for _, want := range []string{`unknown key "limt"`, "limit", "refresh", "title"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "private") {
		t.Errorf("error leaked an unexported field: %v", err)
	}
}

func TestDecodeReportsWrongTypes(t *testing.T) {
	var cfg testConfig
	err := ctxFor(t, "type: hackernews\nlimit: lots\n").Decode(&cfg)
	if err == nil {
		t.Fatal("a string was accepted for an int field")
	}
	// The line is the one in the dashboard file, not in some rewritten copy.
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error does not point at the offending line: %v", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("error should be one line, got: %v", err)
	}
}

func TestDecodeWithoutAConfigBlock(t *testing.T) {
	cfg := testConfig{Limit: 20}
	if err := (Context{Name: "hn"}).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Limit != 20 {
		t.Errorf("limit = %d, want the default", cfg.Limit)
	}
}

func TestRefreshPrefersWidgetThenDefaultThenFloor(t *testing.T) {
	ctx := Context{Name: "p", Type: "processes", DefaultRefresh: 30 * time.Second}

	cases := []struct {
		name       string
		spec       string
		def, floor time.Duration
		want       time.Duration
	}{
		{"widget config wins", "10s", 3 * time.Second, time.Second, 10 * time.Second},
		{"widget default when unset", "", 3 * time.Second, time.Second, 3 * time.Second},
		{"dashboard default when no widget default", "", 0, time.Second, 30 * time.Second},
		{"floor clamps a too-eager config", "10ms", 3 * time.Second, 500 * time.Millisecond, 500 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ctx.Refresh(tc.spec, tc.def, tc.floor)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("refresh = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRefreshRejectsGarbage(t *testing.T) {
	ctx := Context{Name: "p", Type: "processes"}
	_, err := ctx.Refresh("every 5 minutes", 0, time.Second)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "refresh") {
		t.Errorf("error should name the key, got: %v", err)
	}
}
