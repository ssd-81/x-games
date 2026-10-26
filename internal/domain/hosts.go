package domain

import "strings"

// Host describes who organises an event. It separates company run events
// from university fests, community events and one person side projects.
type Host string

const (
	HostUnknown    Host = "unknown"
	HostCompany    Host = "company"
	HostUniversity Host = "university"
	HostCommunity  Host = "community"
	HostIndividual Host = "individual"
)

// IsUnknown reports whether the host is unknown (also true for the zero
// value).
func (h Host) IsUnknown() bool { return h == "" || h == HostUnknown }

// Label returns a human readable label for the host.
func (h Host) Label() string {
	switch h {
	case HostUnknown:
		return ""
	default:
		return string(h)
	}
}

// Track describes the outcome an event offers. It is the axis that separates
// "win a prize" from "get hired for what you build" and from "learn".
type Track string

const (
	TrackUnknown Track = "unknown"
	// TrackCompete is the classic build-event: prizes, winners, rankings.
	TrackCompete Track = "compete"
	// TrackHire is a build that is judged with a hiring outcome in mind:
	// pre-placement internships, job offers, trials, take-home challenges.
	TrackHire Track = "hire"
	// TrackLearn is a fellowship, residency or mentorship program.
	TrackLearn Track = "learn"
)

// IsUnknown reports whether the track is unknown (also true for the zero
// value).
func (t Track) IsUnknown() bool { return t == "" || t == TrackUnknown }

// Label returns a human readable label for the track.
func (t Track) Label() string {
	if t == TrackUnknown {
		return ""
	}
	return string(t)
}

// companySuffixes mark an organisation as a company.
var companySuffixes = []string{
	"inc", "inc.", "ltd", "ltd.", "pvt", "pvt.", "llp", "llc", "corp", "corp.",
	"corporation", "technologies", "technology", "systems", "labs", "software",
	"solutions", "ventures", "capital", "bank", "motors", "airlines", "media",
	"digital", "networks", "platforms", "consulting", "services", "group",
	"company", "co.", "holdings", "enterprises", "industries", "studios",
}

// universityMarkers mark an organisation as an academic institution.
var universityMarkers = []string{
	"university", "universität", "college", "institute of technology",
	"institute of science", "institute of engineering", "polytechnic",
	"school of", "academy", "iit", "nit", "vit", "b.tech", "campus",
	"faculty", "departmental", "students union", "shastra", "vidyalaya",
}

// communityMarkers mark an organisation as a community or student group.
var communityMarkers = []string{
	"hack club", "code club", "coding club", "dev club", "student club",
	"student organization", "studentchakra", "community", "collective",
	"association", "society", "federation", "chapter", "user group", "oss",
	"open source", "club", "guild", "foundation", "initiative",
}

// individualMarkers mark an organisation as a single person.
var individualMarkers = []string{
	"'s hackathon", "for _", "presents", "by @",
}

// KnownCompanies is a small built-in registry of companies that frequently
// run build-events. It is a hint, not a blocklist: the heuristics below carry
// most of the load and users can extend the list in their configuration.
var KnownCompanies = map[string]bool{}

func init() {
	for _, c := range []string{
		"amazon", "aws", "google", "alphabet", "meta", "facebook", "instagram",
		"whatsapp", "microsoft", "azure", "github", "gitlab", "apple", "nvidia",
		"openai", "anthropic", "deepmind", "huggingface", "mistral", "cohere",
		"ibm", "oracle", "salesforce", "adobe", "figma", "notion", "linear",
		"vercel", "netlify", "cloudflare", "digitalocean", "heroku", "render",
		"railway", "fly.io", "supabase", "mongodb", "redis", "elastic", "datadog",
		"stripe", "razorpay", "paypal", "visa", "mastercard", "zoho", "freshworks",
		"browserstack", "twilio", "hubspot", "zapier", "slack", "discord", "zoom",
		"spotify", "netflix", "twitch", "uber", "lyft", "airbnb", "dropbox",
		"shopify", "squarespace", "canva", "miro", "asana", "twilio", "okta",
		"hashicorp", "databricks", "snowflake", "confluent", "mongodb", "tcs",
		"infosys", "tcs", "wipro", "hcl", "tech mahindra", "cognizant", "lti",
		"hacker earth", "hackerearth", "samsung", "samsung research", "xiaomi",
		"oneplus", "oppo", "vivo", "realme", "boat", "dell", "hp", "lenovo",
		"asus", "acer", "sony", "lg", "philips", "siemens", "bosch", "intel",
		"amd", "qualcomm", "arm", "snyk", "hashi", "fastly", "akamai", "f5",
		"thoughtworks", "cognizant", "amdocs", "nvidia", "cerebras", "groq",
		"together ai", "replicate", "runway", "scale ai", "weights and biases",
	} {
		KnownCompanies[c] = true
	}
}

// DetectHost guesses who hosts an event from its organiser name.
// registry holds extra names (from the user configuration).
func DetectHost(org string, registry map[string]bool) Host {
	name := strings.ToLower(strings.TrimSpace(org))
	if name == "" {
		return HostUnknown
	}
	if registry != nil && registry[name] {
		return HostCompany
	}
	if KnownCompanies[name] {
		return HostCompany
	}
	// Academic institutions are checked before the company suffixes: names
	// like "Indian Institute of Technology" end with a company-looking word.
	for _, m := range universityMarkers {
		if strings.Contains(name, m) {
			return HostUniversity
		}
	}
	for _, m := range communityMarkers {
		if strings.Contains(name, m) {
			return HostCommunity
		}
	}
	// A company name with a legal suffix.
	for _, s := range companySuffixes {
		if strings.HasSuffix(name, " "+s) || strings.HasSuffix(name, " "+s+".") {
			return HostCompany
		}
	}
	for _, m := range individualMarkers {
		if strings.Contains(name, m) {
			return HostIndividual
		}
	}
	// A single capitalised word is often a brand: Meta, PayPal, Twitch.
	if isSingleBrandWord(name) {
		return HostCompany
	}
	return HostUnknown
}

// isSingleBrandWord reports whether the name looks like a brand name
// (one word, no institution markers).
func isSingleBrandWord(name string) bool {
	if strings.ContainsAny(name, " .,&") {
		return false
	}
	return len(name) >= 3
}

// IsCompanyHost reports whether the host is a company.
func (h Host) IsCompanyHost() bool { return h == HostCompany }
