package classify

import "regexp"

// vocabEntry is a lexical signal: a label plus the regular expressions that
// trigger it. Matching is case-insensitive.
type vocabEntry struct {
	label string
	res   []*regexp.Regexp
}

func newVocab(label string, patterns ...string) vocabEntry {
	e := vocabEntry{label: label}
	for _, p := range patterns {
		e.res = append(e.res, regexp.MustCompile("(?i)"+p))
	}
	return e
}

func (e vocabEntry) match(text string) bool {
	for _, re := range e.res {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// buildVocab are words that indicate "you compete by building something".
var buildVocab = []vocabEntry{
	newVocab("hackathon", `\bhack[ -]?athons?\b`, `\bhackfest\b`, `\bhack-a-thon\b`),
	newVocab("buildathon", `\bbuild[ -]?athons?\b`, `\bbuild[ -]?fest\b`, `\bmakeathons?\b`, `\bcreateathons?\b`, `\bcodeathons?\b`),
	newVocab("build", `\bbuild (a|an|your|and|with|using|new|from)\b`, `\bbuilding (a|an|your)\b`, `\bbuild in public\b`, `\bbuild together\b`),
	newVocab("challenge", `\bbuild challenge\b`, `\bdesign challenge\b`, `\bcoding challenge\b`, `\bchallenge\b`),
	newVocab("competition", `\bcompetition\b`, `\bcompetitions\b`, `\bcontest\b`, `\bcontests\b`),
	newVocab("ship", `\bship\b`, `\bshipping\b`, `\bshowcase\b`),
	newVocab("demo", `\bdemo day\b`, `\bdemos?\b`),
	newVocab("prototype", `\bprototype\b`, `\bprototyp(?:e|ing)\b`, `\bmvp\b`, `\bminimum viable\b`),
	newVocab("team", `\bteam of\b`, `\bteams? of \d`, `\bindividual or team\b`, `\bmulti[ -]?disciplinary team\b`),
	newVocab("hack", `\bhack\b`, `\bhacking\b`, `\bjam\b`, `\bbuild night\b`),
	newVocab("duration", `\b\d{2}[ -]?(hour|hours|day|days)\b`, `\bweekend\b`, `\bbootcamp\b`),
	newVocab("idea", `\bidea to\b`, `\bfrom scratch\b`, `\bgreenfield\b`, `\bworking prototype\b`),
	newVocab("submission", `\bsubmit your\b`, `\bsubmission\b`, `\bsubmissions\b`, `\bproject submission\b`),
}

// eventVocab are words typical of events (as opposed to employment).
var eventVocab = []vocabEntry{
	newVocab("prize", `\bprize\b`, `\bprizes\b`, `\bprize pool\b`, `\bcash prize\b`, `\breward\b`, `\brewards\b`),
	newVocab("winner", `\bwinner\b`, `\bwinners\b`, `\bwinning\b`, `\btop 3\b`),
	newVocab("judging", `\bjudging\b`, `\bjudges?\b`, `\bevaluation criteria\b`, `\bjudging criteria\b`, `\bscoring\b`),
	newVocab("certificate", `\bcertificate\b`, `\bcertificates\b`, `\bletter of recommendation\b`),
	newVocab("mentorship", `\bmentorship\b`, `\bmentors?\b`, `\bmentored\b`, `\bguidance\b`, `\bcohort\b`),
	newVocab("opensource", `\bopen[ -]?source\b`, `\bcontribute\b`, `\bcontributing\b`, `\bgithub repository\b`),
	newVocab("stipend", `\bstipend\b`, `\bgrant\b`, `\bfellowship\b`, `\bfellows\b`, `\bresidency\b`, `\bresident\b`),
	newVocab("eligibility", `\beligibility\b`, `\beligible\b`, `\bwho can participate\b`, `\bparticipation criteria\b`),
	newVocab("registration", `\bregistration\b`, `\bregister now\b`, `\bregister\b`, `\bapply\b`),
}

// jobVocab are words typical of employment postings. Each hit reduces the
// score of an item.
var jobVocab = []vocabEntry{
	newVocab("salary", `\bsalary\b`, `\bctc\b`, `\blpa\b`, `\bper annum\b`, `\bp\.?a\.?\b`, `\bpay scale\b`, `\bcompensation\b`, `\brange of \d+ ?(rs|inr|lpa)`),
	newVocab("employment", `\bfull[ -]?time\b`, `\bpart[ -]?time\b`, `\bpermanent\b`, `\bemployment type\b`, `\bwork mode\b`, `\bwork from (office|home)\b`, `\bremote job\b`),
	newVocab("recruitment", `\brecruit(?:er|ment|ing)\b`, `\bhiring\b`, `\bopenings?\b`, `\bvacanc(?:y|ies)\b`, `\bwe are looking for\b`, `\bapply now\b`),
	newVocab("interview", `\binterview (process|round|schedule)\b`, `\baptitude\b`, `\bhr round\b`, `\btechnical round\b`, `\bnotice period\b`),
	newVocab("resume", `\bresume\b`, `\bcurriculum vitae\b`, `\bjob description\b`, `\bjd\b`, `\bportfolio required\b`, `\bapply with\b`),
	newVocab("contract", `\bcontract\b`, `\bcontractual\b`, `\bnotice period\b`, `\bbond\b`, `\bprobation\b`),
	newVocab("monthly", `\bper month\b`, `\bmonthly stipend\b`, `\bunpaid\b`, `\bpaid internship\b`, `\bexposure to\b`),
	newVocab("benefits", `\benefits\b`, `\bperks\b`, `\binsurance\b`, `\bgratuity\b`, `\bleave policy\b`, `\bflexible hours\b`),
	newVocab("candidate", `\bcandidates?\b`, `\bapplicants?\b`, `\bprofile\b`, `\brecruitee?\b`),
	newVocab("joining", `\bimmediate join(?:er|ing)?\b`, `\bquick join(?:er|ing)?\b`, `\bstarting (soon|immediately)\b`),
	newVocab("salesrole", `\bsales\b`, `\bbusiness development\b`, `\bbd intern\b`, `\bmarketing intern\b`, `\boperations intern\b`),
}

// hireVocab are words that indicate the outcome of the build is a job, an
// internship or a contract: the event is judged on what you build and the
// reward is being hired.
var hireVocab = []vocabEntry{
	newVocab("preplacement", `\bpre[ -]?placement\b`, `\bpre[ -]?placed\b`),
	newVocab("internshipoffer", `\binternship opportunit`, `\b1[ -]month free internship\b`, `\binternship at\b`),
	newVocab("hired", `\bhired\b`, `\bhiring based on\b`, `\bget (an |a )?offer\b`, `\boffer letter\b`),
	newVocab("trial", `\btake[ -]?home\b`, `\btrial project\b`, `\bpoc\b`, `\bpaid trial\b`, `\bwork sample\b`),
	newVocab("convert", `\bconvert(?:ed|ing)? to (a )?full[ -]?time\b`, `\bconversion\b`, `\bfull[ -]?time (?:role|opportunity|position)\b`),
	newVocab("portfolio", `\bportfolio\b`, `\bpresent your (?:work|project|build)\b`, `\bshowcase your (?:work|project)\b`, `\bdemo (?:your|to our) (?:project|work|team)\b`),
	newVocab("recommendation", `\bletter of recommendation\b`, `\bprofessional recommendation\b`),
}

// kindWeights maps an explicit kind hint (from the source) to a score delta.
// A decisive kind (hackathon) settles the classification on its own; an
// ambiguous kind (internship) only nudges it.
var kindWeights = map[string]float64{
	"hackathon":   0.45,
	"buildathon":  0.45,
	"challenge":   0.42,
	"competition": 0.40,
	"contest":     0.35,
	"ctf":         0.35,
	"fellowship":  0.40,
	"residency":   0.38,
	"meetup":      0.18,
	"internship":  0.10,
	"conference":  -0.08,
	"job":         -0.45,
	"unknown":     0.0,
}
