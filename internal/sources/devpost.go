package sources

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
)

// Devpost fetches hackathons from devpost.com.
//
// Endpoint: https://devpost.com/api/hackathons?page=N&status[]=upcoming&status[]=open
type Devpost struct {
	Client HTTPClient
	// BaseURL allows tests to point at a local server.
	BaseURL string
}

type devpostResponse struct {
	Hackathons []devpostHackathon `json:"hackathons"`
	Meta       struct {
		TotalCount int `json:"total_count"`
		PerPage    int `json:"per_page"`
	} `json:"meta"`
}

type devpostHackathon struct {
	ID                int    `json:"id"`
	Title             string `json:"title"`
	URL               string `json:"url"`
	OrganizationName  string `json:"organization_name"`
	DisplayedLocation struct {
		Icon     string `json:"icon"`
		Location string `json:"location"`
	} `json:"displayed_location"`
	OpenState    string `json:"open_state"`
	Featured     bool   `json:"featured"`
	InviteOnly   bool   `json:"invite_only"`
	PrizeAmount  string `json:"prize_amount"`
	PrizesCounts struct {
		Cash  int `json:"cash"`
		Other int `json:"other"`
	} `json:"prizes_counts"`
	Registrations   int    `json:"registrations_count"`
	SubmissionDates string `json:"submission_period_dates"`
	TimeLeft        string `json:"time_left_to_submission"`
	Themes          []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"themes"`
}

// Name returns the source identifier.
func (d *Devpost) Name() string { return "devpost" }

// Describe returns the static metadata of the source.
func (d *Devpost) Describe() domain.SourceMeta {
	return domain.SourceMeta{
		Name:        "devpost",
		Homepage:    "https://devpost.com/hackathons",
		Docs:        "https://devpost.com/api/hackathons",
		Trust:       0.9,
		DefaultKind: domain.KindHackathon,
		Enabled:     true,
		Interval:    30 * time.Minute,
		RateLimit:   domain.RateLimit{RequestsPerMinute: 30, MinInterval: 2 * time.Second},
		Regions:     []string{"Global"},
		Note:        "The largest global hackathon list.",
	}
}

// Fetch returns hackathons. The Devpost API returns nine items per page, so
// the adapter pages internally until the requested limit is reached.
func (d *Devpost) Fetch(ctx context.Context, req domain.FetchRequest) ([]domain.Opportunity, error) {
	base := d.BaseURL
	if base == "" {
		base = "https://devpost.com/api/hackathons"
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	status := req.Types
	if len(status) == 0 {
		status = []string{"upcoming", "open"}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	out := make([]domain.Opportunity, 0, limit)
	for len(out) < limit {
		q := url.Values{}
		q.Set("page", strconv.Itoa(page))
		for _, st := range status {
			q.Add("status[]", st)
		}
		endpoint := base + "?" + q.Encode()

		var resp devpostResponse
		if err := d.Client.GetJSON(ctx, endpoint, &resp); err != nil {
			if len(out) == 0 {
				return nil, &domain.ErrSourceUnavailable{Source: d.Name(), Err: err}
			}
			break
		}
		if len(resp.Hackathons) == 0 {
			break
		}
		for _, h := range resp.Hackathons {
			if len(out) >= limit {
				break
			}
			o, err := d.convert(h)
			if err != nil {
				continue // skip malformed records instead of failing the page
			}
			out = append(out, *o)
		}
		perPage := resp.Meta.PerPage
		if perPage <= 0 {
			perPage = len(resp.Hackathons)
		}
		if page*perPage >= resp.Meta.TotalCount {
			break
		}
		page++
	}
	return out, nil
}

func (d *Devpost) convert(h devpostHackathon) (*domain.Opportunity, error) {
	if strings.TrimSpace(h.Title) == "" {
		return nil, fmt.Errorf("devpost record without title")
	}
	o := domain.NewOpportunity(d.Name(), strconv.Itoa(h.ID), h.Title)
	o.URL = strings.TrimSpace(h.URL)
	o.Org = h.OrganizationName
	o.Kind = domain.KindHackathon
	// Devpost reports either "Online" or the city of the event.
	o.Mode = modeFromLocation(h.DisplayedLocation.Location)
	if o.Mode == domain.ModeOnline {
		o.Region = "Global"
	} else {
		o.Region = h.DisplayedLocation.Location
	}
	o.Status = statusFromOpenState(h.OpenState)

	for _, t := range h.Themes {
		if t.Name != "" {
			o.Tags = append(o.Tags, t.Name)
		}
	}
	if h.SubmissionDates != "" {
		start, end := parseDateRange(h.SubmissionDates)
		o.StartsAt = start
		o.EndsAt = end
		if end != nil {
			o.DeadlineAt = end
		} else if start != nil {
			o.DeadlineAt = start
		}
	}
	if h.PrizeAmount != "" {
		plain := domain.StripHTML(h.PrizeAmount)
		plain = strings.ReplaceAll(plain, "$<span", "")
		if m, ok := domain.ParseMoney(plain, "USD"); ok {
			o.Prize = m
		}
	}
	if h.InviteOnly {
		o.Eligibility = "Invite only"
	}
	return o, nil
}

// modeFromLocation maps a Devpost location string to a mode.
func modeFromLocation(loc string) domain.Mode {
	l := strings.ToLower(loc)
	switch {
	case l == "":
		return domain.ModeUnknown
	case strings.Contains(l, "online"):
		return domain.ModeOnline
	case strings.Contains(l, "hybrid"):
		return domain.ModeHybrid
	default:
		return domain.ModeOffline
	}
}

// statusFromOpenState maps the Devpost open_state to a status.
func statusFromOpenState(state string) domain.Status {
	switch strings.ToLower(state) {
	case "upcoming":
		return domain.StatusUpcoming
	case "open":
		return domain.StatusOpen
	case "ongoing":
		return domain.StatusOngoing
	case "ended", "closed", "archived":
		return domain.StatusEnded
	default:
		return domain.StatusUnknown
	}
}

// parseDateRange parses the many date range formats used by the sources,
// e.g. "Oct 30 - Dec 04, 2026" or "Oct 30, 2026 - Dec 04, 2026".
func parseDateRange(s string) (start, end *time.Time) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.SplitN(s, " - ", 2)
	year := extractYear(s)
	start = parseDatePart(parts[0], year)
	if len(parts) > 1 {
		end = parseDatePart(parts[1], year)
	}
	return start, end
}

// extractYear finds the (last) four digit year in a string.
func extractYear(s string) string {
	matches := yearPattern.FindAllString(s, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

// parseDatePart parses a single date, adding the year when it is missing.
func parseDatePart(part, year string) *time.Time {
	part = strings.TrimSpace(part)
	if part == "" {
		return nil
	}
	if t := domain.ParseTimeBestEffort(part); t != nil {
		return t
	}
	if year != "" {
		if t := domain.ParseTimeBestEffort(part + " " + year); t != nil {
			return t
		}
	}
	return nil
}

var yearPattern = regexp.MustCompile(`\b(20\d{2})\b`)
