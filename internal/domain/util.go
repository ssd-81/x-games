package domain

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func itoa(i int) string { return strconv.Itoa(i) }

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// DedupKey builds a cross-source dedup key from the normalized title, the
// organizer and the day of the deadline.
func DedupKey(title, org string, deadline *time.Time) string {
	t := normalizeText(title)
	o := normalizeText(org)
	d := "0000-00-00"
	if deadline != nil {
		d = deadline.UTC().Format("2006-01-02")
	}
	sum := sha1.Sum([]byte(t + "|" + o + "|" + d))
	return hex.EncodeToString(sum[:])[:16]
}

// StripHTML removes tags from an HTML fragment and collapses whitespace.
// It is intentionally simple: XGames renders plain text only.
func StripHTML(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
			b.WriteRune(' ')
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	out := b.String()
	var fields []string
	for _, f := range strings.Fields(out) {
		fields = append(fields, decodeEntities(f))
	}
	return strings.Join(fields, " ")
}

var entities = strings.NewReplacer(
	"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
	"&quot;", "\"", "&#39;", "'", "&apos;", "'", "&mdash;", "-", "&ndash;", "-",
	"&rsquo;", "'", "&lsquo;", "'", "&ldquo;", "\"", "&rdquo;", "\"", "&hellip;", "...",
	"&copy;", "(c)", "&reg;", "(r)", "&bull;", "*",
)

// decodeEntities decodes the HTML entities that survive word splitting.
func decodeEntities(s string) string {
	s = entities.Replace(s)
	// Numeric entities.
	for strings.Contains(s, "&#") {
		start := strings.Index(s, "&#")
		end := strings.Index(s[start:], ";")
		if end < 0 {
			break
		}
		end += start
		code := s[start+2 : end]
		var r rune
		var err error
		if strings.HasSuffix(code, "x") || strings.HasSuffix(code, "X") {
			n, e := strconv.ParseInt(code[:len(code)-1], 16, 32)
			if e == nil {
				r, err = rune(n), nil
			}
		} else {
			n, e := strconv.ParseInt(code, 10, 32)
			if e == nil {
				r, err = rune(n), nil
			}
		}
		if err != nil {
			break
		}
		s = s[:start] + string(r) + s[end+1:]
	}
	return s
}

// Truncate shortens a string to n runes, appending an ellipsis when cut.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// TitleCase upper-cases the first letter of every word.
func TitleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		r := []rune(w)
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// FormatDeadline renders a deadline relative to now, e.g. "closes in 4d".
func FormatDeadline(deadline *time.Time, now time.Time) string {
	if deadline == nil {
		return "no deadline"
	}
	d := deadline.Sub(now)
	if d < 0 {
		return "closed"
	}
	if d < time.Hour {
		return fmt.Sprintf("closes in %dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("closes in %dh", int(d.Hours()))
	}
	return fmt.Sprintf("closes in %dd", int(d.Hours()/24))
}
