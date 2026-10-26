package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ssd-81/x-games/internal/classify"
	"github.com/ssd-81/x-games/internal/config"
	"github.com/ssd-81/x-games/internal/domain"
	"github.com/ssd-81/x-games/internal/logging"
	"github.com/ssd-81/x-games/internal/store"
)

func newTestModel(t *testing.T) *model {
	t.Helper()
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	deadline := time.Now().Add(72 * time.Hour)
	items := []*domain.Opportunity{
		{Source: "devpost", SourceID: "1", Title: "Razorpay AI Buildathon", Org: "Razorpay",
			Description: "Build an agent. Teams of 2-4.", Kind: domain.KindBuildathon,
			DeadlineAt: &deadline, Prize: domain.Money{Amount: 200000, Currency: "INR"},
			Score: 0.9, Verdict: domain.VerdictInclude},
		{Source: "unstop", SourceID: "2", Title: "HackWithInfy Challenge", Org: "Infosys",
			Kind: domain.KindChallenge, DeadlineAt: &deadline,
			Score: 0.8, Verdict: domain.VerdictInclude},
		{Source: "unstop", SourceID: "3", Title: "Marketing Internship", Org: "Acme",
			Kind: domain.KindInternship, Score: 0.1, Verdict: domain.VerdictExclude},
	}
	for _, o := range items {
		o.ID = o.ComputeID()
		o.ContentHash = o.ComputeContentHash()
		if _, err := st.Upsert(o); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	m := &model{
		store:       st,
		cfg:         config.Config{},
		classifier:  classify.New(classify.DefaultConfig()),
		logger:      logging.Discard(),
		sortBy:      "relevance",
		starred:     map[string]bool{},
		autoRefresh: time.Minute,
	}
	m.search()
	return m
}

func key(m *model, s string) {
	var msg tea.KeyMsg
	switch s {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	m.Update(msg)
}

func TestModelFiltersOutExcludedItems(t *testing.T) {
	m := newTestModel(t)
	if len(m.items) != 2 {
		t.Fatalf("expected the excluded internship to be hidden, got %d items", len(m.items))
	}
	for _, o := range m.items {
		if o.Verdict == domain.VerdictExclude {
			t.Fatalf("excluded item listed: %s", o.Title)
		}
	}
}

func TestModelShowAll(t *testing.T) {
	m := newTestModel(t)
	m.showAll = true
	m.search()
	if len(m.items) != 3 {
		t.Fatalf("expected 3 items with showAll, got %d", len(m.items))
	}
}

func TestModelNavigation(t *testing.T) {
	m := newTestModel(t)
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
	key(m, "j")
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	key(m, "k")
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
	key(m, "G")
	if m.cursor != len(m.items)-1 {
		t.Fatalf("cursor = %d, want %d", m.cursor, len(m.items)-1)
	}
	key(m, "g")
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
}

func TestModelSearchMode(t *testing.T) {
	m := newTestModel(t)
	key(m, "/")
	if m.mode != modeSearch {
		t.Fatalf("mode = %v, want search", m.mode)
	}
	for _, r := range "razorpay" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	key(m, "enter")
	if m.mode != modeBrowse {
		t.Fatalf("mode = %v, want browse", m.mode)
	}
	if len(m.items) != 1 || !strings.Contains(m.items[0].Title, "Razorpay") {
		t.Fatalf("unexpected results: %+v", m.items)
	}
}

func TestModelDetailMode(t *testing.T) {
	m := newTestModel(t)
	key(m, "enter")
	if m.mode != modeDetail {
		t.Fatalf("mode = %v, want detail", m.mode)
	}
	key(m, "esc")
	if m.mode != modeBrowse {
		t.Fatalf("mode = %v, want browse", m.mode)
	}
}

func TestModelQuit(t *testing.T) {
	m := newTestModel(t)
	key(m, "q")
	if !m.quitting {
		t.Fatal("expected the model to quit")
	}
}

func TestViewRenders(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	view := m.View()
	if !strings.Contains(view, "Razorpay AI Buildathon") {
		t.Fatalf("expected the list to contain the event, got:\n%s", view)
	}
	if !strings.Contains(view, "xgames") {
		t.Fatalf("expected the header, got:\n%s", view)
	}
}

func TestViewDetailShowsReasons(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	key(m, "enter")
	view := m.View()
	if !strings.Contains(view, "deadline") && !strings.Contains(view, "kind") {
		t.Fatalf("expected the detail fields, got:\n%s", view)
	}
}

func TestHelpView(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	key(m, "?")
	if m.mode != modeHelp {
		t.Fatalf("mode = %v, want help", m.mode)
	}
	if !strings.Contains(m.View(), "search") {
		t.Fatalf("expected the help text, got:\n%s", m.View())
	}
}
