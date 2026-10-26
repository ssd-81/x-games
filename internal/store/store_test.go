package store

import (
	"testing"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenMemory()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seed(t *testing.T, s *Store) {
	t.Helper()
	deadline := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	items := []*domain.Opportunity{
		{
			Source: "devpost", SourceID: "1", Title: "Razorpay AI Buildathon",
			Org: "Razorpay", Description: "Build an AI agent. Teams of 2-4. Prize pool Rs 2,00,000.",
			Kind: domain.KindBuildathon, Status: domain.StatusOpen, Mode: domain.ModeHybrid,
			Region: "India", DeadlineAt: &deadline, Prize: domain.Money{Amount: 200000, Currency: "INR", Text: "₹2,00,000"},
			TeamMin: 2, TeamMax: 4, Tags: []string{"AI", "Fintech"}, Score: 0.9, Verdict: domain.VerdictInclude,
		},
		{
			Source: "unstop", SourceID: "2", Title: "Software Engineer Internship",
			Org: "Gogo Pogo", Description: "Full-time role, CTC 12 LPA, apply now with your resume.",
			Kind: domain.KindInternship, Status: domain.StatusOpen, Region: "India",
			DeadlineAt: &deadline, Score: 0.05, Verdict: domain.VerdictExclude,
		},
		{
			Source: "unstop", SourceID: "3", Title: "HackWithInfy Coding Challenge",
			Org: "Infosys", Description: "Online coding challenge. Win prizes. Submit your project.",
			Kind: domain.KindChallenge, Status: domain.StatusOpen, Mode: domain.ModeOnline,
			Region: "India", DeadlineAt: &deadline, Prize: domain.Money{Amount: 100000, Currency: "INR"},
			Score: 0.8, Verdict: domain.VerdictInclude,
		},
	}
	for _, o := range items {
		o.ID = o.ComputeID()
		o.ContentHash = o.ComputeContentHash()
		if _, err := s.Upsert(o); err != nil {
			t.Fatalf("upsert %s: %v", o.Title, err)
		}
	}
}

func TestUpsertAndGet(t *testing.T) {
	s := testStore(t)
	seed(t, s)

	n, err := s.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}

	o := &domain.Opportunity{Source: "devpost", SourceID: "1", Title: "Razorpay AI Buildathon"}
	o.ID = o.ComputeID()
	got, err := s.Get(o.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Razorpay AI Buildathon" || got.Org != "Razorpay" {
		t.Fatalf("unexpected record: %+v", got)
	}
	if len(got.Tags) != 2 {
		t.Fatalf("tags = %v, want 2 entries", got.Tags)
	}
}

func TestUpsertDetectsUpdate(t *testing.T) {
	s := testStore(t)
	seed(t, s)

	o := &domain.Opportunity{Source: "devpost", SourceID: "1", Title: "Razorpay AI Buildathon"}
	o.ID = o.ComputeID()
	o.Org = "Razorpay"
	o.ContentHash = o.ComputeContentHash()
	res, err := s.Upsert(o)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated {
		t.Fatalf("expected update, got %+v", res)
	}
}

func TestSearchFullText(t *testing.T) {
	s := testStore(t)
	seed(t, s)

	res, err := s.Search(Filter{Query: "razorpay"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].Title != "Razorpay AI Buildathon" {
		t.Fatalf("unexpected results: %+v", res.Items)
	}

	res, err = s.Search(Filter{Query: "buildathon OR challenge"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(res.Items), res.Items)
	}
}

func TestSearchFilters(t *testing.T) {
	s := testStore(t)
	seed(t, s)

	res, err := s.Search(Filter{Kinds: []string{"challenge"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Kind != domain.KindChallenge {
		t.Fatalf("unexpected kind filter result: %+v", res.Items)
	}

	res, err = s.Search(Filter{Verdict: string(domain.VerdictExclude)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Title != "Software Engineer Internship" {
		t.Fatalf("unexpected verdict filter result: %+v", res.Items)
	}

	res, err = s.Search(Filter{MinPrize: 150000})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Org != "Razorpay" {
		t.Fatalf("unexpected prize filter result: %+v", res.Items)
	}
}

func TestSearchSortByDeadline(t *testing.T) {
	s := testStore(t)
	seed(t, s)

	soon := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	o := &domain.Opportunity{Source: "x", SourceID: "4", Title: "Soon", Kind: domain.KindHackathon, DeadlineAt: &soon}
	o.ID = o.ComputeID()
	o.ContentHash = o.ComputeContentHash()
	if _, err := s.Upsert(o); err != nil {
		t.Fatal(err)
	}

	res, err := s.Search(Filter{Sort: "deadline"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) == 0 || res.Items[0].Title != "Soon" {
		t.Fatalf("expected the closest deadline first, got %+v", res.Items)
	}
}

func TestDismissAndStar(t *testing.T) {
	s := testStore(t)
	seed(t, s)

	o := &domain.Opportunity{Source: "devpost", SourceID: "1"}
	o.ID = o.ComputeID()
	if err := s.Dismiss(o.ID); err != nil {
		t.Fatal(err)
	}
	res, err := s.Search(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range res.Items {
		if item.ID == o.ID {
			t.Fatal("dismissed item should not be listed")
		}
	}
	res, err = s.Search(Filter{IncludeDismissed: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range res.Items {
		if item.ID == o.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("dismissed item should be listed when included")
	}

	other := &domain.Opportunity{Source: "unstop", SourceID: "3"}
	other.ID = other.ComputeID()
	if err := s.Star(other.ID, "interesting"); err != nil {
		t.Fatal(err)
	}
	res, err = s.Search(Filter{Starred: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 starred item, got %d", len(res.Items))
	}
}

func TestBuildFTSQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ai buildathon", `"ai"* "buildathon"*`},
		{`title:razorpay`, `title:"razorpay"*`},
		{"-job hackathon", `NOT "job" "hackathon"*`},
	}
	for _, tc := range cases {
		if got := BuildFTSQuery(tc.in); got != tc.want {
			t.Errorf("BuildFTSQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRulesRoundTrip(t *testing.T) {
	s := testStore(t)

	r := Rule{ID: "keep-hack", Expr: "title =~ /hackathon/i", Action: domain.VerdictInclude, Enabled: true}
	if err := s.AddRule(r); err != nil {
		t.Fatal(err)
	}
	rules, err := s.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != "keep-hack" || rules[0].Action != domain.VerdictInclude {
		t.Fatalf("unexpected rules: %+v", rules)
	}
	if !rules[0].Enabled {
		t.Fatal("expected the rule to be enabled")
	}
	if err := s.SetRuleEnabled("keep-hack", false); err != nil {
		t.Fatal(err)
	}
	rules, _ = s.ListRules()
	if rules[0].Enabled {
		t.Fatal("expected the rule to be disabled")
	}
	if err := s.DeleteRule("keep-hack"); err != nil {
		t.Fatal(err)
	}
	rules, _ = s.ListRules()
	if len(rules) != 0 {
		t.Fatalf("expected no rules, got %+v", rules)
	}
}
