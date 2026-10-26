package domain

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Source describes a place where build-events are published.
type Source interface {
	// Name returns the stable identifier of the source, e.g. "devpost".
	Name() string
	// Describe returns static metadata used by the UI and the classifier.
	Describe() SourceMeta
	// Fetch returns the opportunities offered by the source.
	Fetch(ctx context.Context, req FetchRequest) ([]Opportunity, error)
}

// SourceMeta is the static description of a source.
type SourceMeta struct {
	Name        string
	Homepage    string
	Docs        string
	Trust       float64       // 0..1 default trust for items from this source
	DefaultKind Kind          // hint used by the classifier (KindUnknown if unsure)
	Enabled     bool          // enabled by default
	Interval    time.Duration // suggested refresh interval
	RateLimit   RateLimit
	Regions     []string
	Note        string
}

// RateLimit describes the politeness budget of a source.
type RateLimit struct {
	RequestsPerMinute int
	MinInterval       time.Duration
}

// FetchRequest is the input of a source fetch.
type FetchRequest struct {
	// Since enables incremental fetching when the source supports it.
	Since time.Time
	// Limit is the maximum number of items to fetch (0 = source default).
	Limit int
	// Page is the zero based page index for paginated sources.
	Page int
	// Query is an optional server side filter.
	Query string
	// Types restricts the opportunity types a multi-type source should return.
	Types []string
}

// FetchResult is returned by sources that support pagination metadata.
type FetchResult struct {
	Items      []Opportunity
	NextCursor string
	HasMore    bool
}

// ErrSourceUnavailable indicates a source could not be reached.
type ErrSourceUnavailable struct {
	Source string
	Err    error
}

func (e *ErrSourceUnavailable) Error() string {
	return fmt.Sprintf("source %s unavailable: %v", e.Source, e.Err)
}

func (e *ErrSourceUnavailable) Unwrap() error { return e.Err }

// ErrNotFound is returned when an entity does not exist.
var ErrNotFound = fmt.Errorf("not found")

// ParseMoney converts a human readable amount into a Money value.
// It understands rupee, dollar and euro symbols and "K"/"L"/"Cr"/"M"/"B"
// magnitude suffixes. ok is false when nothing numeric could be found.
func ParseMoney(text, currencyHint string) (Money, bool) {
	orig := strings.TrimSpace(text)
	if orig == "" {
		return Money{}, false
	}
	lower := strings.ToLower(orig)

	cur := currencyHint
	switch {
	case strings.Contains(lower, "₹"), strings.Contains(lower, "inr"), strings.Contains(lower, "rs."), strings.Contains(lower, "rs "):
		cur = "INR"
	case strings.Contains(lower, "$"), strings.Contains(lower, "usd"):
		cur = "USD"
	case strings.Contains(lower, "€"), strings.Contains(lower, "eur"):
		cur = "EUR"
	case strings.Contains(lower, "£"), strings.Contains(lower, "gbp"):
		cur = "GBP"
	}
	if cur == "" {
		cur = "INR"
	}

	// Keep only digits, separators and magnitude letters.
	var numeric strings.Builder
	var suffix strings.Builder
	seenDigit := false
	for _, r := range orig {
		switch {
		case r >= '0' && r <= '9':
			numeric.WriteRune(r)
			seenDigit = true
		case r == ',' || r == '.' || r == ' ':
			if seenDigit {
				numeric.WriteRune(r)
			}
		case r == 'K' || r == 'k':
			suffix.WriteString("K")
		case r == 'L' || r == 'l':
			suffix.WriteString("L")
		case r == 'C' || r == 'c':
			suffix.WriteString("Cr")
		case r == 'M' || r == 'm':
			suffix.WriteString("M")
		case r == 'B' || r == 'b':
			suffix.WriteString("B")
		}
	}
	if !seenDigit {
		return Money{}, false
	}
	clean := strings.ReplaceAll(numeric.String(), " ", "")
	clean = strings.ReplaceAll(clean, ",", "")
	// Handle "1.5" style magnitudes and thousands separators ambiguity:
	// if there is a dot followed by exactly 3 digits it is a thousands group.
	if strings.Count(clean, ".") > 1 {
		clean = strings.ReplaceAll(clean, ".", "")
	}
	val, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return Money{}, false
	}
	switch suffix.String() {
	case "K":
		val *= 1_000
	case "L":
		val *= 100_000
	case "Cr":
		val *= 10_000_000
	case "M":
		val *= 1_000_000
	case "B":
		val *= 1_000_000_000
	}
	return Money{Amount: val, Currency: cur, Text: orig}, true
}

// ParseTimeBestEffort parses the many date formats found in the wild.
// It returns nil when nothing sensible could be parsed.
func ParseTimeBestEffort(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"Jan 02, 2006",
		"Jan 2, 2006",
		"02 Jan 2006",
		"2 Jan 2006",
		"Jan 02 2006",
		"Jan 2 2006",
		"2006/01/02",
		"02-01-2006",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return &t
		}
	}
	return nil
}
