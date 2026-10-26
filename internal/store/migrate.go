package store

import (
	"fmt"
)

type migration struct {
	version int
	name    string
	stmts   []string
}

var migrations = []migration{
	{
		version: 1,
		name:    "initial schema",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS opportunities (
				id             TEXT PRIMARY KEY,
				source         TEXT NOT NULL,
				source_id      TEXT NOT NULL,
				url            TEXT NOT NULL,
				title          TEXT NOT NULL,
				org            TEXT NOT NULL DEFAULT '',
				description    TEXT NOT NULL DEFAULT '',
				kind           TEXT NOT NULL DEFAULT 'unknown',
				status         TEXT NOT NULL DEFAULT 'unknown',
				mode           TEXT NOT NULL DEFAULT 'unknown',
				region         TEXT NOT NULL DEFAULT '',
				starts_at      TEXT,
				ends_at        TEXT,
				deadline_at    TEXT,
				prize_amount   REAL NOT NULL DEFAULT 0,
				prize_currency TEXT NOT NULL DEFAULT '',
				prize_text     TEXT NOT NULL DEFAULT '',
				team_min       INTEGER NOT NULL DEFAULT 0,
				team_max       INTEGER NOT NULL DEFAULT 0,
				tags           TEXT NOT NULL DEFAULT '[]',
				eligibility    TEXT NOT NULL DEFAULT '',
				judging        TEXT NOT NULL DEFAULT '',
				image_url      TEXT NOT NULL DEFAULT '',
				score          REAL NOT NULL DEFAULT 0,
				verdict        TEXT NOT NULL DEFAULT 'uncertain',
				reasons        TEXT NOT NULL DEFAULT '[]',
				content_hash   TEXT NOT NULL DEFAULT '',
				first_seen_at  TEXT NOT NULL,
				last_seen_at   TEXT NOT NULL,
				updated_at     TEXT NOT NULL,
				is_new         INTEGER NOT NULL DEFAULT 1,
				user_verdict   TEXT
			)`,
			`CREATE INDEX IF NOT EXISTS idx_opp_kind_deadline ON opportunities(kind, deadline_at)`,
			`CREATE INDEX IF NOT EXISTS idx_opp_verdict_score ON opportunities(verdict, score DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_opp_updated ON opportunities(updated_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_opp_source ON opportunities(source)`,

			`CREATE TABLE IF NOT EXISTS opportunity_sources (
				source         TEXT NOT NULL,
				source_id      TEXT NOT NULL,
				opportunity_id TEXT NOT NULL,
				url            TEXT NOT NULL,
				PRIMARY KEY (source, source_id)
			)`,

			`CREATE TABLE IF NOT EXISTS opportunity_tags (
				opportunity_id TEXT NOT NULL,
				tag            TEXT NOT NULL,
				PRIMARY KEY (opportunity_id, tag)
			)`,

			`CREATE TABLE IF NOT EXISTS watchlist (
				opportunity_id TEXT PRIMARY KEY,
				note           TEXT NOT NULL DEFAULT '',
				added_at       TEXT NOT NULL
			)`,

			`CREATE TABLE IF NOT EXISTS dismissals (
				opportunity_id TEXT PRIMARY KEY,
				dismissed_at   TEXT NOT NULL
			)`,

			`CREATE TABLE IF NOT EXISTS rules (
				id         TEXT PRIMARY KEY,
				expr       TEXT NOT NULL,
				action     TEXT NOT NULL,
				origin     TEXT NOT NULL DEFAULT 'db',
				enabled    INTEGER NOT NULL DEFAULT 1,
				created_at TEXT NOT NULL
			)`,

			`CREATE TABLE IF NOT EXISTS sync_runs (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				source      TEXT NOT NULL,
				started_at  TEXT NOT NULL,
				finished_at TEXT,
				status      TEXT NOT NULL,
				fetched     INTEGER NOT NULL DEFAULT 0,
				inserted    INTEGER NOT NULL DEFAULT 0,
				updated     INTEGER NOT NULL DEFAULT 0,
				skipped     INTEGER NOT NULL DEFAULT 0,
				error       TEXT NOT NULL DEFAULT '',
				duration_ms INTEGER NOT NULL DEFAULT 0
			)`,

			`CREATE TABLE IF NOT EXISTS notifications (
				id             INTEGER PRIMARY KEY AUTOINCREMENT,
				opportunity_id TEXT NOT NULL,
				channel        TEXT NOT NULL,
				kind           TEXT NOT NULL,
				sent_at        TEXT NOT NULL,
				ok             INTEGER NOT NULL DEFAULT 1,
				error          TEXT NOT NULL DEFAULT ''
			)`,

			`CREATE TABLE IF NOT EXISTS settings (
				key   TEXT PRIMARY KEY,
				value TEXT NOT NULL
			)`,
		},
	},
	{
		version: 2,
		name:    "full text search",
		stmts: []string{
			`CREATE VIRTUAL TABLE IF NOT EXISTS opportunities_fts USING fts5(
				title, org, description, tags,
				content='opportunities', content_rowid='rowid',
				tokenize='porter unicode61'
			)`,
			`CREATE TRIGGER IF NOT EXISTS opportunities_ai AFTER INSERT ON opportunities BEGIN
				INSERT INTO opportunities_fts(rowid, title, org, description, tags)
				VALUES (new.rowid, new.title, new.org, new.description, new.tags);
			END`,
			`CREATE TRIGGER IF NOT EXISTS opportunities_ad AFTER DELETE ON opportunities BEGIN
				INSERT INTO opportunities_fts(opportunities_fts, rowid, title, org, description, tags)
				VALUES ('delete', old.rowid, old.title, old.org, old.description, old.tags);
			END`,
			`CREATE TRIGGER IF NOT EXISTS opportunities_au AFTER UPDATE ON opportunities BEGIN
				INSERT INTO opportunities_fts(opportunities_fts, rowid, title, org, description, tags)
				VALUES ('delete', old.rowid, old.title, old.org, old.description, old.tags);
				INSERT INTO opportunities_fts(rowid, title, org, description, tags)
				VALUES (new.rowid, new.title, new.org, new.description, new.tags);
			END`,
		},
	},
	{
		version: 3,
		name:    "host and track axes",
		stmts: []string{
			`ALTER TABLE opportunities ADD COLUMN host TEXT NOT NULL DEFAULT 'unknown'`,
			`ALTER TABLE opportunities ADD COLUMN track TEXT NOT NULL DEFAULT 'unknown'`,
			`CREATE INDEX IF NOT EXISTS idx_opp_host_track ON opportunities(host, track)`,
		},
	},
}

// migrate applies all pending migrations.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	var current int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.version, err)
		}
		for _, stmt := range m.stmts {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, name) VALUES (?, ?)`, m.version, m.name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
	}
	return nil
}
