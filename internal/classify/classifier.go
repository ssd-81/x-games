// Package classify implements the build-signal classifier: it decides whether
// an opportunity is an event you compete in by building something, or a job
// listing. The classifier is deterministic and fully explainable: every score
// contribution is recorded as a reason.
package classify

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ssd-81/x-games/internal/domain"
)

// Default thresholds.
const (
	DefaultIncludeThreshold = 0.55
	DefaultExcludeThreshold = 0.15
)

// Group caps: the maximum absolute contribution of each signal group. Caps
// keep a single keyword from dominating the decision.
const (
	capBuildVocab = 0.36
	capEventVocab = 0.20
	capJobVocab   = 0.45

	weightBuildVocab = 0.06
	weightEventVocab = 0.03
	weightJobVocab   = 0.06

	weightStructure   = 0.05
	capStructure      = 0.25
	weightSourceTrust = 0.10
)

// Config configures the classifier.
type Config struct {
	IncludeThreshold float64
	ExcludeThreshold float64
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		IncludeThreshold: DefaultIncludeThreshold,
		ExcludeThreshold: DefaultExcludeThreshold,
	}
}

// Result is the outcome of a classification.
type Result struct {
	Score   float64
	Verdict domain.Verdict
	Track   domain.Track
	Host    domain.Host
	Reasons []string
}

// Classifier scores opportunities against the build-signal model.
type Classifier struct {
	cfg   Config
	rules []Rule
}

// New returns a classifier with the given configuration.
func New(cfg Config) *Classifier {
	if cfg.IncludeThreshold <= 0 {
		cfg.IncludeThreshold = DefaultIncludeThreshold
	}
	if cfg.ExcludeThreshold <= 0 {
		cfg.ExcludeThreshold = DefaultExcludeThreshold
	}
	if cfg.ExcludeThreshold >= cfg.IncludeThreshold {
		cfg.ExcludeThreshold = cfg.IncludeThreshold / 2
	}
	return &Classifier{cfg: cfg}
}

// WithRules returns a copy of the classifier that also applies the given
// rules. Rules are evaluated after scoring and can override the verdict.
func (c *Classifier) WithRules(rules []Rule) *Classifier {
	clone := *c
	clone.rules = append([]Rule(nil), rules...)
	return &clone
}

// Rules returns the rules attached to the classifier.
func (c *Classifier) Rules() []Rule { return c.rules }

// Classify scores an opportunity and returns the verdict with its reasons.
func (c *Classifier) Classify(o *domain.Opportunity) Result {
	var reasons []string
	raw := 0.0

	// 1. Explicit kind hint from the source: decisive when it is unambiguous.
	if w, ok := kindWeights[string(o.Kind)]; ok && w != 0 {
		raw += w
		reasons = append(reasons, fmt.Sprintf("%+0.2f kind:%s", w, o.Kind))
	}

	// 2. Source trust: curated build-event sources are more likely relevant.
	if trust := sourceTrust(o.Source); trust > 0 {
		w := weightSourceTrust * trust
		raw += w
		reasons = append(reasons, fmt.Sprintf("%+0.2f source-trust:%s", w, o.Source))
	}

	text := o.SearchText()
	title := strings.ToLower(o.Title + " " + o.Org)

	// 3. Build vocabulary (bounded).
	buildHits := 0
	for _, v := range buildVocab {
		if v.match(text) {
			buildHits++
			reasons = append(reasons, fmt.Sprintf("build-vocab:%s", v.label))
		}
	}
	if buildHits > 0 {
		w := min(float64(buildHits)*weightBuildVocab, capBuildVocab)
		raw += w
		reasons = append(reasons, fmt.Sprintf("%+0.2f build-vocab", w))
	}

	// 4. Event vocabulary (bounded).
	eventHits := 0
	for _, v := range eventVocab {
		if v.match(text) {
			eventHits++
			reasons = append(reasons, fmt.Sprintf("event-vocab:%s", v.label))
		}
	}
	if eventHits > 0 {
		w := min(float64(eventHits)*weightEventVocab, capEventVocab)
		raw += w
		reasons = append(reasons, fmt.Sprintf("%+0.2f event-vocab", w))
	}

	// 5. Event structure: a deadline, a prize, a team size, eligibility and
	// judging criteria are the shape of an event, not of a job posting.
	structure := 0
	if o.DeadlineAt != nil {
		structure++
		reasons = append(reasons, "structure:deadline")
	}
	if !o.Prize.IsZero() {
		structure++
		reasons = append(reasons, "structure:prize")
	}
	if o.TeamMin > 0 || o.TeamMax > 0 {
		structure++
		reasons = append(reasons, "structure:team-size")
	}
	if o.Eligibility != "" {
		structure++
		reasons = append(reasons, "structure:eligibility")
	}
	if o.Judging != "" {
		structure++
		reasons = append(reasons, "structure:judging")
	}
	if structure > 0 {
		w := min(float64(structure)*weightStructure, capStructure)
		raw += w
		reasons = append(reasons, fmt.Sprintf("%+0.2f structure", w))
	}

	// 6. Job vocabulary (bounded, negative).
	jobHits := 0
	for _, v := range jobVocab {
		if v.match(text) {
			jobHits++
			reasons = append(reasons, fmt.Sprintf("job-vocab:%s", v.label))
		}
	}
	if jobHits > 0 {
		w := -min(float64(jobHits)*weightJobVocab, capJobVocab)
		raw += w
		reasons = append(reasons, fmt.Sprintf("%+0.2f job-vocab", w))
	}

	// 7. A job posting without a deadline is not an event: structural penalty.
	if o.DeadlineAt == nil && jobHits > 0 {
		raw -= 0.10
		reasons = append(reasons, "-0.10 job-without-deadline")
	}

	// 8. A pure title match on a sales/marketing role is a job listing even
	// when the body is filled with event vocabulary.
	if matchesAny(jobVocab, title) {
		raw -= 0.05
		reasons = append(reasons, "-0.05 job-vocab-in-title")
	}

	// 9. Hiring track: the build is judged with a hiring outcome in mind.
	// This separates "build something, we might hire you" from a job
	// posting, and it is what turns an internship listing into a build-event.
	hireHits := 0
	for _, v := range hireVocab {
		if v.match(text) {
			hireHits++
			reasons = append(reasons, fmt.Sprintf("hire-vocab:%s", v.label))
		}
	}
	track := domain.TrackUnknown
	if hireHits > 0 && structure >= 2 {
		track = domain.TrackHire
		w := min(float64(hireHits)*0.10, 0.30)
		raw += w
		reasons = append(reasons, fmt.Sprintf("%+0.2f hire-track", w))
	} else if o.Kind == domain.KindFellowship || o.Kind == domain.KindResidency {
		track = domain.TrackLearn
	} else if o.Kind.IsBuildEvent() {
		track = domain.TrackCompete
	}

	// 10. Host detection: company organised events are a strong signal that
	// the listing is a real, build oriented event.
	if o.Host.IsUnknown() && o.Org != "" {
		o.Host = domain.DetectHost(o.Org, nil)
	}
	if o.Host == domain.HostCompany {
		raw += 0.05
		reasons = append(reasons, "+0.05 host:company")
	}

	score := clamp(raw, 0, 1)

	// The verdict is decided on the raw (unclamped) score so that a clearly
	// job-shaped item is not confused with an item that simply has no
	// signals yet.
	verdict := domain.VerdictUncertain
	switch {
	case raw >= c.cfg.IncludeThreshold:
		verdict = domain.VerdictInclude
	case raw <= c.cfg.ExcludeThreshold:
		verdict = domain.VerdictExclude
	}

	// 9. User rules override the score.
	for _, r := range c.rules {
		if !r.Enabled {
			continue
		}
		ok, err := r.Evaluate(o)
		if err != nil || !ok {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("rule:%s(%s)", r.ID, r.Action))
		switch r.Action {
		case domain.VerdictInclude:
			verdict = domain.VerdictInclude
			raw = max(raw, c.cfg.IncludeThreshold)
			score = max(score, c.cfg.IncludeThreshold)
		case domain.VerdictExclude:
			verdict = domain.VerdictExclude
			raw = min(raw, c.cfg.ExcludeThreshold)
			score = min(score, c.cfg.ExcludeThreshold)
		}
	}

	return Result{Score: round2(score), Verdict: verdict, Track: track, Host: o.Host, Reasons: reasons}
}

// Apply stores the classification result on the opportunity.
func (c *Classifier) Apply(o *domain.Opportunity) Result {
	res := c.Classify(o)
	o.Score = res.Score
	o.Verdict = res.Verdict
	o.Track = res.Track
	o.Host = res.Host
	o.Reasons = res.Reasons
	return res
}

// sourceTrust returns the default trust per source. Curated build-event
// sources get a small nudge; unknown sources get none.
func sourceTrust(source string) float64 {
	switch source {
	case "devpost", "unstop", "mlh", "devfolio", "hackerrank":
		return 0.9
	case "ctftime", "codeforces":
		return 0.7
	case "rss", "html":
		return 0.4
	default:
		return 0
	}
}

func matchesAny(entries []vocabEntry, text string) bool {
	for _, v := range entries {
		if v.match(text) {
			return true
		}
	}
	return false
}

func clamp(v, lo, hi float64) float64 {
	return min(max(v, lo), hi)
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// ReasonSummary renders the reasons as a single human readable line.
func ReasonSummary(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	sorted := append([]string(nil), reasons...)
	sort.Strings(sorted)
	return strings.Join(sorted, ", ")
}
