package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ssd-81/x-games/internal/classify"
	"github.com/ssd-81/x-games/internal/domain"
)

// Rule is a stored user rule.
type Rule struct {
	ID        string
	Expr      string
	Action    domain.Verdict
	Origin    string
	Enabled   bool
	CreatedAt time.Time
}

// ListRules returns every stored rule ordered by creation.
func (s *Store) ListRules() ([]Rule, error) {
	rows, err := s.db.Query(`SELECT id, expr, action, origin, enabled, created_at FROM rules ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		var enabled int
		var created sql.NullString
		if err := rows.Scan(&r.ID, &r.Expr, &r.Action, &r.Origin, &enabled, &created); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddRule stores a rule.
func (s *Store) AddRule(r Rule) error {
	if r.ID == "" {
		return fmt.Errorf("rule id is required")
	}
	if r.Origin == "" {
		r.Origin = "db"
	}
	_, err := s.db.Exec(`INSERT INTO rules(id, expr, action, origin, enabled, created_at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET expr=excluded.expr, action=excluded.action, enabled=excluded.enabled`,
		r.ID, r.Expr, string(r.Action), r.Origin, boolToInt(r.Enabled), fmtTimeVal(time.Now().UTC()))
	return err
}

// DeleteRule removes a rule.
func (s *Store) DeleteRule(id string) error {
	_, err := s.db.Exec(`DELETE FROM rules WHERE id = ?`, id)
	return err
}

// SetRuleEnabled enables or disables a rule.
func (s *Store) SetRuleEnabled(id string, enabled bool) error {
	_, err := s.db.Exec(`UPDATE rules SET enabled = ? WHERE id = ?`, boolToInt(enabled), id)
	return err
}

// ToClassifyRule converts a stored rule into a classifier rule.
func (r Rule) ToClassifyRule() classify.Rule {
	return classify.Rule{
		ID:      r.ID,
		Expr:    r.Expr,
		Action:  r.Action,
		Origin:  r.Origin,
		Enabled: r.Enabled,
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
