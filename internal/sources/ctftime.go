package sources

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
)

// CTFtime fetches capture the flag events from ctftime.org.
//
// Endpoint: https://ctftime.org/api/v1/events/?limit=N
type CTFtime struct {
	Client  HTTPClient
	BaseURL string
}

type ctfEvent struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	Start        int64   `json:"start"`
	End          int64   `json:"end"`
	Length       int64   `json:"length"`
	Location     string  `json:"location"`
	Weight       float64 `json:"weight"`
	Restrictions string  `json:"restrictions"`
}

// Name returns the source identifier.
func (c *CTFtime) Name() string { return "ctftime" }

// Describe returns the static metadata of the source.
func (c *CTFtime) Describe() domain.SourceMeta {
	return domain.SourceMeta{
		Name:        "ctftime",
		Homepage:    "https://ctftime.org/events/",
		Docs:        "https://ctftime.org/api/v1/events/",
		Trust:       0.7,
		DefaultKind: domain.KindCTF,
		Enabled:     false,
		Interval:    2 * time.Hour,
		RateLimit:   domain.RateLimit{RequestsPerMinute: 20, MinInterval: 3 * time.Second},
		Regions:     []string{"Global"},
		Note:        "CTF events: a contest variant of the build-event taxonomy.",
	}
}

// Fetch returns the upcoming CTF events.
func (c *CTFtime) Fetch(ctx context.Context, req domain.FetchRequest) ([]domain.Opportunity, error) {
	base := c.BaseURL
	if base == "" {
		base = "https://ctftime.org/api/v1/events/"
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	endpoint := fmt.Sprintf("%s?limit=%d", base, limit)

	var events []ctfEvent
	if err := c.Client.GetJSON(ctx, endpoint, &events); err != nil {
		return nil, &domain.ErrSourceUnavailable{Source: c.Name(), Err: err}
	}

	out := make([]domain.Opportunity, 0, len(events))
	for _, e := range events {
		if strings.TrimSpace(e.Title) == "" {
			continue
		}
		o := domain.NewOpportunity(c.Name(), strconv.Itoa(e.ID), e.Title)
		o.URL = fmt.Sprintf("https://ctftime.org/event/%d", e.ID)
		o.Org = e.Location
		o.Description = domain.StripHTML(e.Description)
		o.Kind = domain.KindCTF
		o.Region = e.Location
		if e.Start > 0 {
			t := time.Unix(e.Start, 0).UTC()
			o.StartsAt = &t
		}
		if e.End > 0 {
			t := time.Unix(e.End, 0).UTC()
			o.EndsAt = &t
			o.DeadlineAt = &t
		}
		o.Status = domain.StatusUpcoming
		if e.Length > 0 {
			o.Tags = append(o.Tags, fmt.Sprintf("%dh event", int(e.Length/3600)))
		}
		if e.Restrictions != "" {
			o.Eligibility = "Restrictions: " + e.Restrictions
		}
		out = append(out, *o)
	}
	return out, nil
}
