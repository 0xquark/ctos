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

func init() {
	widget.Register(widget.Spec{
		Name:    "hackernews",
		Summary: "the Hacker News front page, enter opens a story in your browser",
		New:     New,
		Example: `type: hackernews
limit: 20                 # stories to fetch, 1-100
refresh: 5m               # never polls faster than a minute
title: hacker news`,
	})
}

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
	stories []story
	err     error
}

type refreshMsg struct{}

// HackerNews lists front-page stories.
type HackerNews struct {
	widget.Base
	cfg     config
	theme   theme.Theme
	refresh time.Duration

	stories []story
	list    widget.List
	err     error
	loading bool
	fetched time.Time
}

// New builds a hackernews widget from its dashboard configuration.
func New(ctx widget.Context) (widget.Widget, error) {
	cfg := config{Limit: 20, Title: "hacker news"}
	if err := ctx.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Limit <= 0 || cfg.Limit > 100 {
		cfg.Limit = 20
	}

	// The front page changes slowly; polling faster is just rude to the API.
	refresh, err := ctx.Refresh(cfg.Refresh, 0, time.Minute)
	if err != nil {
		return nil, err
	}

	return &HackerNews{cfg: cfg, theme: ctx.Theme, refresh: refresh}, nil
}

// Title is the label drawn in the widget frame.
func (h *HackerNews) Title() string { return h.cfg.Title }

// Init starts the first fetch.
func (h *HackerNews) Init() tea.Cmd {
	h.loading = true
	return h.fetch()
}

// Update handles navigation, fetch results and the refresh timer.
func (h *HackerNews) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case loadedMsg:
		h.loading = false
		h.err = msg.err
		if msg.err == nil {
			h.stories = msg.stories
			h.list.SetLen(len(h.stories))
			h.fetched = time.Now()
		}
		return h.scheduleRefresh()

	case refreshMsg:
		if h.loading {
			return nil
		}
		h.loading = true
		return h.fetch()

	case tea.KeyMsg:
		if h.list.HandleKey(msg, h.perScreen()) {
			return nil
		}
		if msg.String() == "r" && !h.loading {
			h.loading = true
			return h.fetch()
		}
	}
	return nil
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
	if h.list.Empty() {
		return nil
	}
	url := h.stories[h.list.Cursor()].Link()
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
	return h.Every(h.refresh, refreshMsg{})
}

// fetch queries Algolia off the UI goroutine.
func (h *HackerNews) fetch() tea.Cmd {
	limit := h.cfg.Limit
	return h.Cmd(func() tea.Msg {
		stories, err := fetchStories(limit)
		return loadedMsg{stories: stories, err: err}
	})
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

	var b strings.Builder
	start, end := h.list.Window(h.perScreen())
	for i := start; i < end; i++ {
		if i > start {
			b.WriteByte('\n')
		}
		b.WriteString(h.titleLine(h.stories[i], i, i == h.list.Cursor()))
		if h.twoLineRows() {
			b.WriteByte('\n')
			b.WriteString(h.metaLine(h.stories[i]))
		}
	}
	return b.String()
}

// twoLineRows reports whether there is room for each story's metadata line.
func (h *HackerNews) twoLineRows() bool { return h.H >= 4 }

// perScreen is how many stories fit, which is also how far a page key moves.
func (h *HackerNews) perScreen() int {
	if h.twoLineRows() {
		return max(1, h.H/2)
	}
	return max(1, h.H)
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
