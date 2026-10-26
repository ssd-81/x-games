package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
)

// Unstop fetches opportunities from unstop.com.
//
// Endpoint: https://unstop.com/api/public/opportunity/search-result
//
// The response distinguishes hackathons (type=hackathons) from job postings
// (type=jobs, subtype=internships), which is the strongest signal available
// for the classifier.
type Unstop struct {
	Client  HTTPClient
	BaseURL string
	// Types restricts which opportunity types are fetched.
	Types []string
}

type unstopResponse struct {
	Data struct {
		Data []unstopOpportunity `json:"data"`
	} `json:"data"`
}

type unstopOpportunity struct {
	ID             int    `json:"id"`
	Title          string `json:"title"`
	SEOURL         string `json:"seo_url"`
	PublicURL      string `json:"public_url"`
	ShortURL       string `json:"short_url"`
	Type           string `json:"type"`
	Subtype        string `json:"subtype"`
	Region         string `json:"region"`
	Status         string `json:"status"`
	Details        string `json:"details"`
	StartDate      string `json:"start_date"`
	EndDate        string `json:"end_date"`
	UpdatedAt      string `json:"updated_at"`
	IsPaid         bool   `json:"isPaid"`
	RegnOpen       int    `json:"regn_open"`
	RequiredSkills []struct {
		ID        int    `json:"id"`
		Skill     string `json:"skill"`
		SkillName string `json:"skill_name"`
	} `json:"required_skills"`
	WorkFunction []struct {
		Name string `json:"name"`
	} `json:"workfunction"`
	Organisation struct {
		Name string `json:"name"`
	} `json:"organisation"`
	Prizes []struct {
		Rank     string  `json:"rank"`
		Cash     float64 `json:"cash"`
		Currency string  `json:"currency"`
		Others   string  `json:"others"`
	} `json:"prizes"`
	RegnRequirements struct {
		StartRegnDt  string `json:"start_regn_dt"`
		EndRegnDt    string `json:"end_regn_dt"`
		MinTeamSize  int    `json:"min_team_size"`
		MaxTeamSize  int    `json:"max_team_size"`
		Eligibility  string `json:"eligibility"`
		WorkLocation string `json:"work_location_type"`
		RegStatus    string `json:"reg_status"`
	} `json:"regnRequirements"`
	Filters []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"filters"`
	Locations []struct {
		Address string `json:"address"`
		City    string `json:"city"`
		Country string `json:"country"`
	} `json:"locations"`
}

// Name returns the source identifier.
func (u *Unstop) Name() string { return "unstop" }

// Describe returns the static metadata of the source.
func (u *Unstop) Describe() domain.SourceMeta {
	return domain.SourceMeta{
		Name:        "unstop",
		Homepage:    "https://unstop.com/hackathons",
		Docs:        "https://unstop.com/api/public/opportunity/search-result",
		Trust:       0.9,
		DefaultKind: domain.KindUnknown,
		Enabled:     true,
		Interval:    30 * time.Minute,
		RateLimit:   domain.RateLimit{RequestsPerMinute: 30, MinInterval: 2 * time.Second},
		Regions:     []string{"India", "Global"},
		Note:        "Indian hackathons, buildathons, internships and competitions.",
	}
}

// Fetch returns opportunities, paging through the source until the limit is
// reached.
func (u *Unstop) Fetch(ctx context.Context, req domain.FetchRequest) ([]domain.Opportunity, error) {
	base := u.BaseURL
	if base == "" {
		base = "https://unstop.com/api/public/opportunity/search-result"
	}
	types := req.Types
	if len(types) == 0 {
		types = u.Types
	}
	if len(types) == 0 {
		types = []string{"hackathons", "competitions"}
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	perPage := req.Limit
	if perPage <= 0 || perPage > 50 {
		perPage = 50
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	var all []domain.Opportunity
	for _, t := range types {
		for len(all) < limit {
			q := url.Values{}
			q.Set("opportunity", t)
			q.Set("oppstatus", "open")
			q.Set("per_page", strconv.Itoa(perPage))
			q.Set("page", strconv.Itoa(page))
			q.Set("sort", "latest")

			var resp unstopResponse
			if err := u.Client.GetJSON(ctx, base+"?"+q.Encode(), &resp); err != nil {
				if len(all) == 0 {
					return nil, &domain.ErrSourceUnavailable{Source: u.Name(), Err: err}
				}
				break
			}
			if len(resp.Data.Data) == 0 {
				break
			}
			for _, raw := range resp.Data.Data {
				if len(all) >= limit {
					break
				}
				o, err := u.convert(raw)
				if err != nil {
					continue
				}
				all = append(all, *o)
			}
			if len(resp.Data.Data) < perPage {
				break
			}
			page++
		}
	}
	return all, nil
}

func (u *Unstop) convert(raw unstopOpportunity) (*domain.Opportunity, error) {
	if strings.TrimSpace(raw.Title) == "" {
		return nil, fmt.Errorf("unstop record without title")
	}
	o := domain.NewOpportunity(u.Name(), strconv.Itoa(raw.ID), raw.Title)
	o.URL = strings.TrimSpace(firstNonEmpty(raw.SEOURL, raw.PublicURL))
	o.Org = raw.Organisation.Name
	o.Description = domain.StripHTML(raw.Details)
	o.Kind = kindFromUnstopType(raw.Type, raw.Subtype)
	// The unstop "region" field actually holds the mode (online/offline/hybrid);
	// the geographic region comes from the locations list.
	o.Mode = modeFromUnstopRegion(raw.Region)
	o.Status = statusFromUnstopStatus(raw.Status)

	if t := domain.ParseTimeBestEffort(raw.EndDate); t != nil {
		o.EndsAt = t
	}
	if t := domain.ParseTimeBestEffort(raw.StartDate); t != nil {
		o.StartsAt = t
	}
	if t := domain.ParseTimeBestEffort(raw.RegnRequirements.EndRegnDt); t != nil {
		o.DeadlineAt = t
	} else if o.EndsAt != nil {
		o.DeadlineAt = o.EndsAt
	}
	if t := domain.ParseTimeBestEffort(raw.RegnRequirements.StartRegnDt); t != nil && o.StartsAt == nil {
		o.StartsAt = t
	}

	o.TeamMin = raw.RegnRequirements.MinTeamSize
	o.TeamMax = raw.RegnRequirements.MaxTeamSize
	if o.TeamMin == 1 && o.TeamMax == 1 {
		// A single person event: do not imply a team constraint.
		o.TeamMin, o.TeamMax = 0, 0
	}

	o.Prize = prizeFromUnstop(raw.Prizes)
	if o.Prize.IsZero() {
		var parts []string
		for _, p := range raw.Prizes {
			if p.Others != "" {
				parts = append(parts, strings.TrimSpace(p.Others))
			}
		}
		if len(parts) > 0 {
			o.Prize.Text = strings.Join(parts, ", ")
		}
	}

	for _, s := range raw.RequiredSkills {
		name := s.SkillName
		if name == "" {
			name = s.Skill
		}
		if name != "" {
			o.Tags = append(o.Tags, name)
		}
	}
	for _, f := range raw.Filters {
		if f.Name != "" {
			o.Tags = append(o.Tags, f.Name)
		}
	}
	for _, w := range raw.WorkFunction {
		if w.Name != "" {
			o.Tags = append(o.Tags, w.Name)
		}
	}
	o.Tags = dedupeStrings(o.Tags)

	o.Eligibility = summarizeEligibility(raw.RegnRequirements.Eligibility)
	for _, l := range raw.Locations {
		loc := strings.TrimSpace(strings.TrimSpace(l.City) + " " + strings.TrimSpace(l.Country))
		if loc == "" {
			loc = strings.TrimSpace(l.Address)
		}
		if loc != "" {
			o.Region = loc
			break
		}
	}
	if o.Region == "" {
		o.Region = "Global"
	}
	return o, nil
}

// kindFromUnstopType maps the unstop type/subtype to a domain kind.
func kindFromUnstopType(t, subtype string) domain.Kind {
	switch strings.ToLower(t) {
	case "hackathons":
		if strings.Contains(strings.ToLower(subtype), "build") {
			return domain.KindBuildathon
		}
		return domain.KindHackathon
	case "competitions":
		return domain.KindCompetitio
	case "jobs":
		if strings.ToLower(subtype) == "internships" {
			return domain.KindInternship
		}
		return domain.KindJob
	case "events":
		return domain.KindMeetup
	default:
		return domain.KindUnknown
	}
}

// modeFromUnstopRegion maps the unstop region to a mode.
func modeFromUnstopRegion(region string) domain.Mode {
	switch strings.ToLower(region) {
	case "online":
		return domain.ModeOnline
	case "offline":
		return domain.ModeOffline
	case "hybrid":
		return domain.ModeHybrid
	default:
		return domain.ModeUnknown
	}
}

// statusFromUnstopStatus maps the unstop status to a domain status.
func statusFromUnstopStatus(status string) domain.Status {
	switch strings.ToUpper(status) {
	case "LIVE", "OPEN":
		return domain.StatusOpen
	case "UPCOMING", "NOTSTARTED":
		return domain.StatusUpcoming
	case "ENDED", "CLOSED", "COMPLETED":
		return domain.StatusEnded
	default:
		return domain.StatusUnknown
	}
}

// prizeFromUnstop sums the cash prizes of an opportunity.
func prizeFromUnstop(prizes []struct {
	Rank     string  `json:"rank"`
	Cash     float64 `json:"cash"`
	Currency string  `json:"currency"`
	Others   string  `json:"others"`
}) domain.Money {
	var total float64
	currency := ""
	for _, p := range prizes {
		if p.Cash > 0 {
			total += p.Cash
			if currency == "" {
				currency = currencyFromUnstop(p.Currency)
			}
		}
	}
	if total == 0 {
		return domain.Money{}
	}
	return domain.Money{Amount: total, Currency: currency, Text: formatAmount(total, currency)}
}

// currencyFromUnstop maps the unstop currency code to an ISO code.
func currencyFromUnstop(code string) string {
	switch strings.ToLower(code) {
	case "fa-rupee", "inr", "rupee":
		return "INR"
	case "fa-dollar", "usd", "dollar":
		return "USD"
	case "fa-euro", "eur", "euro":
		return "EUR"
	case "":
		return "INR"
	default:
		return strings.ToUpper(code)
	}
}

// summarizeEligibility turns the JSON eligibility blob into a short summary.
func summarizeEligibility(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var elig struct {
		Sector      []string `json:"sector"`
		Experience  []string `json:"experience"`
		Others      []string `json:"others"`
		Engineering []string `json:"engineering"`
	}
	if err := json.Unmarshal([]byte(raw), &elig); err != nil {
		return ""
	}
	var parts []string
	add := func(label string, values []string) {
		if len(values) == 0 {
			return
		}
		parts = append(parts, label+": "+strings.Join(values, ", "))
	}
	add("Who", elig.Sector)
	add("Experience", elig.Experience)
	add("Engineering", elig.Engineering)
	if len(elig.Others) > 0 && !containsAll(elig.Others, "all") {
		add("Other", elig.Others)
	}
	return strings.Join(parts, " · ")
}

func containsAll(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func formatAmount(amount float64, currency string) string {
	switch currency {
	case "INR":
		return "₹" + humanizeIndian(amount)
	default:
		return fmt.Sprintf("%s %.0f", symbolFor(currency), amount)
	}
}

// humanizeIndian formats an amount using the Indian numbering system.
func humanizeIndian(amount float64) string {
	whole := int64(amount)
	if whole >= 10000000 {
		return fmt.Sprintf("%.2f Cr", float64(whole)/10000000)
	}
	if whole >= 100000 {
		return fmt.Sprintf("%.2f L", float64(whole)/100000)
	}
	if whole >= 1000 {
		return fmt.Sprintf("%.1fK", float64(whole)/1000)
	}
	return fmt.Sprintf("%d", whole)
}

func symbolFor(currency string) string {
	switch currency {
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "INR":
		return "₹"
	default:
		return currency + " "
	}
}
