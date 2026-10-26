// Package tui implements the interactive terminal interface of XGames.
package tui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ssd-81/x-games/internal/app"
	"github.com/ssd-81/x-games/internal/classify"
	"github.com/ssd-81/x-games/internal/config"
	"github.com/ssd-81/x-games/internal/domain"
	"github.com/ssd-81/x-games/internal/httpx"
	"github.com/ssd-81/x-games/internal/logging"
	"github.com/ssd-81/x-games/internal/sources"
	"github.com/ssd-81/x-games/internal/store"
)

// Options configures the TUI.
type Options struct {
	ConfigPath string
	DBPath     string
	Query      string
	Verbose    bool
}

// Run starts the interactive interface.
func Run(opts Options) error {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	if opts.DBPath != "" {
		cfg.DBPath = opts.DBPath
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	logger := logging.New(opts.Verbose)
	client := httpx.New(httpx.Options{Logger: logger})
	registry := sources.Default(client)

	cl := app.ClassifierWithRules(classify.New(classify.DefaultConfig()), app.LoadRules(st, cfg, logger))
	engine := app.NewSyncEngine(st, cl, logger)

	m := newModel(st, cfg, cl, engine, registry, client, logger)
	m.query = opts.Query
	if opts.Query != "" {
		m.search()
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	return p.Start()
}

// mode is the current interaction mode of the interface.
type mode int

const (
	modeBrowse mode = iota
	modeSearch
	modeDetail
	modeHelp
	modeFilter
	modeReview
)

// model is the root bubbletea model.
type model struct {
	store      *store.Store
	cfg        config.Config
	classifier *classify.Classifier
	engine     *app.SyncEngine
	registry   *sources.Registry
	client     *httpx.Client
	logger     *slog.Logger

	mode  mode
	query string

	items   []*domain.Opportunity
	cursor  int
	total   int
	offset  int
	sortBy  string
	showAll bool
	starred map[string]bool

	searchInput   textInput
	filterInput   textInput
	detailOffset  int
	statusText    string
	errText       string
	syncing       bool
	lastSync      time.Time
	autoRefresh   time.Duration
	width, height int
	quitting      bool
	showDetail    bool
}

// textInput is a tiny single line input (bubbles' textinput is used when
// available; this keeps the TUI dependency free for tests).
type textInput struct {
	value  string
	cursor int
}

func (t *textInput) set(v string) {
	t.value = v
	t.cursor = len(v)
}

func (t *textInput) insert(s string) {
	if t.cursor > len(t.value) {
		t.cursor = len(t.value)
	}
	t.value = t.value[:t.cursor] + s + t.value[t.cursor:]
	t.cursor += len(s)
}

func (t *textInput) backspace() {
	if t.cursor == 0 {
		return
	}
	t.value = t.value[:t.cursor-1] + t.value[t.cursor:]
	t.cursor--
}

func (t *textInput) delete() {
	if t.cursor >= len(t.value) {
		return
	}
	t.value = t.value[:t.cursor] + t.value[t.cursor+1:]
}

func (t *textInput) left() {
	if t.cursor > 0 {
		t.cursor--
	}
}

func (t *textInput) right() {
	if t.cursor < len(t.value) {
		t.cursor++
	}
}

func (t *textInput) clear() {
	t.value = ""
	t.cursor = 0
}

func newModel(st *store.Store, cfg config.Config, cl *classify.Classifier, engine *app.SyncEngine,
	registry *sources.Registry, client *httpx.Client, logger *slog.Logger) *model {
	m := &model{
		store:       st,
		cfg:         cfg,
		classifier:  cl,
		engine:      engine,
		registry:    registry,
		client:      client,
		logger:      logger,
		sortBy:      "relevance",
		starred:     map[string]bool{},
		autoRefresh: 5 * time.Minute,
	}
	m.filterInput.set(fmt.Sprintf("kind:%s", "hackathon"))
	return m
}

// --- styles ---

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	accentStyle = lipgloss.NewStyle().Bold(true)
	warnStyle   = lipgloss.NewStyle().Bold(true)
)

// --- helpers ---

func (m *model) current() *domain.Opportunity {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	return m.items[m.cursor]
}

// search runs the current query against the store.
func (m *model) search() {
	pq := app.ParseQuery(m.query)
	f := pq.Filter(m.cfg.UI.PageSize, m.sortBy)
	f.Verdict = "include"
	if m.showAll {
		f.Verdict = ""
	}
	f.IncludeDismissed = false
	res, err := m.store.Search(f)
	if err != nil {
		m.errText = err.Error()
		return
	}
	m.errText = ""
	m.items = res.Items
	m.total = res.Total
	m.cursor = 0
	m.offset = 0
}

// refresh reloads the items, keeping the cursor position when possible.
func (m *model) refresh() {
	title := ""
	if cur := m.current(); cur != nil {
		title = cur.Title
	}
	m.search()
	if title != "" {
		for i, o := range m.items {
			if o.Title == title {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
}

func (m *model) clampCursor() {
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *model) visibleRows() int {
	// Header (1) + footer (1) + borders.
	h := m.height - 4
	if h < 3 {
		h = 3
	}
	return h
}

func (m *model) moveCursor(delta int) {
	m.cursor += delta
	m.clampCursor()
	// Keep the cursor inside the visible window.
	rows := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// status writes a transient message to the status line.
func (m *model) status(format string, args ...any) {
	m.statusText = fmt.Sprintf(format, args...)
	m.errText = ""
}

// fail writes an error to the status line.
func (m *model) fail(err error) {
	if err == nil {
		return
	}
	m.errText = err.Error()
	m.statusText = ""
}

// syncNow triggers a background sync.
func (m *model) syncNow() tea.Cmd {
	if m.syncing {
		return nil
	}
	m.syncing = true
	m.statusText = "syncing sources..."
	return func() tea.Msg {
		return doSync(m.engine, m.registry, m.cfg)
	}
}

// syncDoneMsg is returned when a background sync finished.
type syncDoneMsg struct {
	summary string
	err     error
}

func doSync(engine *app.SyncEngine, registry *sources.Registry, cfg config.Config) tea.Msg {
	var selected []app.Source
	for _, s := range registry.All() {
		meta := s.Describe()
		if !cfg.SourceEnabled(meta.Name, meta.Enabled) {
			continue
		}
		selected = append(selected, s)
	}
	results, err := engine.Sync(context.Background(), selected, app.SyncOptions{Limit: 50})
	if err != nil {
		return syncDoneMsg{err: err}
	}
	return syncDoneMsg{summary: app.SyncResultSummary(results)}
}

// truncate shortens a string for the list view.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// deadlineLabel renders the deadline of an opportunity.
func deadlineLabel(o *domain.Opportunity, now time.Time) string {
	if o.DeadlineAt == nil {
		return "—"
	}
	d := o.DeadlineAt.Sub(now)
	if d < 0 {
		return "closed"
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.0fh left", d.Hours())
	}
	return fmt.Sprintf("%.0fd left", d.Hours()/24)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func init() {
	// Keep the terminal usable when the output is not a TTY.
	if os.Getenv("TERM") == "dumb" {
		lipgloss.SetColorProfile(0)
	}
	_ = strings.TrimSpace
}
