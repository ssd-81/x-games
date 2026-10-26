package classify

import (
	"testing"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
)

func ptrTime(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

// golden cases: the classifier must separate build-events from job listings.
func TestClassifyGolden(t *testing.T) {
	cases := []struct {
		name    string
		op      *domain.Opportunity
		verdict domain.Verdict
	}{
		{
			name: "razorpay ai buildathon",
			op: &domain.Opportunity{
				Source: "devpost", Title: "Razorpay AI Buildathon 2026", Org: "Razorpay",
				Description: "Build an AI agent that solves a real payments problem. Teams of 2-4, 48-hour hackathon, submit a working prototype and demo it on demo day. Prize pool of Rs 2,00,000.",
				Kind:        domain.KindBuildathon, DeadlineAt: ptrTime("2026-11-06T00:00:00Z"),
				Prize:   domain.Money{Amount: 200000, Currency: "INR", Text: "₹2,00,000"},
				TeamMin: 2, TeamMax: 4, Eligibility: "students and freshers",
			},
			verdict: domain.VerdictInclude,
		},
		{
			name: "classic hackathon from devpost",
			op: &domain.Opportunity{
				Source: "devpost", Title: "Global AI Hackathon", Org: "Acme",
				Description: "24-hour hackathon. Build a project, win prizes. Judging criteria: innovation, impact. Teams of up to 4. Certificates for all participants.",
				Kind:        domain.KindHackathon, DeadlineAt: ptrTime("2026-12-01T00:00:00Z"),
				Prize: domain.Money{Amount: 10000, Currency: "USD", Text: "$10,000"},
			},
			verdict: domain.VerdictInclude,
		},
		{
			name: "software engineer internship posting",
			op: &domain.Opportunity{
				Source: "unstop", Title: "Software Engineer Internship", Org: "Gogo Pogo",
				Description: "We are hiring interns. Full-time, work from office. Salary of Rs 50,000 per annum. Apply now with your resume. Interview process: 3 rounds. Immediate joiners preferred.",
				Kind:        domain.KindInternship, DeadlineAt: ptrTime("2026-11-09T00:00:00Z"),
			},
			verdict: domain.VerdictExclude,
		},
		{
			name: "marketing internship with exposure",
			op: &domain.Opportunity{
				Source: "unstop", Title: "Marketing Internship", Org: "Some Co",
				Description: "Looking for interns for our marketing team. Monthly stipend of Rs 10,000. You will get exposure to the industry. Apply now. Immediate joining required. Candidates must be available for 6 months.",
				Kind:        domain.KindInternship,
			},
			verdict: domain.VerdictExclude,
		},
		{
			name: "build-based ai internship",
			op: &domain.Opportunity{
				Source: "unstop", Title: "AI Engineer Internship - Build An LLM Product", Org: "Startup Labs",
				Description: "Six week internship where you build and ship an LLM powered feature in our production repository. You will be mentored by the founding team, demo your project at the end, and receive a stipend of Rs 40,000 plus a letter of recommendation.",
				Kind:        domain.KindInternship, DeadlineAt: ptrTime("2026-11-20T00:00:00Z"),
				Prize:   domain.Money{Amount: 40000, Currency: "INR"},
				TeamMax: 1,
			},
			verdict: domain.VerdictInclude,
		},
		{
			name: "fellowship program",
			op: &domain.Opportunity{
				Source: "rss", Title: "Summer of Code Fellowship", Org: "Open Source Foundation",
				Description: "Contribute to open source projects, get mentorship from maintainers, work in a cohort with other fellows. Stipend provided. Application deadline soon.",
				Kind:        domain.KindFellowship, DeadlineAt: ptrTime("2026-10-30T00:00:00Z"),
				Prize: domain.Money{Amount: 3000, Currency: "USD"},
			},
			verdict: domain.VerdictInclude,
		},
		{
			name: "generic conference talk",
			op: &domain.Opportunity{
				Source: "rss", Title: "Cloud Summit 2026", Org: "CloudCo",
				Description: "Two days of talks about cloud infrastructure and AI.",
				Kind:        domain.KindConference,
			},
			verdict: domain.VerdictExclude,
		},
		{
			name: "sparse listing stays uncertain",
			op: &domain.Opportunity{
				Source: "unknown", Title: "Program at Acme", Org: "Acme",
				Description: "Build a project with our mentors. Submit your prototype and demo it on demo day.",
			},
			verdict: domain.VerdictUncertain,
		},
		{
			name: "no signals at all is excluded",
			op: &domain.Opportunity{
				Source: "unknown", Title: "Announcement", Org: "Acme",
				Description: "Our office will be closed on Monday.",
			},
			verdict: domain.VerdictExclude,
		},
	}

	c := New(DefaultConfig())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := c.Classify(tc.op)
			if res.Verdict != tc.verdict {
				t.Errorf("verdict = %s, want %s (score %.2f, reasons: %v)",
					res.Verdict, tc.verdict, res.Score, res.Reasons)
			}
		})
	}
}

func TestClassifyReasonsAreExplainable(t *testing.T) {
	o := &domain.Opportunity{
		Source: "devpost", Title: "Razorpay AI Buildathon", Org: "Razorpay",
		Description: "Build an agent. Teams of 2-4. Prize pool Rs 2,00,000.",
		Kind:        domain.KindBuildathon, DeadlineAt: ptrTime("2026-11-06T00:00:00Z"),
	}
	res := New(DefaultConfig()).Classify(o)
	if len(res.Reasons) == 0 {
		t.Fatal("expected reasons")
	}
	found := false
	for _, r := range res.Reasons {
		if r == "build-vocab:buildathon" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected build-vocab reason, got %v", res.Reasons)
	}
}

func TestClassifyRulesOverrideScore(t *testing.T) {
	o := &domain.Opportunity{
		Source: "unstop", Title: "Sales Internship", Org: "Acme",
		Description: "Full-time role, CTC 12 LPA, apply now.",
		Kind:        domain.KindInternship,
	}
	base := New(DefaultConfig())
	if base.Classify(o).Verdict != domain.VerdictExclude {
		t.Fatal("expected exclude without rules")
	}
	withRules := base.WithRules([]Rule{{
		ID: "keep-sales", Expr: `title =~ /sales/i`, Action: domain.VerdictInclude, Enabled: true,
	}})
	res := withRules.Classify(o)
	if res.Verdict != domain.VerdictInclude {
		t.Fatalf("verdict = %s, want include", res.Verdict)
	}
	if len(res.Reasons) == 0 || res.Reasons[len(res.Reasons)-1] != "rule:keep-sales(include)" {
		t.Errorf("expected rule reason, got %v", res.Reasons)
	}
}

func TestRuleExpressionLanguage(t *testing.T) {
	o := &domain.Opportunity{
		Title:       "Razorpay AI Buildathon",
		Org:         "Razorpay",
		Description: "Build a working prototype",
		Kind:        domain.KindBuildathon,
		Source:      "devpost",
		Tags:        []string{"AI", "Fintech"},
		Prize:       domain.Money{Amount: 100000, Currency: "INR"},
	}
	cases := []struct {
		expr string
		want bool
	}{
		{`title =~ /razorpay/i`, true},
		{`title =~ /paytm/i`, false},
		{`kind == buildathon`, true},
		{`kind == hackathon or kind == buildathon`, true},
		{`org contains "raz"`, true},
		{`not (title =~ /paytm/i)`, true},
		{`source == devpost and description contains "prototype"`, true},
		{`tags contains "AI"`, true},
		{`title =~ /buildathon/ and prize >= 1000`, true},
	}
	for _, tc := range cases {
		r := Rule{ID: "t", Expr: tc.expr, Action: domain.VerdictInclude, Enabled: true}
		got, err := r.Evaluate(o)
		if err != nil {
			t.Fatalf("%s: %v", tc.expr, err)
		}
		if got != tc.want {
			t.Errorf("%s = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestRuleExpressionErrors(t *testing.T) {
	for _, expr := range []string{`title =~ /unclosed`, `title == `, `(title == a`, `title @ x`} {
		r := Rule{ID: "t", Expr: expr, Action: domain.VerdictInclude}
		if _, err := r.Evaluate(&domain.Opportunity{}); err == nil {
			t.Errorf("expected error for %q", expr)
		}
	}
}
