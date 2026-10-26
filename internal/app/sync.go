// Package app contains the use cases of XGames: syncing sources, searching
// the store and watching for new events.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ssd-81/x-games/internal/classify"
	"github.com/ssd-81/x-games/internal/domain"
	"github.com/ssd-81/x-games/internal/store"
)

// Source is the minimal interface the sync engine needs.
type Source interface {
	Name() string
	Describe() domain.SourceMeta
	Fetch(ctx context.Context, req domain.FetchRequest) ([]domain.Opportunity, error)
}

// SyncEngine fetches opportunities from sources, classifies them and stores
// them.
type SyncEngine struct {
	Store      *store.Store
	Classifier *classify.Classifier
	Logger     *slog.Logger
	// Now can be overridden in tests.
	Now func() time.Time
}

// NewSyncEngine returns a sync engine.
func NewSyncEngine(st *store.Store, cl *classify.Classifier, logger *slog.Logger) *SyncEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &SyncEngine{Store: st, Classifier: cl, Logger: logger, Now: time.Now}
}

// SyncResult reports the outcome of a sync run.
type SyncResult struct {
	Source    string        `json:"source"`
	Fetched   int           `json:"fetched"`
	Inserted  int           `json:"inserted"`
	Updated   int           `json:"updated"`
	Skipped   int           `json:"skipped"`
	Included  int           `json:"included"`
	Excluded  int           `json:"excluded"`
	Uncertain int           `json:"uncertain"`
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error,omitempty"`
}

// SyncOptions configures a sync run.
type SyncOptions struct {
	// Pages to fetch per source (0 = one page).
	Pages int
	// Limit per page (0 = source default).
	Limit int
	// Sources restricts the run to the named sources.
	Sources []string
	// Types restricts which opportunity types a source should return.
	Types []string
	// Only classifies and stores items matching the filter.
	Only *store.Filter
}

// Sync fetches from the given sources and stores the results.
func (e *SyncEngine) Sync(ctx context.Context, sources []Source, opts SyncOptions) ([]SyncResult, error) {
	var wg sync.WaitGroup
	results := make([]SyncResult, len(sources))
	limit := 4
	sem := make(chan struct{}, limit)

	for i, src := range sources {
		wg.Add(1)
		go func(i int, src Source) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = e.syncOne(ctx, src, opts)
		}(i, src)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i].Source < results[j].Source })
	return results, nil
}

func (e *SyncEngine) syncOne(ctx context.Context, src Source, opts SyncOptions) SyncResult {
	start := time.Now()
	res := SyncResult{Source: src.Name()}

	run := store.SyncRun{Source: src.Name(), StartedAt: start, Status: "ok"}

	// Adapters page internally; --pages simply multiplies the batch size.
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if opts.Pages > 1 {
		limit *= opts.Pages
	}
	req := domain.FetchRequest{Page: 0, Limit: limit, Types: opts.Types}
	items, err := src.Fetch(ctx, req)
	fetched := items
	if err != nil {
		run.Status = "error"
		run.Error = err.Error()
		res.Error = err.Error()
		e.Logger.Warn("source failed", "source", src.Name(), "error", err)
	}

	run.Fetched = len(fetched)
	res.Fetched = len(fetched)

	for _, item := range fetched {
		o := item
		o.ID = o.ComputeID()
		if o.ContentHash == "" {
			o.ContentHash = o.ComputeContentHash()
		}
		if o.Kind == domain.KindUnknown {
			o.Kind = src.Describe().DefaultKind
		}
		e.Classifier.Apply(&o)

		switch o.Verdict {
		case domain.VerdictInclude:
			res.Included++
		case domain.VerdictExclude:
			res.Excluded++
		default:
			res.Uncertain++
		}

		up, err := e.Store.Upsert(&o)
		if err != nil {
			run.Skipped++
			res.Skipped++
			e.Logger.Warn("upsert failed", "source", src.Name(), "title", o.Title, "error", err)
			continue
		}
		if up.Inserted {
			run.Inserted++
			res.Inserted++
		} else if up.Updated {
			run.Updated++
			res.Updated++
		}
	}

	run.FinishedAt = time.Now()
	run.DurationMS = run.FinishedAt.Sub(run.StartedAt).Milliseconds()
	res.Duration = run.FinishedAt.Sub(run.StartedAt)
	if err := e.Store.RecordSyncRun(run); err != nil {
		e.Logger.Warn("could not record sync run", "source", src.Name(), "error", err)
	}
	return res
}

// SyncResultSummary renders a one line summary of a sync run.
func SyncResultSummary(results []SyncResult) string {
	var total, inserted, updated, failed int
	for _, r := range results {
		total += r.Fetched
		inserted += r.Inserted
		updated += r.Updated
		if r.Error != "" {
			failed++
		}
	}
	return fmt.Sprintf("fetched %d, new %d, updated %d, failed sources %d", total, inserted, updated, failed)
}
