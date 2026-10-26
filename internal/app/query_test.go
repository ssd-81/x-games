package app

import "testing"

func TestParseQueryFilters(t *testing.T) {
	pq := ParseQuery("ai buildathon kind:hackathon host:company track:hire prize:>50000 deadline:<30d -internship")

	if len(pq.Kinds) != 1 || pq.Kinds[0] != "hackathon" {
		t.Errorf("kinds = %v", pq.Kinds)
	}
	if len(pq.Hosts) != 1 || pq.Hosts[0] != "company" {
		t.Errorf("hosts = %v", pq.Hosts)
	}
	if len(pq.Tracks) != 1 || pq.Tracks[0] != "hire" {
		t.Errorf("tracks = %v", pq.Tracks)
	}
	if pq.MinPrize != 50000 {
		t.Errorf("min prize = %v", pq.MinPrize)
	}
	if pq.DeadlineBefore == nil {
		t.Error("expected a deadline filter")
	}
	if pq.Match == "" {
		t.Error("expected a full text match")
	}
}

func TestParseQueryNegation(t *testing.T) {
	pq := ParseQuery("kind:!internship source:!unstop")
	if len(pq.ExcludeKinds) != 1 || pq.ExcludeKinds[0] != "internship" {
		t.Errorf("excluded kinds = %v", pq.ExcludeKinds)
	}
	if len(pq.ExcludeSources) != 1 || pq.ExcludeSources[0] != "unstop" {
		t.Errorf("excluded sources = %v", pq.ExcludeSources)
	}
	f := pq.Filter(10, "relevance")
	if f.Exclude["kind"] == nil || f.Exclude["source"] == nil {
		t.Fatalf("expected exclusions in the filter: %+v", f.Exclude)
	}
}

func TestParseQueryEmpty(t *testing.T) {
	pq := ParseQuery("")
	if pq.Match != "" {
		t.Errorf("expected no match expression, got %q", pq.Match)
	}
	f := pq.Filter(10, "relevance")
	if f.Match != "" {
		t.Errorf("expected no match in the filter, got %q", f.Match)
	}
}
