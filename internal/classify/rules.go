package classify

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ssd-81/x-games/internal/domain"
)

// Rule is a user defined include/exclude rule.
type Rule struct {
	ID      string
	Expr    string
	Action  domain.Verdict
	Origin  string // "db" (from the TUI) or "config"
	Enabled bool
}

// Evaluate evaluates the rule against an opportunity.
func (r Rule) Evaluate(o *domain.Opportunity) (bool, error) {
	p, err := parseExpr(r.Expr)
	if err != nil {
		return false, err
	}
	ctx := contextFor(o)
	return p.eval(ctx), nil
}

// ---------------------------------------------------------------------------
// Expression language
//
//   title =~ /sales|growth/i
//   kind == hackathon and prize >= 50000
//   description contains "buildathon" or org =~ /razorpay/
//   not (title =~ /intern/i) and source == unstop
//
// ---------------------------------------------------------------------------

type expr interface {
	eval(ctx evalContext) bool
}

type evalContext interface {
	field(name string) (string, bool)
}

type opContext map[string]string

func (c opContext) field(name string) (string, bool) {
	v, ok := c[name]
	return v, ok
}

func contextFor(o *domain.Opportunity) opContext {
	return opContext{
		"title":       o.Title,
		"org":         o.Org,
		"description": o.Description,
		"eligibility": o.Eligibility,
		"kind":        string(o.Kind),
		"source":      o.Source,
		"region":      o.Region,
		"status":      string(o.Status),
		"mode":        string(o.Mode),
		"tags":        strings.Join(o.Tags, ", "),
		"url":         o.URL,
		"prize":       strconv.FormatFloat(o.Prize.Amount, 'f', -1, 64),
	}
}

type andExpr struct{ left, right expr }

func (e andExpr) eval(ctx evalContext) bool { return e.left.eval(ctx) && e.right.eval(ctx) }

type orExpr struct{ left, right expr }

func (e orExpr) eval(ctx evalContext) bool { return e.left.eval(ctx) || e.right.eval(ctx) }

type notExpr struct{ inner expr }

func (e notExpr) eval(ctx evalContext) bool { return !e.inner.eval(ctx) }

type cmpExpr struct {
	left  operand
	op    string
	right operand
	re    *regexp.Regexp // pre-compiled for the "=~" operator
}

func (e cmpExpr) eval(ctx evalContext) bool {
	l, lok := e.left.value(ctx)
	r, rok := e.right.value(ctx)
	if !lok || !rok {
		return false
	}
	switch e.op {
	case "=~", "matches":
		if e.re == nil {
			return false
		}
		return e.re.MatchString(l)
	case "==":
		return strings.EqualFold(l, r)
	case "!=":
		return !strings.EqualFold(l, r)
	case "contains":
		return strings.Contains(strings.ToLower(l), strings.ToLower(r))
	case ">":
		lf, err1 := strconv.ParseFloat(l, 64)
		rf, err2 := strconv.ParseFloat(r, 64)
		return err1 == nil && err2 == nil && lf > rf
	case ">=":
		lf, err1 := strconv.ParseFloat(l, 64)
		rf, err2 := strconv.ParseFloat(r, 64)
		return err1 == nil && err2 == nil && lf >= rf
	case "<":
		lf, err1 := strconv.ParseFloat(l, 64)
		rf, err2 := strconv.ParseFloat(r, 64)
		return err1 == nil && err2 == nil && lf < rf
	case "<=":
		lf, err1 := strconv.ParseFloat(l, 64)
		rf, err2 := strconv.ParseFloat(r, 64)
		return err1 == nil && err2 == nil && lf <= rf
	}
	return false
}

type operand interface {
	value(ctx evalContext) (string, bool)
}

type fieldOperand struct{ name string }

func (f fieldOperand) value(ctx evalContext) (string, bool) { return ctx.field(f.name) }

type literalOperand struct{ text string }

func (l literalOperand) value(evalContext) (string, bool) { return l.text, true }

// --- tokenizer ---

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokString
	tokRegex
	tokNumber
	tokOp
	tokLParen
	tokRParen
)

type token struct {
	kind tokenKind
	text string
}

func tokenize(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{tokLParen, "("})
			i++
		case c == ')':
			toks = append(toks, token{tokRParen, ")"})
			i++
		case c == '"' || c == '\'':
			quote := c
			j := i + 1
			var b strings.Builder
			for j < len(s) && s[j] != quote {
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
				b.WriteByte(s[j])
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("unterminated string in expression")
			}
			toks = append(toks, token{tokString, b.String()})
			i = j + 1
		case c == '/':
			j := i + 1
			var b strings.Builder
			for j < len(s) && s[j] != '/' {
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
				b.WriteByte(s[j])
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("unterminated regex in expression")
			}
			pattern := b.String()
			j++
			// Optional flags, e.g. /foo/i
			flags := ""
			for j < len(s) && isIdentChar(s[j]) {
				flags += string(s[j])
				j++
			}
			toks = append(toks, token{tokRegex, buildRegex(pattern, flags)})
			i = j
		case c == '=' && i+1 < len(s) && s[i+1] == '~':
			toks = append(toks, token{tokOp, "=~"})
			i += 2
		case c == '=' && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, token{tokOp, "=="})
			i += 2
		case c == '!' && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, token{tokOp, "!="})
			i += 2
		case c == '>' && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, token{tokOp, ">="})
			i += 2
		case c == '<' && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, token{tokOp, "<="})
			i += 2
		case c == '>' || c == '<':
			toks = append(toks, token{tokOp, string(c)})
			i++
		case isIdentStart(c):
			j := i
			for j < len(s) && isIdentChar(s[j]) {
				j++
			}
			toks = append(toks, token{tokIdent, s[i:j]})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			toks = append(toks, token{tokNumber, s[i:j]})
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q in expression", c)
		}
	}
	toks = append(toks, token{tokEOF, ""})
	return toks, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// buildRegex turns a user supplied pattern plus optional flags into a Go
// regular expression. Matching is case-insensitive by default.
func buildRegex(pattern, flags string) string {
	prefix := ""
	if strings.Contains(flags, "i") {
		prefix += "(?i)"
	}
	if strings.Contains(flags, "m") {
		prefix += "(?m)"
	}
	if strings.Contains(flags, "s") {
		prefix += "(?s)"
	}
	if prefix == "" {
		prefix = "(?i)"
	}
	return prefix + "(" + pattern + ")"
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '.'
}

// --- parser ---

type parser struct {
	toks []token
	pos  int
}

func parseExpr(src string) (expr, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	e, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q in expression", p.peek().text)
	}
	return e, nil
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) next() token {
	t := p.toks[p.pos]
	p.pos++
	return t
}

func (p *parser) parseOr() (expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "or") {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orExpr{left, right}
	}
	return left, nil
}

func (p *parser) parseAnd() (expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "and") {
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = andExpr{left, right}
	}
	return left, nil
}

func (p *parser) parseUnary() (expr, error) {
	if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "not") {
		p.next()
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return notExpr{inner}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (expr, error) {
	t := p.peek()
	if t.kind == tokLParen {
		p.next()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("missing closing parenthesis in expression")
		}
		p.next()
		return inner, nil
	}
	left, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	opTok := p.next()
	op := opTok.text
	switch opTok.kind {
	case tokOp:
	case tokIdent:
		switch strings.ToLower(opTok.text) {
		case "contains", "matches":
			op = strings.ToLower(opTok.text)
		default:
			return nil, fmt.Errorf("expected an operator, got %q", opTok.text)
		}
	default:
		return nil, fmt.Errorf("expected an operator, got %q", opTok.text)
	}
	right, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	e := cmpExpr{left: left, op: op, right: right}
	if op == "=~" || op == "matches" {
		pattern := right.(literalOperand).text
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", pattern, err)
		}
		e.re = re
	}
	return e, nil
}

func (p *parser) parseOperand() (operand, error) {
	t := p.next()
	switch t.kind {
	case tokString:
		return literalOperand{text: t.text}, nil
	case tokRegex:
		return literalOperand{text: t.text}, nil
	case tokNumber:
		return literalOperand{text: t.text}, nil
	case tokIdent:
		switch strings.ToLower(t.text) {
		case "and", "or", "not":
			return nil, fmt.Errorf("unexpected keyword %q", t.text)
		}
		if knownFields[strings.ToLower(t.text)] {
			return fieldOperand{strings.ToLower(t.text)}, nil
		}
		// Unknown identifiers are treated as literals, so that
		// "kind == hackathon" compares against the word hackathon.
		return literalOperand{text: t.text}, nil
	}
	return nil, fmt.Errorf("unexpected token %q in expression", t.text)
}

// knownFields are the fields an expression can reference.
var knownFields = map[string]bool{
	"title":       true,
	"org":         true,
	"description": true,
	"eligibility": true,
	"kind":        true,
	"source":      true,
	"region":      true,
	"status":      true,
	"mode":        true,
	"tags":        true,
	"url":         true,
	"prize":       true,
}
