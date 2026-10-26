package sources

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
)

// fixtureClient serves recorded responses from testdata/fixtures.
type fixtureClient struct {
	dir     string
	subject string
	calls   int
}

func (f *fixtureClient) GetJSON(ctx context.Context, url string, out any) error {
	body, err := f.GetBytes(ctx, url, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func (f *fixtureClient) GetString(ctx context.Context, url string) (string, error) {
	b, err := f.GetBytes(ctx, url, nil)
	return string(b), err
}

func (f *fixtureClient) GetBytes(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	f.calls++
	name := f.subject + "_page1.json"
	if strings.Contains(url, "opportunity=competitions") {
		name = f.subject + "_competitions_page1.json"
	}
	path := filepath.Join(f.dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "fixtures", name)
}

func TestDevpostAdapter(t *testing.T) {
	client := &fixtureClient{dir: fixtureDir(t, "devpost"), subject: "hackathons"}
	src := &Devpost{Client: client}

	items, err := src.Fetch(context.Background(), domain.FetchRequest{Limit: 10})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items")
	}
	o := items[0]
	if o.Source != "devpost" || o.SourceID == "" {
		t.Errorf("unexpected source identity: %+v", o)
	}
	if o.Kind != domain.KindHackathon {
		t.Errorf("kind = %s, want hackathon", o.Kind)
	}
	if o.Title == "" || o.Org == "" {
		t.Errorf("missing title or org: %+v", o)
	}
	if !strings.Contains(o.URL, "devpost.com") {
		t.Errorf("unexpected url: %s", o.URL)
	}
	if o.DeadlineAt == nil {
		t.Errorf("expected a deadline from the submission period: %+v", o)
	}
}

func TestDevpostPrizeParsing(t *testing.T) {
	o := devpostHackathon{PrizeAmount: "$<span data-currency-value>100,000</span>", Title: "x"}
	m, ok := domain.ParseMoney(domain.StripHTML(o.PrizeAmount), "USD")
	if !ok {
		t.Fatal("expected the prize to parse")
	}
	if m.Amount != 100000 || m.Currency != "USD" {
		t.Fatalf("unexpected money: %+v", m)
	}
}

func TestUnstopAdapter(t *testing.T) {
	client := &fixtureClient{dir: fixtureDir(t, "unstop"), subject: "hackathons"}
	src := &Unstop{Client: client, Types: []string{"hackathons"}}

	items, err := src.Fetch(context.Background(), domain.FetchRequest{Limit: 10})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items")
	}
	o := items[0]
	if o.Source != "unstop" {
		t.Errorf("source = %s", o.Source)
	}
	if o.Kind != domain.KindHackathon {
		t.Errorf("kind = %s, want hackathon", o.Kind)
	}
	if o.Org == "" {
		t.Errorf("expected an organisation")
	}
	if o.DeadlineAt == nil {
		t.Errorf("expected a registration deadline")
	}
	if o.TeamMax == 0 && o.TeamMin == 0 {
		t.Logf("no team size in the fixture")
	}
	if o.Prize.Amount > 0 && o.Prize.Currency != "INR" {
		t.Errorf("unexpected prize currency: %+v", o.Prize)
	}
	if o.Description == "" {
		t.Errorf("expected a description")
	}
	if strings.Contains(o.Description, "<") {
		t.Errorf("description should be plain text: %q", o.Description)
	}
}

func TestUnstopKindMapping(t *testing.T) {
	cases := []struct {
		typ, subtype string
		want         domain.Kind
	}{
		{"hackathons", "general_competition", domain.KindHackathon},
		{"hackathons", "build_challenge", domain.KindBuildathon},
		{"competitions", "", domain.KindCompetitio},
		{"jobs", "internships", domain.KindInternship},
		{"jobs", "", domain.KindJob},
		{"events", "", domain.KindMeetup},
	}
	for _, tc := range cases {
		if got := kindFromUnstopType(tc.typ, tc.subtype); got != tc.want {
			t.Errorf("kindFromUnstopType(%q,%q) = %s, want %s", tc.typ, tc.subtype, got, tc.want)
		}
	}
}

func TestParseDateRange(t *testing.T) {
	start, end := parseDateRange("Oct 30 - Dec 04, 2026")
	if start == nil || end == nil {
		t.Fatalf("expected both dates, got %v %v", start, end)
	}
	if start.Month() != time.October || start.Day() != 30 {
		t.Errorf("unexpected start: %v", start)
	}
	if end.Month() != time.December || end.Day() != 4 || end.Year() != 2026 {
		t.Errorf("unexpected end: %v", end)
	}

	start, end = parseDateRange("Jan 05, 2026 - Jan 07, 2026")
	if start == nil || end == nil || end.Day() != 7 {
		t.Fatalf("unexpected range: %v %v", start, end)
	}
}

func TestRegistryDefaults(t *testing.T) {
	r := Default(&fixtureClient{dir: fixtureDir(t, "devpost")})
	names := map[string]bool{}
	for _, s := range r.All() {
		names[s.Name()] = true
	}
	for _, want := range []string{"devpost", "unstop", "ctftime"} {
		if !names[want] {
			t.Errorf("missing source %s", want)
		}
	}
	if _, ok := r.Get("devpost"); !ok {
		t.Error("Get(devpost) failed")
	}
}
