package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
	"github.com/ssd-81/x-games/internal/store"
)

// ParsedQuery is the result of parsing the single line query language.
//
// The language mixes free text with field filters:
//
//	ai buildathon kind:hackathon prize:>50000 deadline:<30d region:India
//
// Supported fields: kind, source, region, mode, status, tag, org, title,
// description, prize, deadline. A "!" prefix negates a value, "-" negates a
// free text term.
type ParsedQuery struct {
	Text           string
	Match          string
	Kinds          []string
	ExcludeKinds   []string
	Sources        []string
	ExcludeSources []string
	Regions        []string
	Modes          []string
	Statuses       []string
	Tags           []string
	MinPrize       float64
	MaxPrize       float64
	DeadlineBefore *time.Time
	DeadlineAfter  *time.Time
	HasFilters     bool
}

// ParseQuery parses a query string into a ParsedQuery.
func ParseQuery(q string) ParsedQuery {
	pq := ParsedQuery{}
	var ftsTerms []string

	tokens := splitQuery(q)
	for _, tok := range tokens {
		field, value, hasField := strings.Cut(tok, ":")
		if !hasField {
			if strings.HasPrefix(tok, "-") && len(tok) > 1 {
				ftsTerms = append(ftsTerms, "NOT "+quoteTerm(tok[1:]))
				continue
			}
			ftsTerms = append(ftsTerms, quoteTerm(tok)+"*")
			continue
		}

		field = strings.ToLower(field)
		negate := strings.HasPrefix(value, "!")
		if negate {
			value = value[1:]
		}
		if value == "" {
			continue
		}

		switch field {
		case "kind", "type":
			if negate {
				pq.ExcludeKinds = append(pq.ExcludeKinds, value)
			} else {
				pq.Kinds = append(pq.Kinds, value)
			}
			pq.HasFilters = true
		case "source":
			if negate {
				pq.ExcludeSources = append(pq.ExcludeSources, value)
			} else {
				pq.Sources = append(pq.Sources, value)
			}
			pq.HasFilters = true
		case "region":
			pq.Regions = append(pq.Regions, value)
			pq.HasFilters = true
		case "mode":
			pq.Modes = append(pq.Modes, value)
			pq.HasFilters = true
		case "status":
			pq.Statuses = append(pq.Statuses, value)
			pq.HasFilters = true
		case "tag":
			pq.Tags = append(pq.Tags, value)
			pq.HasFilters = true
		case "org":
			ftsTerms = append(ftsTerms, "org:"+quoteTerm(value)+"*")
			pq.HasFilters = true
		case "title":
			ftsTerms = append(ftsTerms, "title:"+quoteTerm(value)+"*")
		case "description":
			ftsTerms = append(ftsTerms, "description:"+quoteTerm(value)+"*")
		case "prize":
			applyPrizeFilter(&pq, value)
			pq.HasFilters = true
		case "deadline", "closes":
			applyDeadlineFilter(&pq, value)
			pq.HasFilters = true
		default:
			// Unknown field: treat the whole token as free text.
			ftsTerms = append(ftsTerms, quoteTerm(tok)+"*")
		}
	}

	pq.Match = strings.Join(ftsTerms, " ")
	pq.Text = strings.TrimSpace(q)
	return pq
}

// Filter converts the parsed query into a store filter.
func (pq ParsedQuery) Filter(limit int, sortBy string) store.Filter {
	f := store.Filter{
		Match:          pq.Match,
		Kinds:          pq.Kinds,
		Sources:        pq.Sources,
		Regions:        pq.Regions,
		Modes:          pq.Modes,
		Status:         pq.Statuses,
		Tags:           pq.Tags,
		MinPrize:       pq.MinPrize,
		MaxPrize:       pq.MaxPrize,
		DeadlineBefore: pq.DeadlineBefore,
		DeadlineAfter:  pq.DeadlineAfter,
		Limit:          limit,
		Sort:           sortBy,
	}
	if len(pq.ExcludeKinds) > 0 || len(pq.ExcludeSources) > 0 {
		f.Exclude = map[string][]string{
			"kind":   pq.ExcludeKinds,
			"source": pq.ExcludeSources,
		}
	}
	return f
}

// applyPrizeFilter handles prize:>1000 and prize:1000-5000.
func applyPrizeFilter(pq *ParsedQuery, value string) {
	if lo, hi, ok := strings.Cut(value, "-"); ok {
		if l, err := strconv.ParseFloat(strings.TrimSpace(lo), 64); err == nil {
			pq.MinPrize = l
		}
		if h, err := strconv.ParseFloat(strings.TrimSpace(hi), 64); err == nil {
			pq.MaxPrize = h
		}
		return
	}
	switch {
	case strings.HasPrefix(value, ">="):
		v, err := strconv.ParseFloat(strings.TrimPrefix(value, ">="), 64)
		if err != nil {
			return
		}
		pq.MinPrize = v
	case strings.HasPrefix(value, "<="):
		v, err := strconv.ParseFloat(strings.TrimPrefix(value, "<="), 64)
		if err != nil {
			return
		}
		pq.MaxPrize = v
	case strings.HasPrefix(value, ">"):
		v, err := strconv.ParseFloat(strings.TrimPrefix(value, ">"), 64)
		if err != nil {
			return
		}
		pq.MinPrize = v
	case strings.HasPrefix(value, "<"):
		v, err := strconv.ParseFloat(strings.TrimPrefix(value, "<"), 64)
		if err != nil {
			return
		}
		pq.MaxPrize = v
	default:
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return
		}
		pq.MinPrize = v
	}
}

// applyDeadlineFilter handles deadline:<30d and deadline:>7d.
func applyDeadlineFilter(pq *ParsedQuery, value string) {
	durStr := strings.TrimLeft(value, "<>")
	negateBefore := strings.HasPrefix(value, "<")
	negateAfter := strings.HasPrefix(value, ">")

	d, err := parseRelativeDuration(durStr)
	if err != nil {
		return
	}
	switch {
	case negateBefore:
		t := time.Now().Add(d)
		pq.DeadlineBefore = &t
	case negateAfter:
		t := time.Now().Add(d)
		pq.DeadlineAfter = &t
	default:
		t := time.Now().Add(d)
		pq.DeadlineAfter = &t
	}
}

// parseRelativeDuration parses "30d", "72h", "2w", "45m".
func parseRelativeDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	switch {
	case strings.HasSuffix(s, "d"):
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(v * 24 * float64(time.Hour)), nil
	case strings.HasSuffix(s, "w"):
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "w"), 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(v * 7 * 24 * float64(time.Hour)), nil
	default:
		return time.ParseDuration(s)
	}
}

// splitQuery splits a query into tokens, keeping quoted phrases together.
func splitQuery(q string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range q {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case (r == ' ' || r == '\t') && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
}

func quoteTerm(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// KindLabel renders a kind for display.
func KindLabel(k domain.Kind) string { return k.Label() }
