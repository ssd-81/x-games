package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
)

// UpsertResult reports what happened to an opportunity during an upsert.
type UpsertResult struct {
	Inserted  bool
	Updated   bool
	Unchanged bool
}

// Upsert inserts or updates an opportunity. The stable id is derived from the
// source and the source id, so a renamed event keeps its identity.
func (s *Store) Upsert(o *domain.Opportunity) (UpsertResult, error) {
	var res UpsertResult
	if o.ID == "" {
		o.ID = o.ComputeID()
	}
	if o.ContentHash == "" {
		o.ContentHash = o.ComputeContentHash()
	}
	now := time.Now().UTC()

	var existingHash string
	var firstSeen sql.NullString
	err := s.db.QueryRow(`SELECT content_hash, first_seen_at FROM opportunities WHERE id = ?`, o.ID).
		Scan(&existingHash, &firstSeen)
	switch {
	case err == sql.ErrNoRows:
		res.Inserted = true
		o.FirstSeenAt = now
	case err != nil:
		return res, fmt.Errorf("lookup opportunity %s: %w", o.ID, err)
	default:
		if existingHash != o.ContentHash {
			res.Updated = true
		} else {
			res.Unchanged = true
		}
		o.FirstSeenAt = parseTime(firstSeen)
	}
	o.LastSeenAt = now
	o.UpdatedAt = now

	tags, err := json.Marshal(o.Tags)
	if err != nil {
		return res, fmt.Errorf("encode tags: %w", err)
	}
	reasons, err := json.Marshal(o.Reasons)
	if err != nil {
		return res, fmt.Errorf("encode reasons: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return res, fmt.Errorf("begin upsert: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO opportunities (
			id, source, source_id, url, title, org, description, kind, status, mode, region,
			starts_at, ends_at, deadline_at, prize_amount, prize_currency, prize_text,
			team_min, team_max, tags, eligibility, judging, image_url,
			score, verdict, reasons, content_hash, first_seen_at, last_seen_at, updated_at, is_new
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)
		ON CONFLICT(id) DO UPDATE SET
			url=excluded.url, title=excluded.title, org=excluded.org, description=excluded.description,
			kind=excluded.kind, status=excluded.status, mode=excluded.mode, region=excluded.region,
			starts_at=excluded.starts_at, ends_at=excluded.ends_at, deadline_at=excluded.deadline_at,
			prize_amount=excluded.prize_amount, prize_currency=excluded.prize_currency, prize_text=excluded.prize_text,
			team_min=excluded.team_min, team_max=excluded.team_max, tags=excluded.tags,
			eligibility=excluded.eligibility, judging=excluded.judging, image_url=excluded.image_url,
			score=excluded.score, verdict=excluded.verdict, reasons=excluded.reasons,
			content_hash=excluded.content_hash, last_seen_at=excluded.last_seen_at,
			updated_at=excluded.updated_at, is_new=0
	`,
		o.ID, o.Source, o.SourceID, o.URL, o.Title, o.Org, o.Description, string(o.Kind), string(o.Status),
		string(o.Mode), o.Region, fmtTime(o.StartsAt), fmtTime(o.EndsAt), fmtTime(o.DeadlineAt),
		o.Prize.Amount, o.Prize.Currency, o.Prize.Text, o.TeamMin, o.TeamMax, string(tags),
		o.Eligibility, o.Judging, o.ImageURL, o.Score, string(o.Verdict), string(reasons),
		o.ContentHash, fmtTimeVal(o.FirstSeenAt), fmtTimeVal(o.LastSeenAt), fmtTimeVal(o.UpdatedAt),
	)
	if err != nil {
		return res, fmt.Errorf("upsert opportunity %s: %w", o.ID, err)
	}

	// Record the (source, source_id) mapping for cross source deduplication.
	if _, err := tx.Exec(`
		INSERT INTO opportunity_sources(source, source_id, opportunity_id, url)
		VALUES (?,?,?,?)
		ON CONFLICT(source, source_id) DO UPDATE SET opportunity_id=excluded.opportunity_id, url=excluded.url
	`, o.Source, o.SourceID, o.ID, o.URL); err != nil {
		return res, fmt.Errorf("record source mapping: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM opportunity_tags WHERE opportunity_id = ?`, o.ID); err != nil {
		return res, fmt.Errorf("clear tags: %w", err)
	}
	for _, t := range o.Tags {
		if t = strings.TrimSpace(t); t == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO opportunity_tags(opportunity_id, tag) VALUES (?,?)`, o.ID, t); err != nil {
			return res, fmt.Errorf("insert tag: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("commit upsert: %w", err)
	}
	return res, nil
}

// Get returns a single opportunity by id.
func (s *Store) Get(id string) (*domain.Opportunity, error) {
	rows, err := s.db.Query(`SELECT `+oppColumns+` FROM opportunities WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, domain.ErrNotFound
	}
	return scanOpportunity(rows)
}

// List returns the most recently seen opportunities.
func (s *Store) List(limit int) ([]*domain.Opportunity, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT * FROM opportunities ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Opportunity
	for rows.Next() {
		o, err := scanOpportunity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Count returns the number of stored opportunities.
func (s *Store) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM opportunities`).Scan(&n)
	return n, err
}

// SetUserVerdict stores a manual include/exclude decision.
func (s *Store) SetUserVerdict(id string, verdict domain.Verdict) error {
	_, err := s.db.Exec(`UPDATE opportunities SET user_verdict = ? WHERE id = ?`, string(verdict), id)
	return err
}

// Dismiss hides an opportunity from the default view.
func (s *Store) Dismiss(id string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO dismissals(opportunity_id, dismissed_at) VALUES (?, ?)`,
		id, fmtTimeVal(time.Now().UTC()))
	return err
}

// UnDismiss removes a dismissal.
func (s *Store) UnDismiss(id string) error {
	_, err := s.db.Exec(`DELETE FROM dismissals WHERE opportunity_id = ?`, id)
	return err
}

// DismissedIDs returns the set of dismissed opportunity ids.
func (s *Store) DismissedIDs() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT opportunity_id FROM dismissals`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// Star adds an opportunity to the watchlist.
func (s *Store) Star(id, note string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO watchlist(opportunity_id, note, added_at) VALUES (?,?,?)`,
		id, note, fmtTimeVal(time.Now().UTC()))
	return err
}

// Unstar removes an opportunity from the watchlist.
func (s *Store) Unstar(id string) error {
	_, err := s.db.Exec(`DELETE FROM watchlist WHERE opportunity_id = ?`, id)
	return err
}

// StarredIDs returns the watchlisted opportunity ids.
func (s *Store) StarredIDs() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT opportunity_id FROM watchlist`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// RecordSyncRun stores the outcome of a sync run.
func (s *Store) RecordSyncRun(run SyncRun) error {
	_, err := s.db.Exec(`
		INSERT INTO sync_runs(source, started_at, finished_at, status, fetched, inserted, updated, skipped, error, duration_ms)
		VALUES (?,?,?,?,?,?,?,?,?,?)
	`, run.Source, fmtTimeVal(run.StartedAt), fmtTimeVal(run.FinishedAt), run.Status,
		run.Fetched, run.Inserted, run.Updated, run.Skipped, run.Error, run.DurationMS)
	return err
}

// SyncRun is the record of one source fetch.
type SyncRun struct {
	Source     string
	StartedAt  time.Time
	FinishedAt time.Time
	Status     string
	Fetched    int
	Inserted   int
	Updated    int
	Skipped    int
	Error      string
	DurationMS int64
}

// RecentSyncRuns returns the most recent sync runs.
func (s *Store) RecentSyncRuns(limit int) ([]SyncRun, error) {
	rows, err := s.db.Query(`SELECT source, started_at, finished_at, status, fetched, inserted, updated, skipped, error, duration_ms
		FROM sync_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncRun
	for rows.Next() {
		var r SyncRun
		var started, finished sql.NullString
		if err := rows.Scan(&r.Source, &started, &finished, &r.Status, &r.Fetched, &r.Inserted,
			&r.Updated, &r.Skipped, &r.Error, &r.DurationMS); err != nil {
			return nil, err
		}
		r.StartedAt = parseTime(started)
		r.FinishedAt = parseTime(finished)
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- helpers ---

const oppColumns = `id, source, source_id, url, title, org, description, kind, status, mode, region,
	starts_at, ends_at, deadline_at, prize_amount, prize_currency, prize_text,
	team_min, team_max, tags, eligibility, judging, image_url,
	score, verdict, reasons, content_hash, first_seen_at, last_seen_at, updated_at, user_verdict, is_new`

type scanner interface {
	Scan(dest ...any) error
}

func scanOpportunity(row scanner) (*domain.Opportunity, error) {
	var (
		o                           domain.Opportunity
		kind, status, mode, verdict string
		tagsJSON, reasonsJSON       string
		starts, ends, deadline      sql.NullString
		firstSeen, lastSeen, upd    sql.NullString
		userVerdict                 sql.NullString
		isNew                       int
	)
	err := row.Scan(&o.ID, &o.Source, &o.SourceID, &o.URL, &o.Title, &o.Org, &o.Description,
		&kind, &status, &mode, &o.Region, &starts, &ends, &deadline,
		&o.Prize.Amount, &o.Prize.Currency, &o.Prize.Text,
		&o.TeamMin, &o.TeamMax, &tagsJSON, &o.Eligibility, &o.Judging, &o.ImageURL,
		&o.Score, &verdict, &reasonsJSON, &o.ContentHash, &firstSeen, &lastSeen, &upd, &userVerdict, &isNew)
	if err != nil {
		return nil, err
	}
	o.IsNew = isNew != 0
	o.Kind = domain.Kind(kind)
	o.Status = domain.Status(status)
	o.Mode = domain.Mode(mode)
	o.Verdict = domain.Verdict(verdict)
	o.StartsAt = parseTimePtr(starts)
	o.EndsAt = parseTimePtr(ends)
	o.DeadlineAt = parseTimePtr(deadline)
	o.FirstSeenAt = parseTime(firstSeen)
	o.LastSeenAt = parseTime(lastSeen)
	o.UpdatedAt = parseTime(upd)
	_ = json.Unmarshal([]byte(tagsJSON), &o.Tags)
	_ = json.Unmarshal([]byte(reasonsJSON), &o.Reasons)
	return &o, nil
}

func fmtTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return fmtTimeVal(*t)
}

func fmtTimeVal(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s sql.NullString) time.Time {
	if !s.Valid || s.String == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseTimePtr(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return nil
	}
	return &t
}

// NotificationSent reports whether a notification was already sent for the
// given opportunity, channel and kind.
func (s *Store) NotificationSent(opportunityID, channel, kind string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE opportunity_id = ? AND channel = ? AND kind = ?`,
		opportunityID, channel, kind).Scan(&n)
	return n > 0, err
}

// RecordNotification stores the outcome of a notification.
func (s *Store) RecordNotification(opportunityID, channel, kind string, ok bool, errMsg string) error {
	_, err := s.db.Exec(`INSERT INTO notifications(opportunity_id, channel, kind, sent_at, ok, error) VALUES (?,?,?,?,?,?)`,
		opportunityID, channel, kind, fmtTimeVal(time.Now().UTC()), ok, errMsg)
	return err
}

// MarkSeen clears the "new" flag of an opportunity.
func (s *Store) MarkSeen(id string) error {
	_, err := s.db.Exec(`UPDATE opportunities SET is_new = 0 WHERE id = ?`, id)
	return err
}
