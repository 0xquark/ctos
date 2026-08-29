// Package hackernews shows the Hacker News front page via the Algolia API.
package hackernews

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/0xquark/ctos/internal/humanize"
	"github.com/0xquark/ctos/internal/theme"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func init() { widget.Register("hackernews", New) }

// apiURL returns the front page. Algolia serves the whole page in one request,
// unlike the official Firebase API which needs one call per story.
const apiURL = "https://hn.algolia.com/api/v1/search?tags=front_page&hitsPerPage=%d"

// itemURL is where a story's comment thread lives.
const itemURL = "https://news.ycombinator.com/item?id=%s"

const requestTimeout = 15 * time.Second

type config struct {
	Limit   int    `yaml:"limit"`
	Refresh string `yaml:"refresh"`
	Title   string `yaml:"title"`
}

type story struct {
	ID       string
	Title    string
	URL      string
	Points   int
	Comments int
	Author   string
	Created  time.Time
}

// Link is the story's own URL, falling back to its HN thread for text posts.
func (s story) Link() string {
	if s.URL != "" {
		return s.URL
	}
	return fmt.Sprintf(itemURL, s.ID)
}

type loadedMsg struct {
	name    string
	stories []story
	err     error
}

type refreshMsg struct{ name string }

// HackerNews lists front-page stories.
type HackerNews struct {
	widget.Base
	name    string
	cfg     config
	theme   theme.Theme
	refresh time.Duration

	stories []story
	cursor  int
	offset  int
	err     error
	loading bool
	fetched time.Time
}

// New builds a hackernews widget from its dashboard configuration.
func New(ctx widget.Context) (widget.Widget, error) {
	cfg := config{Limit: 20, Title: "hacker news"}
	if ctx.Node != nil {
		if err := ctx.Node.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("hackernews %q: %w", ctx.Name, err)
		}
	}
	if cfg.Limit <= 0 || cfg.Limit > 100 {
		cfg.Limit = 20
	}

	refresh := ctx.DefaultRefresh
	if cfg.Refresh != "" {
		d, err := time.ParseDuration(cfg.Refresh)
		if err != nil {
			return nil, fmt.Errorf("hackernews %q: invalid refresh %q: use a form like \"5m\"", ctx.Name, cfg.Refresh)
		}
		refresh = d
	}
	// The front page changes slowly; polling faster is just rude to the API.
	if refresh < time.Minute {
		refresh = time.Minute
	}

	return &HackerNews{name: ctx.Name, cfg: cfg, theme: ctx.Theme, refresh: refresh}, nil
}

// Title is the label drawn in the widget frame.
func (h *HackerNews) Title() string { return h.cfg.Title }

// Init starts the first fetch.
func (h *HackerNews) Init() tea.Cmd {
	h.loading = true
	return h.fetch()
}

// Update handles navigation, fetch results and the refresh timer.
func (h *HackerNews) Update(msg tea.Msg) (widget.Widget, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.name != h.name {
			return h, nil
		}
		h.loading = false
		h.err = msg.err
		if msg.err == nil {
			h.stories = msg.stories
			h.fetched = time.Now()
			if h.cursor >= len(h.stories) {
				h.cursor = max(0, len(h.stories)-1)
			}
		}
		return h, h.scheduleRefresh()

	case refreshMsg:
		if msg.name != h.name || h.loading {
			return h, nil
		}
		h.loading = true
		return h, h.fetch()

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			h.move(-1)
		case "down", "j":
			h.move(1)
		case "home", "g":
			h.cursor = 0
		case "end", "G":
			h.cursor = max(0, len(h.stories)-1)
		case "r":
			if !h.loading {
				h.loading = true
				return h, h.fetch()
			}
		}
	}
	return h, nil
}

func (h *HackerNews) move(delta int) {
	if len(h.stories) == 0 {
		return
	}
	h.cursor = min(max(h.cursor+delta, 0), len(h.stories)-1)
}

// Actions exposes opening the selected story in the system browser.
func (h *HackerNews) Actions() []widget.Action {
	if len(h.stories) == 0 {
		return nil
	}
	return []widget.Action{{Name: "open", Run: h.open}}
}

// open launches the story URL in the system browser without suspending the
// TUI, since the browser is a separate window.
func (h *HackerNews) open() tea.Cmd {
	if h.cursor >= len(h.stories) {
		return nil
	}
	url := h.stories[h.cursor].Link()
	return func() tea.Msg {
		_ = openBrowser(url)
		return nil
	}
}

func openBrowser(url string) error {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		cmd = "xdg-open"
	}
	return exec.Command(cmd, url).Start()
}

func (h *HackerNews) scheduleRefresh() tea.Cmd {
	name := h.name
	return tea.Tick(h.refresh, func(time.Time) tea.Msg { return refreshMsg{name: name} })
}

// fetch queries Algolia off the UI goroutine.
func (h *HackerNews) fetch() tea.Cmd {
	name, limit := h.name, h.cfg.Limit
	return func() tea.Msg {
		stories, err := fetchStories(limit)
		return loadedMsg{name: name, stories: stories, err: err}
	}
}

// algoliaResponse mirrors only the fields we render.
type algoliaResponse struct {
	Hits []struct {
		ObjectID    string `json:"objectID"`
		Title       string `json:"title"`
		URL         string `json:"url"`
		Points      int    `json:"points"`
		NumComments int    `json:"num_comments"`
		Author      string `json:"author"`
		CreatedAt   string `json:"created_at"`
	} `json:"hits"`
}

func fetchStories(limit int) ([]story, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(apiURL, limit), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ctos")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach hacker news: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hacker news returned %s", resp.Status)
	}

	return decodeStories(resp.Body)
}

// decodeStories is split out so tests can feed it a fixture.
func decodeStories(r io.Reader) ([]story, error) {
	var payload algoliaResponse
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode hacker news response: %w", err)
	}

	out := make([]story, 0, len(payload.Hits))
	for _, hit := range payload.Hits {
		if hit.Title == "" {
			continue
		}
		created, _ := time.Parse(time.RFC3339, hit.CreatedAt)
		out = append(out, story{
			ID:       hit.ObjectID,
			Title:    hit.Title,
			URL:      hit.URL,
			Points:   hit.Points,
			Comments: hit.NumComments,
			Author:   hit.Author,
			Created:  created,
		})
	}
	return out, nil
}

// View renders the story list. Each story takes two lines when there is room:
// the title, then a dim metadata line.
func (h *HackerNews) View() string {
	switch {
	case h.err != nil && len(h.stories) == 0:
		return h.theme.BadStyle().Render("⚠ " + h.err.Error())
	case h.loading && len(h.stories) == 0:
		return h.theme.DimStyle().Render("loading hacker news…")
	case len(h.stories) == 0:
		return h.theme.DimStyle().Render("no stories")
	}

	perItem := 2
	if h.H < 4 {
		perItem = 1
	}
	visible := max(1, h.H/perItem)

	// Keep the cursor inside the window.
	if h.cursor < h.offset {
		h.offset = h.cursor
	}
	if h.cursor >= h.offset+visible {
		h.offset = h.cursor - visible + 1
	}
	if h.offset > max(0, len(h.stories)-visible) {
		h.offset = max(0, len(h.stories)-visible)
	}

	var b strings.Builder
	end := min(h.offset+visible, len(h.stories))
	for i := h.offset; i < end; i++ {
		if i > h.offset {
			b.WriteByte('\n')
		}
		b.WriteString(h.titleLine(h.stories[i], i, i == h.cursor))
		if perItem == 2 {
			b.WriteByte('\n')
			b.WriteString(h.metaLine(h.stories[i]))
		}
	}
	return b.String()
}

// titleLine renders the rank, selection marker and story title.
func (h *HackerNews) titleLine(s story, index int, selected bool) string {
	rank := fmt.Sprintf("%2d ", index+1)

	marker := "  "
	titleStyle := h.theme.TextStyle()
	if selected {
		marker = "▸ "
		titleStyle = h.theme.AccentStyle().Bold(true)
		if !h.Focused() {
			titleStyle = h.theme.TextStyle().Bold(true)
		}
	}

	// Measured in display cells: "▸ " is two cells but four bytes.
	width := h.W - lipgloss.Width(rank) - lipgloss.Width(marker)
	if width < 1 {
		return humanize.Truncate(s.Title, h.W)
	}
	return h.theme.FaintStyle().Render(rank) +
		h.theme.FaintStyle().Render(marker) +
		titleStyle.Render(humanize.Truncate(s.Title, width))
}

// metaLine renders points, comments, domain and age, colouring the score by
// magnitude so a hot story is visible at a glance.
func (h *HackerNews) metaLine(s story) string {
	parts := []string{h.scoreStyle(s.Points).Render(strconv.Itoa(s.Points) + " pts")}

	if s.Comments > 0 {
		parts = append(parts, h.theme.DimStyle().Render(strconv.Itoa(s.Comments)+" comments"))
	}
	if d := humanize.Domain(s.URL); d != "" {
		parts = append(parts, h.theme.DimStyle().Render(d))
	}
	if !s.Created.IsZero() {
		parts = append(parts, h.theme.FaintStyle().Render(humanize.RelTime(s.Created)))
	}

	indent := "     "
	line := indent + strings.Join(parts, h.theme.FaintStyle().Render(" · "))
	if lipgloss.Width(line) > h.W {
		// Drop the domain first; it is the least useful field.
		line = indent + strings.Join(parts[:min(2, len(parts))], h.theme.FaintStyle().Render(" · "))
	}
	return line
}

func (h *HackerNews) scoreStyle(points int) lipgloss.Style {
	switch {
	case points >= 300:
		return h.theme.AccentStyle().Bold(true)
	case points >= 100:
		return h.theme.WarnStyle()
	default:
		return h.theme.DimStyle()
	}
}
