package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ssd-81/x-games/internal/domain"
)

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	paneStyle     = lipgloss.NewStyle().Padding(0, 1)
	selectedStyle = lipgloss.NewStyle().Bold(true)
	mutedStyle    = lipgloss.NewStyle().Faint(true)
	badgeStyle    = lipgloss.NewStyle().Faint(true)
)

// View renders the interface.
func (m *model) View() string {
	if m.quitting {
		return ""
	}
	if m.mode == modeHelp {
		return m.helpView()
	}
	if m.width == 0 || m.height == 0 {
		return "starting xgames…"
	}

	header := m.headerView()
	footer := m.footerView()

	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	if m.mode == modeDetail {
		return header + "\n" + m.detailView(m.width, bodyHeight) + "\n" + footer
	}

	listWidth := m.width * 45 / 100
	if listWidth < 36 {
		listWidth = m.width
	}
	if listWidth > m.width-30 {
		listWidth = m.width
	}
	detailWidth := m.width - listWidth

	left := m.listView(listWidth, bodyHeight)
	right := m.detailView(detailWidth, bodyHeight)

	// Join the panes side by side, padding the shorter one.
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")
	height := bodyHeight
	if len(leftLines) < height {
		leftLines = append(leftLines, make([]string, height-len(leftLines))...)
	}
	if len(rightLines) < height {
		rightLines = append(rightLines, make([]string, height-len(rightLines))...)
	}
	var b strings.Builder
	for i := 0; i < height; i++ {
		l := ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		}
		b.WriteString(pad(l, listWidth))
		b.WriteString(r)
		if i < height-1 {
			b.WriteString("\n")
		}
	}
	return header + "\n" + b.String() + "\n" + footer
}

func (m *model) headerView() string {
	now := time.Now()
	parts := []string{"xgames"}
	if m.query != "" {
		parts = append(parts, "▸ "+m.query)
	} else {
		parts = append(parts, "▸ all build-events")
	}
	parts = append(parts, fmt.Sprintf("%d/%d", len(m.items), m.total))
	left := strings.Join(parts, "  ·  ")

	right := ""
	switch {
	case m.syncing:
		right = "⟳ syncing"
	case !m.lastSync.IsZero():
		right = "synced " + now.Sub(m.lastSync).Round(time.Second).String() + " ago"
	}

	line := left
	if right != "" {
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap > 0 {
			line = left + strings.Repeat(" ", gap) + right
		} else {
			line = left
		}
	}
	return headerStyle.Render(truncate(line, m.width))
}

// listView renders the results pane.
func (m *model) listView(width, height int) string {
	if len(m.items) == 0 {
		lines := []string{
			"  no build-events found.",
			"",
			"  press r to sync the sources",
			"  press / to search, ? for help",
		}
		return pad(strings.Join(lines, "\n"), width)
	}

	now := time.Now()
	lines := make([]string, 0, height)
	for i := m.offset; i < len(m.items) && len(lines) < height; i++ {
		o := m.items[i]
		title := fmt.Sprintf("%s %s", verdictSymbol(o), o.Title)
		badge := o.Kind.Label()
		if o.Status == domain.StatusOpen || o.Status == domain.StatusOngoing {
			badge += " ●open"
		}
		if o.IsNew {
			badge += " ★new"
		}
		if m.starred[o.ID] {
			badge += " ★"
		}
		prize := o.Prize.String()
		if prize == "" {
			prize = "—"
		}
		meta := fmt.Sprintf("%s · %s · %s", badge, truncate(prize, 16), deadlineLabel(o, now))
		line := truncate(title, width-2)
		if i == m.cursor {
			line = selectedStyle.Render("▸" + truncate(title, width-3))
		}
		lines = append(lines, pad(line, width))
		if len(lines) < height {
			lines = append(lines, pad("  "+truncate(meta, width-4), width))
		}
	}
	return strings.Join(lines, "\n")
}

// detailView renders the details of the selected opportunity.
func (m *model) detailView(width, height int) string {
	o := m.current()
	if o == nil {
		return pad(mutedStyle.Render("no selection"), width)
	}
	now := time.Now()

	var b strings.Builder
	line := func(format string, args ...any) {
		b.WriteString(pad(truncate(fmt.Sprintf(format, args...), width-2), width))
		b.WriteString("\n")
	}

	line("%s", titleStyle.Render(o.Title))
	if o.Org != "" {
		line("  %s", mutedStyle.Render(o.Org))
	}
	b.WriteString("\n")

	line("  kind      %s", o.Kind.Label())
	line("  status    %s", o.Status)
	if o.Mode != domain.ModeUnknown {
		line("  mode      %s", o.Mode)
	}
	if o.Region != "" {
		line("  region    %s", o.Region)
	}
	if o.DeadlineAt != nil {
		line("  deadline  %s  (%s)", o.DeadlineAt.Format("Mon 02 Jan 2006"), deadlineLabel(o, now))
	}
	if !o.Prize.IsZero() {
		line("  prize     %s", o.Prize.String())
	}
	if o.TeamMin > 0 || o.TeamMax > 0 {
		line("  team      %d-%d", o.TeamMin, o.TeamMax)
	}
	if o.Eligibility != "" {
		line("  eligible  %s", o.Eligibility)
	}
	if len(o.Tags) > 0 {
		line("  tags      %s", truncate(strings.Join(o.Tags, ", "), width-14))
	}
	line("  source    %s", o.Source)
	line("  score     %.2f  (%s)", o.Score, o.Verdict)
	b.WriteString("\n")

	if len(o.Reasons) > 0 {
		line("  why this is here:")
		for _, r := range o.Reasons {
			line("    · %s", r)
		}
		b.WriteString("\n")
	}

	if o.Description != "" {
		desc := strings.TrimSpace(o.Description)
		if len(desc) > 1200 {
			desc = desc[:1200] + "…"
		}
		for _, para := range strings.Split(desc, "\n") {
			if strings.TrimSpace(para) == "" {
				b.WriteString("\n")
				continue
			}
			line("  %s", para)
		}
	}

	out := b.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if m.detailOffset > 0 {
		if m.detailOffset >= len(lines) {
			m.detailOffset = len(lines) - 1
		}
		lines = lines[m.detailOffset:]
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	// Pad to the full height so the pane keeps its shape.
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// footerView renders the key hints and the status message.
func (m *model) footerView() string {
	hints := "j/k move · / search · f filter · ⏎ open · r sync · s sort · a star · x dismiss · v all · ? help · q quit"
	switch m.mode {
	case modeSearch:
		hints = "type to search · filters: kind: source: region: prize:> deadline:< · enter apply · esc cancel"
	case modeFilter:
		hints = "filters: kind:hackathon source:devpost region:India prize:>50000 deadline:<30d · enter apply · esc cancel"
	}

	if m.mode == modeSearch || m.mode == modeFilter {
		input := m.searchInput
		if m.mode == modeFilter {
			input = m.filterInput
		}
		return pad(truncate("> "+input.value+"▏", m.width), m.width) + "\n" + pad(truncate(hints, m.width), m.width)
	}

	status := m.statusText
	if m.errText != "" {
		status = "error: " + m.errText
	}
	if status == "" {
		status = hints
	}
	return pad(truncate(status, m.width), m.width) + "\n" + pad(truncate(hints, m.width), m.width)
}

// helpView renders the cheat sheet.
func (m *model) helpView() string {
	sections := []struct {
		title string
		keys  [][2]string
	}{
		{"navigate", [][2]string{
			{"j / ↓", "next event"},
			{"k / ↑", "previous event"},
			{"g / G", "top / bottom"},
			{"⏎", "open the detail view"},
			{"tab", "toggle the side by side layout"},
			{"esc", "back"},
		}},
		{"search", [][2]string{
			{"/", "search (free text and filters)"},
			{"f", "filter (same syntax, filter hints)"},
			{"filters", "kind: source: region: mode: tag: org:"},
			{"", "prize:>50000  deadline:<30d  -term to exclude"},
		}},
		{"curate", [][2]string{
			{"a", "add or remove from the watchlist"},
			{"x", "dismiss this event"},
			{"v", "show excluded and uncertain items"},
			{"s", "cycle the sort order"},
		}},
		{"sync", [][2]string{
			{"r", "sync the sources now"},
			{"o", "show the URL of the selection"},
		}},
		{"general", [][2]string{
			{"?", "this help"},
			{"q / ctrl+c", "quit"},
		}},
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("xgames — help"))
	b.WriteString("\n\n")
	for _, s := range sections {
		b.WriteString(titleStyle.Render(s.title))
		b.WriteString("\n")
		for _, k := range s.keys {
			b.WriteString(fmt.Sprintf("  %-14s %s\n", k[0], k[1]))
		}
		b.WriteString("\n")
	}
	b.WriteString(mutedStyle.Render("press any key to go back"))
	return b.String()
}

// pad pads a string to the given width, truncating when needed.
func pad(s string, width int) string {
	w := lipgloss.Width(s)
	if w > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(s)
	}
	return s + strings.Repeat(" ", width-w)
}
