package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
	"github.com/ssd-81/x-games/internal/notify"
	"github.com/ssd-81/x-games/internal/store"
)

// WatchService monitors the sources and notifies about new, updated and
// closing build-events.
type WatchService struct {
	Store        *store.Store
	Engine       *SyncEngine
	Notifier     notify.Notifier
	Logger       *slog.Logger
	MaxPerRun    int
	QuietHours   string
	RemindBefore []time.Duration
	// Lookback is the window in which a new item is considered "new".
	Lookback time.Duration
}

// NewWatchService returns a watch service.
func NewWatchService(st *store.Store, engine *SyncEngine, n notify.Notifier, logger *slog.Logger) *WatchService {
	if logger == nil {
		logger = slog.Default()
	}
	return &WatchService{
		Store:        st,
		Engine:       engine,
		Notifier:     n,
		Logger:       logger,
		MaxPerRun:    10,
		RemindBefore: []time.Duration{72 * time.Hour, 24 * time.Hour},
		Lookback:     10 * time.Minute,
	}
}

// WatchReport summarizes one watch cycle.
type WatchReport struct {
	Synced      int      `json:"synced"`
	New         []string `json:"new"`
	Updated     []string `json:"updated"`
	ClosingSoon []string `json:"closing_soon"`
	Notified    int      `json:"notified"`
	Errors      []string `json:"errors,omitempty"`
}

// RunOnce performs one watch cycle: sync, diff and notify.
func (w *WatchService) RunOnce(ctx context.Context, sources []Source, opts SyncOptions, profiles []Profile) (WatchReport, error) {
	var report WatchReport

	results, err := w.Engine.Sync(ctx, sources, opts)
	if err != nil {
		return report, err
	}
	for _, r := range results {
		report.Synced += r.Fetched
		if r.Error != "" {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %s", r.Source, r.Error))
		}
	}

	maxPerRun := w.MaxPerRun
	if maxPerRun <= 0 {
		maxPerRun = 10
	}
	notified := 0

	for _, profile := range profiles {
		filter := profile.Filter

		// 1. Items that appeared or changed during the last cycle.
		lookback := w.Lookback
		if lookback <= 0 {
			lookback = 10 * time.Minute
		}
		since := time.Now().Add(-lookback)
		now := time.Now()
		fresh := store.Filter{
			Match:        filter.Match,
			Kinds:        filter.Kinds,
			Sources:      filter.Sources,
			Regions:      filter.Regions,
			Modes:        filter.Modes,
			Tags:         filter.Tags,
			MinPrize:     filter.MinPrize,
			MaxPrize:     filter.MaxPrize,
			Verdict:      "include",
			Limit:        maxPerRun * 3,
			Sort:         "newest",
			UpdatedAfter: &since,
			// Only notify about events that are still open.
			DeadlineAfter: &now,
		}
		if filter.DeadlineBefore != nil {
			fresh.DeadlineBefore = filter.DeadlineBefore
		}
		res, err := w.Store.Search(fresh)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", profile.Name, err))
		} else {
			for _, o := range res.Items {
				if notified >= maxPerRun {
					break
				}
				if !profileMatches(profile, o) {
					continue
				}
				kind := notify.KindUpdated
				if o.FirstSeenAt.After(time.Now().Add(-lookback)) {
					kind = notify.KindNew
				}
				sent, err := w.notifyOnce(ctx, o, kind, profile)
				if err != nil {
					report.Errors = append(report.Errors, err.Error())
					continue
				}
				if sent {
					notified++
					if kind == notify.KindNew {
						report.New = append(report.New, o.Title)
					} else {
						report.Updated = append(report.Updated, o.Title)
					}
				}
			}
		}

		// 2. Items that are about to close.
		for _, remind := range w.RemindBefore {
			if notified >= maxPerRun {
				break
			}
			now := time.Now()
			closing := store.Filter{
				Match:         filter.Match,
				Kinds:         filter.Kinds,
				Sources:       filter.Sources,
				Regions:       filter.Regions,
				Modes:         filter.Modes,
				Tags:          filter.Tags,
				MinPrize:      filter.MinPrize,
				MaxPrize:      filter.MaxPrize,
				Verdict:       "include",
				Limit:         maxPerRun,
				Sort:          "deadline",
				DeadlineAfter: &now,
			}
			deadline := now.Add(remind)
			closing.DeadlineBefore = &deadline
			res, err := w.Store.Search(closing)
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", profile.Name, err))
				break
			}
			for _, o := range res.Items {
				if notified >= maxPerRun {
					break
				}
				if !profileMatches(profile, o) {
					continue
				}
				sent, err := w.notifyOnce(ctx, o, notify.KindClosing, profile)
				if err != nil {
					report.Errors = append(report.Errors, err.Error())
					continue
				}
				if sent {
					notified++
					report.ClosingSoon = append(report.ClosingSoon, o.Title)
				}
			}
		}
	}
	report.Notified = notified
	return report, nil
}

// notifyOnce sends a notification unless it was already sent for this
// opportunity, kind and channel.
func (w *WatchService) notifyOnce(ctx context.Context, o *domain.Opportunity, kind notify.Kind, profile Profile) (bool, error) {
	if w.Notifier == nil {
		return false, nil
	}
	channel := w.Notifier.Name()
	already, err := w.Store.NotificationSent(o.ID, channel, string(kind))
	if err != nil {
		return false, err
	}
	if already {
		return false, nil
	}
	msg := notify.Message{
		Kind:        kind,
		Opportunity: o,
		Title:       notificationTitle(kind, o),
		Body:        notificationBody(o),
		URL:         o.URL,
		SentAt:      time.Now(),
	}
	if quiet := w.inQuietHours(time.Now()); quiet && kind == notify.KindClosing {
		return false, nil
	}
	if err := w.Notifier.Send(ctx, msg); err != nil {
		_ = w.Store.RecordNotification(o.ID, channel, string(kind), false, err.Error())
		return false, fmt.Errorf("notify %s: %w", o.Title, err)
	}
	if err := w.Store.RecordNotification(o.ID, channel, string(kind), true, ""); err != nil {
		w.Logger.Warn("could not record notification", "error", err)
	}
	return true, nil
}

// Profile is a watch filter.
type Profile struct {
	Name   string
	Filter store.Filter
}

// profileMatches applies the extra (non SQL) constraints of a profile.
func profileMatches(p Profile, o *domain.Opportunity) bool {
	if p.Filter.Org != "" && !strings.Contains(strings.ToLower(o.Org), strings.ToLower(p.Filter.Org)) {
		return false
	}
	return true
}

func notificationTitle(kind notify.Kind, o *domain.Opportunity) string {
	switch kind {
	case notify.KindNew:
		return "New build-event: " + o.Title
	case notify.KindUpdated:
		return "Updated: " + o.Title
	case notify.KindClosing:
		return "Closing soon: " + o.Title
	default:
		return o.Title
	}
}

func notificationBody(o *domain.Opportunity) string {
	var parts []string
	if o.Org != "" {
		parts = append(parts, o.Org)
	}
	parts = append(parts, string(o.Kind))
	if !o.Prize.IsZero() {
		parts = append(parts, o.Prize.String())
	}
	if o.DeadlineAt != nil {
		parts = append(parts, domain.FormatDeadline(o.DeadlineAt, time.Now()))
	}
	return strings.Join(parts, " · ")
}

// inQuietHours reports whether the current time is inside the quiet window,
// e.g. "23:00-08:00".
func (w *WatchService) inQuietHours(t time.Time) bool {
	if w.QuietHours == "" {
		return false
	}
	start, end, ok := strings.Cut(w.QuietHours, "-")
	if !ok {
		return false
	}
	s, err1 := time.Parse("15:04", strings.TrimSpace(start))
	e, err2 := time.Parse("15:04", strings.TrimSpace(end))
	if err1 != nil || err2 != nil {
		return false
	}
	nowMin := t.Hour()*60 + t.Minute()
	startMin := s.Hour()*60 + s.Minute()
	endMin := e.Hour()*60 + e.Minute()
	if startMin <= endMin {
		return nowMin >= startMin && nowMin < endMin
	}
	return nowMin >= startMin || nowMin < endMin
}
