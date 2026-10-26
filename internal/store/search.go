package store

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
)

// Filter describes a search request.
type Filter struct {
	// Query is the raw user query (free text and field filters).
	Query string
	// Match is a pre-built FTS5 MATCH expression. When set it takes
	// precedence over Query.
	Match string
	// Exclude maps a field to the values that must not match.
	Exclude map[string][]string
	Kinds   []string
	Sources []string
	Status  []string
	Modes   []string
	Regions []string
	Tags    []string
	Org     string

	// Verdict restricts to include / exclude / uncertain. Empty means any.
	Verdict string

	MinPrize float64
	MaxPrize float64

	DeadlineBefore *time.Time
	DeadlineAfter  *time.Time

	// UpdatedAfter restricts to recently changed items.
	UpdatedAfter *time.Time

	Starred          bool
	IncludeDismissed bool

	Limit  int
	Offset int

	// Sort is one of: relevance (default), deadline, prize, newest, score.
	Sort string
}

// SearchResult is a page of search results.
type SearchResult struct {
	Items  []*domain.Opportunity
	Total  int
	Limit  int
	Offset int
}

// Search runs a filtered query.
func (s *Store) Search(f Filter) (SearchResult, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	if f.Sort == "" {
		f.Sort = "relevance"
	}

	where := []string{}
	args := []any{}

	if f.Match != "" || f.Query != "" {
		match := f.Match
		if match == "" {
			match = BuildFTSQuery(f.Query)
		}
		if match != "" {
			where = append(where, `o.rowid IN (SELECT rowid FROM opportunities_fts WHERE opportunities_fts MATCH ?)`)
			args = append(args, match)
		}
	}
	if len(f.Kinds) > 0 {
		where = append(where, `o.kind IN (`+placeholders(len(f.Kinds))+`)`)
		args = append(args, toAny(f.Kinds)...)
	}
	if ex := f.Exclude["kind"]; len(ex) > 0 {
		where = append(where, `o.kind NOT IN (`+placeholders(len(ex))+`)`)
		args = append(args, toAny(ex)...)
	}
	if len(f.Sources) > 0 {
		where = append(where, `o.source IN (`+placeholders(len(f.Sources))+`)`)
		args = append(args, toAny(f.Sources)...)
	}
	if ex := f.Exclude["source"]; len(ex) > 0 {
		where = append(where, `o.source NOT IN (`+placeholders(len(ex))+`)`)
		args = append(args, toAny(ex)...)
	}
	if len(f.Status) > 0 {
		where = append(where, `o.status IN (`+placeholders(len(f.Status))+`)`)
		args = append(args, toAny(f.Status)...)
	}
	if len(f.Modes) > 0 {
		where = append(where, `o.mode IN (`+placeholders(len(f.Modes))+`)`)
		args = append(args, toAny(f.Modes)...)
	}
	if len(f.Regions) > 0 {
		where = append(where, `o.region IN (`+placeholders(len(f.Regions))+`)`)
		args = append(args, toAny(f.Regions)...)
	}
	if len(f.Tags) > 0 {
		where = append(where, `EXISTS (SELECT 1 FROM opportunity_tags t WHERE t.opportunity_id = o.id AND t.tag IN (`+placeholders(len(f.Tags))+`))`)
		args = append(args, toAny(f.Tags)...)
	}
	if f.Org != "" {
		where = append(where, `o.org LIKE ?`)
		args = append(args, "%"+f.Org+"%")
	}
	if f.Verdict != "" {
		where = append(where, `(o.verdict = ? OR o.user_verdict = ?)`)
		args = append(args, f.Verdict, f.Verdict)
	}
	if f.MinPrize > 0 {
		where = append(where, `o.prize_amount >= ?`)
		args = append(args, f.MinPrize)
	}
	if f.MaxPrize > 0 {
		where = append(where, `o.prize_amount <= ?`)
		args = append(args, f.MaxPrize)
	}
	if f.DeadlineBefore != nil {
		where = append(where, `o.deadline_at IS NOT NULL AND o.deadline_at <= ?`)
		args = append(args, fmtTimeVal(*f.DeadlineBefore))
	}
	if f.DeadlineAfter != nil {
		where = append(where, `o.deadline_at IS NOT NULL AND o.deadline_at >= ?`)
		args = append(args, fmtTimeVal(*f.DeadlineAfter))
	}
	if f.UpdatedAfter != nil {
		where = append(where, `o.updated_at >= ?`)
		args = append(args, fmtTimeVal(*f.UpdatedAfter))
	}
	if f.Starred {
		where = append(where, `o.id IN (SELECT opportunity_id FROM watchlist)`)
	}
	if !f.IncludeDismissed {
		where = append(where, `o.id NOT IN (SELECT opportunity_id FROM dismissals)`)
	}

	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	// Count first so the UI can show "showing 50 of 312".
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM opportunities o`+clause, args...).Scan(&total); err != nil {
		return SearchResult{}, fmt.Errorf("count search: %w", err)
	}

	order := "o.updated_at DESC"
	switch f.Sort {
	case "deadline":
		order = "o.deadline_at IS NULL, o.deadline_at ASC"
	case "prize":
		order = "o.prize_amount DESC, o.updated_at DESC"
	case "newest":
		order = "o.first_seen_at DESC"
	case "score":
		order = "o.score DESC, o.updated_at DESC"
	}

	// For relevance we fetch a wider window and blend the scores in Go.
	limit, offset := f.Limit, f.Offset
	if f.Sort == "relevance" {
		limit = 500
		offset = 0
	}

	sqlText := `SELECT ` + oppColumns + ` FROM opportunities o` + clause + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	rows, err := s.db.Query(sqlText, append(args, limit, offset)...)
	if err != nil {
		return SearchResult{}, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var items []*domain.Opportunity
	for rows.Next() {
		o, err := scanOpportunity(rows)
		if err != nil {
			return SearchResult{}, err
		}
		items = append(items, o)
	}
	if err := rows.Err(); err != nil {
		return SearchResult{}, err
	}

	if f.Sort == "relevance" {
		items = rankByRelevance(items, f, time.Now())
		if offset < len(items) {
			items = items[offset:]
		} else {
			items = nil
		}
		if len(items) > f.Limit {
			items = items[:f.Limit]
		}
	}

	return SearchResult{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}, nil
}

// rankByRelevance blends the classifier score, deadline urgency and recency.
// It is used when no explicit sort order is requested.
func rankByRelevance(items []*domain.Opportunity, f Filter, now time.Time) []*domain.Opportunity {
	now = now.UTC()
	type scored struct {
		o   *domain.Opportunity
		val float64
	}
	scoredItems := make([]scored, 0, len(items))
	for _, o := range items {
		urgency := 0.0
		if o.DeadlineAt != nil {
			days := o.DeadlineAt.Sub(now).Hours() / 24
			if days < 0 {
				urgency = 0.05
			} else {
				urgency = 0.35 * exp(-days/7)
			}
		}
		recency := 0.0
		if !o.FirstSeenAt.IsZero() {
			age := now.Sub(o.FirstSeenAt).Hours() / 24
			recency = 0.25 * exp(-age/14)
		}
		base := o.Score
		if o.Verdict == domain.VerdictInclude {
			base = 0.6 + 0.4*o.Score
		}
		scoredItems = append(scoredItems, scored{o, base*0.6 + urgency + recency})
	}
	sort.SliceStable(scoredItems, func(i, j int) bool {
		if scoredItems[i].val != scoredItems[j].val {
			return scoredItems[i].val > scoredItems[j].val
		}
		return scoredItems[i].o.Title < scoredItems[j].o.Title
	})
	out := make([]*domain.Opportunity, len(scoredItems))
	for i, s := range scoredItems {
		out[i] = s.o
	}
	return out
}

func exp(x float64) float64 {
	// Small helper to avoid importing math in many places.
	return mathExp(x)
}

// BuildFTSQuery converts a user query into an FTS5 MATCH expression.
// It supports quoted phrases, field prefixes (title:ai), negation (-term) and
// prefix matching for the last word of the query.
func BuildFTSQuery(q string) string {
	tokens := tokenizeQuery(q)
	var parts []string
	for _, t := range tokens {
		// Boolean operators are passed through to FTS5.
		switch strings.ToUpper(t) {
		case "OR", "AND", "NOT":
			if len(parts) > 0 {
				parts = append(parts, strings.ToUpper(t))
			}
			continue
		}
		switch {
		case strings.HasPrefix(t, "-") && len(t) > 1:
			parts = append(parts, "NOT "+quoteFTS(t[1:]))
		case strings.Contains(t, ":"):
			field, value, _ := strings.Cut(t, ":")
			if isFTSField(field) && value != "" {
				parts = append(parts, field+":"+quoteFTS(value)+"*")
			} else {
				parts = append(parts, quoteFTS(t))
			}
		default:
			parts = append(parts, quoteFTS(t)+"*")
		}
	}
	return strings.Join(parts, " ")
}

func isFTSField(f string) bool {
	switch f {
	case "title", "org", "description", "tags":
		return true
	}
	return false
}

func quoteFTS(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// tokenizeQuery splits a query into tokens, keeping quoted phrases together.
func tokenizeQuery(q string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := false
	for _, r := range q {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case (r == ' ' || r == '\t' || r == '\n') && !inQuote:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// mathExp is a tiny wrapper so the file does not need the math import in
// several places.
func mathExp(x float64) float64 { return math.Exp(x) }
