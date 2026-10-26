package app

import (
	"fmt"
	"log/slog"

	"github.com/ssd-81/x-games/internal/classify"
	"github.com/ssd-81/x-games/internal/config"
	"github.com/ssd-81/x-games/internal/domain"
	"github.com/ssd-81/x-games/internal/store"
)

// LoadRules merges the rules stored in the database with the rules from the
// configuration file. Database rules take precedence: they are the ones the
// user created interactively.
func LoadRules(st *store.Store, cfg config.Config, logger *slog.Logger) []classify.Rule {
	var out []classify.Rule
	if st != nil {
		stored, err := st.ListRules()
		if err != nil && logger != nil {
			logger.Warn("could not read the rules", "error", err)
		}
		for _, r := range stored {
			out = append(out, r.ToClassifyRule())
		}
	}
	for _, r := range cfg.Rules {
		action := domain.VerdictExclude
		switch r.Action {
		case "include":
			action = domain.VerdictInclude
		case "exclude":
			action = domain.VerdictExclude
		default:
			continue
		}
		out = append(out, classify.Rule{
			ID:      r.ID,
			Expr:    r.When,
			Action:  action,
			Origin:  "config",
			Enabled: true,
		})
	}
	return out
}

// ClassifierWithRules builds a classifier that applies the given rules.
func ClassifierWithRules(base *classify.Classifier, rules []classify.Rule) *classify.Classifier {
	if base == nil {
		base = classify.New(classify.DefaultConfig())
	}
	return base.WithRules(rules)
}

// ValidateRule checks that a rule expression compiles and that its action is
// valid.
func ValidateRule(expr, action string) error {
	if expr == "" {
		return fmt.Errorf("the rule expression is empty")
	}
	switch action {
	case "include", "exclude":
	default:
		return fmt.Errorf("unknown action %q (use include or exclude)", action)
	}
	return nil
}
