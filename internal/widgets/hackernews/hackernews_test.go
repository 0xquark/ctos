package hackernews

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	"gopkg.in/yaml.v3"
)

func TestDecodeStories(t *testing.T) {
	f, err := os.Open("testdata/frontpage.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stories, err := decodeStories(f)
	if err != nil {
		t.Fatal(err)
	}

	// The untitled hit is dropped: a story with no title cannot be rendered.
	if len(stories) != 2 {
		t.Fatalf("got %d stories, want 2", len(stories))
	}

	first := stories[0]
	if first.Title != "Show HN: ctOS, a terminal control plane" {
		t.Errorf("title = %q", first.Title)
	}
	if first.Points != 412 || first.Comments != 137 {
		t.Errorf("points/comments = %d/%d, want 412/137", first.Points, first.Comments)
	}
	if first.Link() != "https://github.com/0xquark/ctos" {
		t.Errorf("link = %q", first.Link())
	}
	want := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	if !first.Created.Equal(want) {
		t.Errorf("created = %v, want %v", first.Created, want)
	}

	// A text post has no URL, so its link is the HN thread.
	if got := stories[1].Link(); got != "https://news.ycombinator.com/item?id=40000002" {
		t.Errorf("text post link = %q, want the HN item page", got)
	}
}

func TestDecodeStoriesRejectsGarbage(t *testing.T) {
	if _, err := decodeStories(strings.NewReader("not json")); err == nil {
		t.Error("expected an error for malformed JSON")
	}
}

func newWidget(t *testing.T, yamlSrc string) (*HackerNews, error) {
	t.Helper()
	ctx := widget.Context{Name: "hn", Theme: theme.New(""), DefaultRefresh: 30 * time.Second}
	if yamlSrc != "" {
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(yamlSrc), &node); err != nil {
			t.Fatal(err)
		}
		ctx.Node = node.Content[0]
	}
	w, err := New(ctx)
	if err != nil {
		return nil, err
	}
	return w.(*HackerNews), nil
}

func TestRefreshFloor(t *testing.T) {
	// A sub-minute interval is clamped: the front page does not change that
	// fast and the API should not be hammered.
	h, err := newWidget(t, "type: hackernews\nrefresh: 5s\n")
	if err != nil {
		t.Fatal(err)
	}
	if h.refresh != time.Minute {
		t.Errorf("refresh = %v, want it clamped to 1m", h.refresh)
	}

	h, err = newWidget(t, "type: hackernews\nrefresh: 10m\n")
	if err != nil {
		t.Fatal(err)
	}
	if h.refresh != 10*time.Minute {
		t.Errorf("refresh = %v, want 10m", h.refresh)
	}
}

func TestInvalidRefreshIsAnError(t *testing.T) {
	if _, err := newWidget(t, "type: hackernews\nrefresh: soon\n"); err == nil {
		t.Error("expected an error for an unparseable refresh interval")
	}
}

func TestLimitBounds(t *testing.T) {
	for _, tc := range []struct {
		yaml string
		want int
	}{
		{"type: hackernews\nlimit: 0\n", 20},
		{"type: hackernews\nlimit: -5\n", 20},
		{"type: hackernews\nlimit: 500\n", 20},
		{"type: hackernews\nlimit: 30\n", 30},
	} {
		h, err := newWidget(t, tc.yaml)
		if err != nil {
			t.Fatal(err)
		}
		if h.cfg.Limit != tc.want {
			t.Errorf("%q -> limit %d, want %d", tc.yaml, h.cfg.Limit, tc.want)
		}
	}
}

// TestMessagesAreScopedByName guards the broadcast model: a widget must ignore
// another widget's async results.
func TestMessagesAreScopedByName(t *testing.T) {
	h, err := newWidget(t, "type: hackernews\n")
	if err != nil {
		t.Fatal(err)
	}
	h.loading = true

	updated, _ := h.Update(loadedMsg{name: "someone-else", stories: []story{{Title: "x"}}})
	if got := updated.(*HackerNews); len(got.stories) != 0 || !got.loading {
		t.Error("widget consumed a message addressed to a different widget")
	}

	updated, _ = h.Update(loadedMsg{name: "hn", stories: []story{{Title: "mine"}}})
	if got := updated.(*HackerNews); len(got.stories) != 1 || got.loading {
		t.Error("widget ignored its own message")
	}
}
