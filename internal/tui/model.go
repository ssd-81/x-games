package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ssd-81/x-games/internal/app"
	"github.com/ssd-81/x-games/internal/domain"
	"github.com/ssd-81/x-games/internal/store"
)

// Init starts the initial background sync and the auto refresh ticker.
func (m *model) Init() tea.Cmd {
	return tea.Batch(m.syncNow(), m.tickCmd())
}

// tickMsg is sent by the auto refresh ticker.
type tickMsg time.Time

// tickCmd schedules the next automatic refresh.
func (m *model) tickCmd() tea.Cmd {
	d := m.autoRefresh
	if d <= 0 {
		d = 5 * time.Minute
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update handles the bubbletea messages.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		if m.quitting {
			return m, nil
		}
		return m, tea.Batch(m.syncNow(), m.tickCmd())

	case syncDoneMsg:
		m.syncing = false
		m.lastSync = time.Now()
		if msg.err != nil {
			m.fail(msg.err)
		} else {
			m.status("%s", msg.summary)
			m.refresh()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.mode == modeSearch || m.mode == modeFilter {
		return m.handleInputKey(msg, key)
	}

	switch key {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
		return m, nil
	case "esc":
		if m.mode == modeDetail {
			m.mode = modeBrowse
			m.detailOffset = 0
		}
		return m, nil
	}

	switch m.mode {
	case modeHelp:
		m.mode = modeBrowse
		return m, nil
	case modeDetail:
		switch key {
		case "j", "down":
			m.detailOffset++
		case "k", "up":
			if m.detailOffset > 0 {
				m.detailOffset--
			}
		case "g":
			m.detailOffset = 0
		case "G":
			m.detailOffset = 1 << 20
		case "o":
			if o := m.current(); o != nil {
				m.status("open: %s", o.URL)
			}
		}
		return m, nil
	}

	// browse mode
	switch key {
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "g", "home":
		m.scrollToTop()
	case "G", "end":
		m.scrollToBottom()
	case "ctrl+d", "pgdown":
		m.moveCursor(m.visibleItems())
	case "ctrl+u", "pgup":
		m.moveCursor(-m.visibleItems())
	case "ctrl+f":
		m.moveCursor(m.visibleItems())
	case "ctrl+b":
		m.moveCursor(-m.visibleItems())
	case "/":
		m.mode = modeSearch
		m.searchInput.set(m.query)
		m.searchInput.cursor = len(m.searchInput.value)
	case "f":
		m.mode = modeFilter
		m.filterInput.set(m.query)
		m.filterInput.cursor = len(m.filterInput.value)
	case "enter":
		if o := m.current(); o != nil {
			m.mode = modeDetail
			m.detailOffset = 0
			if o.IsNew {
				_ = m.store.MarkSeen(o.ID)
				o.IsNew = false
			}
		}
	case "r":
		return m, m.syncNow()
	case "s":
		m.sortBy = nextSort(m.sortBy)
		m.refresh()
		m.status("sort: %s", m.sortBy)
	case "a":
		if o := m.current(); o != nil {
			if m.starred[o.ID] {
				delete(m.starred, o.ID)
				_ = m.store.Unstar(o.ID)
				m.status("removed from watchlist")
			} else {
				m.starred[o.ID] = true
				_ = m.store.Star(o.ID, "")
				m.status("added to watchlist")
			}
		}
	case "x":
		if o := m.current(); o != nil {
			_ = m.store.Dismiss(o.ID)
			m.status("dismissed: %s", truncate(o.Title, 40))
			m.refresh()
		}
	case "h":
		if m.lane == laneHire {
			m.lane = laneAll
		} else {
			m.lane = laneHire
		}
		m.search()
		m.status("lane: %s", m.lane)
	case "c":
		if m.lane == laneCompany {
			m.lane = laneAll
		} else {
			m.lane = laneCompany
		}
		m.search()
		m.status("lane: %s", m.lane)
	case "v":
		m.showAll = !m.showAll
		m.refresh()
		if m.showAll {
			m.status("showing all items (including excluded)")
		} else {
			m.status("showing build-events only")
		}
	case "o":
		if o := m.current(); o != nil {
			m.status("open: %s", o.URL)
		}
	case "tab":
		if m.showDetail {
			m.showDetail = false
		} else {
			m.showDetail = true
		}
	}
	return m, nil
}

func (m *model) handleInputKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	input := &m.searchInput
	if m.mode == modeFilter {
		input = &m.filterInput
	}
	switch key {
	case "esc":
		m.mode = modeBrowse
		return m, nil
	case "enter":
		m.query = input.value
		m.mode = modeBrowse
		m.search()
		m.status("%s", searchSummary(m.total, m.query))
		return m, nil
	case "ctrl+u":
		input.clear()
	case "ctrl+w":
		v := input.value
		if i := strings.LastIndexAny(v, " \t"); i >= 0 {
			input.value = v[:i]
		} else {
			input.clear()
		}
		input.cursor = len(input.value)
	case "backspace":
		input.backspace()
	case "delete":
		input.delete()
	case "left":
		input.left()
	case "right":
		input.right()
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	default:
		// A single KeyMsg may carry several runes (fast typing or pasted
		// text): insert all of them.
		switch msg.Type {
		case tea.KeyRunes:
			input.insert(string(msg.Runes))
		case tea.KeySpace:
			input.insert(" ")
		case tea.KeyTab:
			input.insert("\t")
		default:
			if len(key) == 1 {
				input.insert(key)
			}
		}
	}
	return m, nil
}

func searchSummary(total int, query string) string {
	if query == "" {
		return fmt.Sprintf("%s build-events", plural(total, "event", "events"))
	}
	return fmt.Sprintf("%s for %q", plural(total, "match", "matches"), query)
}

func nextSort(current string) string {
	switch current {
	case "relevance":
		return "deadline"
	case "deadline":
		return "prize"
	case "prize":
		return "newest"
	case "newest":
		return "score"
	default:
		return "relevance"
	}
}

// describeFilter renders the active filters as a compact hint.
func describeFilter(f store.Filter) string {
	var parts []string
	if len(f.Kinds) > 0 {
		parts = append(parts, "kind:"+strings.Join(f.Kinds, "|"))
	}
	if len(f.Sources) > 0 {
		parts = append(parts, "source:"+strings.Join(f.Sources, "|"))
	}
	if len(f.Regions) > 0 {
		parts = append(parts, "region:"+strings.Join(f.Regions, "|"))
	}
	if f.MinPrize > 0 {
		parts = append(parts, fmt.Sprintf("prize>=%.0f", f.MinPrize))
	}
	return strings.Join(parts, " ")
}

// applyPreset applies a named preset from the configuration.
func (m *model) applyPreset(name string) bool {
	preset, ok := m.cfg.Filters.Presets[name]
	if !ok {
		return false
	}
	m.query = preset.Query
	pq := app.ParseQuery(m.query)
	f := pq.Filter(m.cfg.UI.PageSize, m.sortBy)
	for _, k := range preset.Kinds {
		f.Kinds = append(f.Kinds, k)
	}
	if len(preset.Regions) > 0 {
		f.Regions = append(f.Regions, preset.Regions...)
	}
	if preset.MinPrize > 0 {
		f.MinPrize = preset.MinPrize
	}
	f.Verdict = "include"
	res, err := m.store.Search(f)
	if err != nil {
		m.fail(err)
		return true
	}
	m.items = res.Items
	m.total = res.Total
	m.cursor = 0
	m.offset = 0
	return true
}

// verdictColor returns the symbol used in front of a title.
func verdictSymbol(o *domain.Opportunity) string {
	switch o.Verdict {
	case domain.VerdictInclude:
		return "●"
	case domain.VerdictUncertain:
		return "◐"
	default:
		return "○"
	}
}
