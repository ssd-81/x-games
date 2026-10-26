// Package domain holds the core entities and value objects of XGames.
//
// It has no dependencies outside the standard library: everything else in the
// application depends on this package, never the other way around.
package domain

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

// Kind classifies the nature of an opportunity. The taxonomy separates
// build-events (things you compete in by shipping something) from job
// listings (things you get hired for).
type Kind string

const (
	KindUnknown    Kind = "unknown"
	KindHackathon  Kind = "hackathon"
	KindBuildathon Kind = "buildathon"
	KindChallenge  Kind = "challenge"
	KindCompetitio Kind = "competition"
	KindContest    Kind = "contest"
	KindCTF        Kind = "ctf"
	KindFellowship Kind = "fellowship"
	KindResidency  Kind = "residency"
	KindInternship Kind = "internship"
	KindMeetup     Kind = "meetup"
	KindJob        Kind = "job"
	KindConference Kind = "conference"
)

// AllKinds lists every known kind, in display order.
func AllKinds() []Kind {
	return []Kind{
		KindHackathon, KindBuildathon, KindChallenge, KindCompetitio,
		KindContest, KindCTF, KindFellowship, KindResidency,
		KindInternship, KindMeetup, KindConference, KindJob, KindUnknown,
	}
}

// IsBuildEvent reports whether the kind is a build-event in the broad sense:
// you participate by building something rather than by being employed.
func (k Kind) IsBuildEvent() bool {
	switch k {
	case KindHackathon, KindBuildathon, KindChallenge, KindCompetitio,
		KindContest, KindCTF, KindFellowship, KindResidency, KindMeetup:
		return true
	case KindInternship:
		// Internships are ambiguous; the classifier decides.
		return true
	default:
		return false
	}
}

// Label returns a human friendly label for the kind.
func (k Kind) Label() string {
	switch k {
	case KindCompetitio:
		return "competition"
	default:
		return string(k)
	}
}

// Status describes where an opportunity is in its lifecycle.
type Status string

const (
	StatusUnknown   Status = "unknown"
	StatusUpcoming  Status = "upcoming"
	StatusOpen      Status = "open"
	StatusOngoing   Status = "ongoing"
	StatusEnded     Status = "ended"
	StatusCancelled Status = "cancelled"
)

// Mode describes how an event takes place.
type Mode string

const (
	ModeUnknown Mode = "unknown"
	ModeOnline  Mode = "online"
	ModeHybrid  Mode = "hybrid"
	ModeOffline Mode = "offline"
)

// Verdict is the classifier's decision about an opportunity.
type Verdict string

const (
	VerdictInclude   Verdict = "include"
	VerdictExclude   Verdict = "exclude"
	VerdictUncertain Verdict = "uncertain"
)

// Money is a prize or stipend amount with its currency.
type Money struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency,omitempty"`
	Text     string  `json:"text,omitempty"` // original human readable form, e.g. "₹2,00,000"
}

// IsZero reports whether no amount is set.
func (m Money) IsZero() bool { return m.Amount == 0 && m.Text == "" }

// String renders a compact human readable amount.
func (m Money) String() string {
	if m.Text != "" {
		return m.Text
	}
	if m.Amount == 0 {
		return ""
	}
	return trimFloat(m.Amount) + " " + m.Currency
}

// Interval is a date range used by the event detail view.
type Interval struct {
	Start *time.Time
	End   *time.Time
}

// Opportunity is the canonical entity of the application: a single
// build-event (or job listing that we filtered out).
type Opportunity struct {
	ID          string     `json:"id"`
	Source      string     `json:"source"`
	SourceID    string     `json:"source_id"`
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	Org         string     `json:"org"`
	Description string     `json:"description"`
	Kind        Kind       `json:"kind"`
	Status      Status     `json:"status"`
	Mode        Mode       `json:"mode"`
	Region      string     `json:"region"`
	StartsAt    *time.Time `json:"starts_at,omitempty"`
	EndsAt      *time.Time `json:"ends_at,omitempty"`
	DeadlineAt  *time.Time `json:"deadline_at,omitempty"`
	Prize       Money      `json:"prize"`
	TeamMin     int        `json:"team_min"`
	TeamMax     int        `json:"team_max"`
	Tags        []string   `json:"tags"`
	Eligibility string     `json:"eligibility,omitempty"`
	Judging     string     `json:"judging,omitempty"`
	ImageURL    string     `json:"image_url,omitempty"`

	// Classification
	Score   float64  `json:"score"`
	Verdict Verdict  `json:"verdict"`
	Reasons []string `json:"reasons,omitempty"`

	// Bookkeeping
	IsNew       bool      `json:"is_new"`
	ContentHash string    `json:"-"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewOpportunity builds an opportunity with sane defaults.
func NewOpportunity(source, sourceID, title string) *Opportunity {
	return &Opportunity{
		Source:   source,
		SourceID: sourceID,
		Title:    strings.TrimSpace(title),
		Kind:     KindUnknown,
		Status:   StatusUnknown,
		Mode:     ModeUnknown,
		Verdict:  VerdictUncertain,
	}
}

// ComputeID returns the stable identifier for the opportunity. It is derived
// from the source and the source's own id so that title edits do not create a
// new record.
func (o *Opportunity) ComputeID() string {
	key := o.Source + "|" + o.SourceID
	if o.SourceID == "" {
		key = o.Source + "|" + CanonicalURL(o.URL)
	}
	sum := sha1.Sum([]byte(key))
	return hex.EncodeToString(sum[:])[:12]
}

// ComputeContentHash returns a hash over the semantic fields of the
// opportunity. A change in the hash means the source updated the listing.
func (o *Opportunity) ComputeContentHash() string {
	var b strings.Builder
	b.WriteString(normalizeText(o.Title))
	b.WriteString("|")
	b.WriteString(normalizeText(o.Org))
	b.WriteString("|")
	b.WriteString(string(o.Kind))
	b.WriteString("|")
	if o.DeadlineAt != nil {
		b.WriteString(o.DeadlineAt.UTC().Format(time.RFC3339))
	}
	b.WriteString("|")
	if o.StartsAt != nil {
		b.WriteString(o.StartsAt.UTC().Format(time.RFC3339))
	}
	b.WriteString("|")
	b.WriteString(timeKey(o.EndsAt))
	b.WriteString("|")
	b.WriteString(trimFloat(o.Prize.Amount))
	b.WriteString(o.Prize.Currency)
	b.WriteString("|")
	b.WriteString(itoa(o.TeamMin))
	b.WriteString("-")
	b.WriteString(itoa(o.TeamMax))
	b.WriteString("|")
	b.WriteString(normalizeText(o.Description))
	b.WriteString("|")
	b.WriteString(normalizeText(o.Eligibility))
	sum := sha1.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// timeKey renders a timestamp for hashing; a missing timestamp yields "".
func timeKey(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// DaysToDeadline returns the number of days until the deadline. It returns a
// negative value for past deadlines and math.MaxInt when no deadline is set.
func (o *Opportunity) DaysToDeadline(now time.Time) int {
	if o.DeadlineAt == nil {
		return 1<<31 - 1
	}
	return int(o.DeadlineAt.Sub(now).Hours() / 24)
}

// SearchText returns a lowercased blob used by the classifier and the search
// index.
func (o *Opportunity) SearchText() string {
	parts := []string{o.Title, o.Org, o.Description, o.Eligibility, o.Judging}
	parts = append(parts, o.Tags...)
	return strings.ToLower(strings.Join(parts, " \n "))
}

var (
	utmPattern    = regexp.MustCompile(`(?i)^utm_`)
	trackingParam = regexp.MustCompile(`(?i)^(utm_|ref|ref_|fbclid|gclid|mc_cid|mc_eid|source|trk|campaign|fbclid)$`)
)

// CanonicalURL normalizes a URL for identity and deduplication: it drops
// tracking parameters, the fragment and a trailing slash.
func CanonicalURL(raw string) string {
	if raw == "" {
		return ""
	}
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "www.")
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		base, query := raw[:i], raw[i+1:]
		query = strings.TrimPrefix(query, "?")
		if q := strings.Index(query, "#"); q >= 0 {
			query = query[:q]
		}
		var kept []string
		for _, part := range strings.Split(query, "&") {
			if part == "" {
				continue
			}
			key := part
			if eq := strings.Index(part, "="); eq >= 0 {
				key = part[:eq]
			}
			if trackingParam.MatchString(key) || utmPattern.MatchString(key) {
				continue
			}
			kept = append(kept, part)
		}
		raw = base
		if len(kept) > 0 {
			raw += "?" + strings.Join(kept, "&")
		}
	}
	raw = strings.TrimSuffix(raw, "/")
	return raw
}

func normalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Join(strings.Fields(s), " ")
}

func trimFloat(f float64) string {
	s := strings.TrimRight(strings.TrimRight(formatFloat(f), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}
