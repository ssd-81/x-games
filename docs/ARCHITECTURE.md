# XGames — Architecture

**XGames** is a terminal radar for *build-events*: hackathons, buildathons, fellowships, dev
residencies, competitions and other "you are judged on what you ship" opportunities.

It explicitly **excludes standard job listings** (a "Software Engineer Intern" JD is not an event
you compete in). See [§2 Taxonomy](#2-taxonomy--the-inclusion-policy).

---

## 1. Goals / Non-Goals

### Goals
1. **One screen for build-events** — TUI with fast search, filters and a readable detail pane.
2. **Live monitoring** — periodic re-sync, diffing, and alerts when something relevant appears or is
   about to close.
3. **Explainable filtering** — every include/exclude decision is traceable to signals, never an
   opaque heuristic. The user can override it.
4. **Many sources, one shape** — each source is an adapter behind one interface; adding a source
   must not touch the UI, store, or search layer.
5. **Local-first, scriptable** — everything in SQLite; CLI surface for piping into `jq`, cron, etc.

### Non-Goals
- Not a job board. Not a link aggregator.
- Not a general-purpose crawling/scraping framework (we scrape only curated, known-shape sources).
- No hosted service, no accounts, no telemetry. (Opt-in analytics can be added later; default off.)

### Interpretation note
The request was to exclude job listings and focus on "events which equalize based on some form of
building" — read here as: **events whose outcome depends on something you build and demo**. All
filtering decisions in this document implement that reading.

---

## 2. Taxonomy & the Inclusion Policy

### 2.1 Event classes

| Class | Examples | Default |
|---|---|---|
| `hackathon` | Devpost hackathons, MLH events, college hackathons | ✅ include |
| `buildathon` | Razorpay AI Buildathon, Zoho/Freshworks-style build programs | ✅ include |
| `challenge` | HackerEarth challenges, company coding challenges | ✅ include |
| `competition` | Kaggle, Zindi, DrivenData, arena-style comps | ✅ include |
| `ctf` | CTFtime events | ✅ include |
| `fellowship` | GSoC, MOSS, Outreachy, research fellowships | ✅ include |
| `residency` | Builders/residency programs with a build component | ✅ include |
| `contest` | Codeforces contests, algorithmic rounds | ⚠️ include, category filter |
| `meetup` | Community build nights | ⚠️ include, low priority |
| `internship` (build-based) | "AI/ML intern — ship a live project", research internships | ✅ include **if** build-signal passes |
| `job` | Full-time / internship postings with a JD, salary, interview process | ❌ exclude |
| `conference` | Conference with no build track | ❌ exclude (unless it has a hackathon side-track) |

### 2.2 The classifier — "build-signal scoring"

Rather than maintaining a blocklist of job boards (whack-a-mole), the app uses a **positive
classifier**. Every item gets a score in `[0,1]`, a verdict and human-readable reasons.

Signals are weighted, grouped, and each contributes a bounded amount:

| Group | Signal examples | Weight |
|---|---|---|
| **Source kind hint** | Adapter declares `kind` explicitly (Unstop `type=hackathons` vs `type=jobs`) | +0.45 (decisive) |
| **Source trust** | curated build-event source (devpost, unstop, mlh …) | +0.09 (trust × 0.10) |
| **Build vocabulary** | `hackathon`, `buildathon`, `build a …`, `demo day`, `prototype`, `ship`, `team of N`, `48-hour`, `hack` | +0.06 each, capped +0.36 |
| **Event structure** | has deadline **and** prize/team-size/eligibility/judging fields | +0.05 each, capped +0.25 |
| **Event vocabulary** | `prize`, `winner`, `judging criteria`, `leaderboard`, `certificate`, `mentorship`, `stipend`, `open source` | +0.03 each, capped +0.20 |
| **Job vocabulary** | `salary`, `CTC`, `LPA`, `per annum`, `full-time`, `part-time`, `interview process`, `resume/CV`, `JD`, `immediate joiner`, `work from office`, `apply now`, `recruiter`, `candidate` | −0.06 each, capped −0.45 |
| **Job without deadline** | a job-shaped item that also has no deadline | −0.10 |

**Verdicts**
- `score ≥ include_threshold` (default `0.55`) → `include`
- `score ≤ exclude_threshold` (default `0.15`) → `exclude`

The verdict is decided on the **raw** score (which may be negative); the score that is displayed
and stored is clamped to `[0,1]`. This keeps a clearly job-shaped item distinguishable from one
that simply carries no signals yet.
- otherwise → `uncertain` → **kept in the store, hidden by default in the TUI**, surfaced in a
  "Review" view where the user can flip the verdict (one keystroke) — and that flip is remembered
  as a *rule* (§2.3).

**Explainability**: every signal that fired is stored as `verdict_reasons` (e.g. `"+source:kind=hackathon"`,
`"-vocab:salary"`) and rendered in the detail pane. A user must always be able to answer
"why is this here / why is this hidden?".

### 2.3 User rules

Rules are first-class, stored in SQLite (`xg rules add|list|rm`) and configurable in
`~/.config/x-games/config.yaml`:

```yaml
rules:
  - id: exclude-sales-roles
    when: "title =~ /sales|growth|business development/i"
    action: exclude
  - id: allow-ai-buildathon
    when: "description =~ /buildathon/i and kind == buildathon"
    action: include
```

**Shipped expression language** (implemented in `internal/classify/rules.go`):

| Syntax | Meaning |
|---|---|
| `field value` comparisons | `title`, `org`, `description`, `eligibility`, `kind`, `source`, `region`, `status`, `mode`, `tags`, `url`, `prize` |
| `==`, `!=` | string comparison (case-insensitive) |
| `=~ /regex/i` | regular expression (case-insensitive by default, `i`/`m`/`s` flags supported) |
| `contains "text"` | substring match |
| `>`, `>=`, `<`, `<=` | numeric comparison (e.g. `prize >= 50000`) |
| `and`, `or`, `not`, `( )` | boolean composition |
| bare words | compared as literals (`kind == hackathon`), unknown identifiers are literals |

Rule evaluation happens **after** scoring and **overrides** it. Database rules (created
interactively) take precedence over configuration rules.

### 2.4 Why this works for the stated goal

A job posting almost never has a deadline, a prize, a team size and judging criteria — it has a
salary band and an interview process. The classifier therefore separates the two classes on
*structure*, not on wording alone, and the wording signals act as tie-breakers. New job boards are
excluded by default because an unknown source with no `kind` hint starts at score 0 and needs
positive build-signal mass to cross the threshold.

---

## 3. System Overview

```
┌──────────────────────────────────────────────────────────────────────────┐
│  PRESENTATION                                                             │
│  ┌────────────────┐ ┌─────────────┐ ┌──────────────┐ ┌────────────────┐  │
│  │ TUI (bubbletea)│ │ CLI (search)│ │ CLI (sync)   │ │ CLI (watch)   │  │
│  │ list/detail/   │ │ --json      │ │ one-shot     │ │ headless      │  │
│  │ filter/watch   │ │ pipeable    │ │              │ │ monitor       │  │
│  └───────┬────────┘ └──────┬──────┘ └──────┬───────┘ └───────┬────────┘  │
└──────────┼─────────────────┼───────────────┼─────────────────┼────────────┘
           │                 │               │                 │
┌──────────▼─────────────────▼───────────────▼─────────────────▼────────────┐
│  APPLICATION CORE  (use cases, no I/O details)                            │
│  SyncEngine · SearchService · ClassifyService · WatchService ·            │
│  DiffService · NotifyService · RuleEngine                                 │
└───────┬───────────────┬───────────────────┬──────────────────┬─────────────┘
        │               │                   │                  │
┌───────▼──────┐ ┌──────▼───────┐  ┌────────▼────────┐  ┌──────▼──────────┐
│  PORTS       │ │  Classifier  │  │  Search index   │  │  Notifier      │
│  (interfaces)│ │  rules+score │  │  (FTS5 + SQL)   │  │  channels      │
│              │ │              │  │                 │  │                │
│  Source      │ │              │  │                 │  │                │
│  Store       │ │              │  │                 │  │                │
│  Clock       │ │              │  │                 │  │                │
│  Logger      │ │              │  │                 │  │                │
└───────┬──────┘ └──────────────┘  └─────────────────┘  └──────┬──────────┘
        │
┌───────▼───────────────────────────────────────────────────────────────────┐
│  ADAPTERS (infrastructure)                                               │
│  Sources: devpost · unstop · ctftime · github · mlh · devfolio · rss ·   │
│           html-generic                                                   │
│  Store:   SQLite (modernc.org/sqlite, pure Go) + migrations + FTS5       │
│  HTTP:    client w/ ETag cache, retry/backoff, per-host rate limit       │
│  Notify:  desktop(notify-send) · webhook(slack/discord) · email · file   │
└──────────────────────────────────────────────────────────────────────────┘
```

**Layering rule**: dependencies point inward only. The core never imports an adapter; adapters are
registered into the core via `init()`-free explicit registration in `cmd/xg` (or a registry file
that imports both sides) — Go's lack of DI makes explicit wiring the cleanest option.

---

## 4. Module Map

```
x-games/
├── cmd/xg/                     # single binary entrypoint, wiring, subcommands
│   ├── main.go
│   ├── root.go                 # cobra-less flag dispatch (stdlib flag + subcommands)
│   ├── tui_cmd.go
│   ├── search_cmd.go
│   ├── sync_cmd.go
│   ├── watch_cmd.go
│   ├── sources_cmd.go
│   └── rules_cmd.go
├── internal/
│   ├── domain/                 # entities + value objects, zero deps
│   │   ├── opportunity.go      # Opportunity, Kind, Status, Mode, Money, Interval
│   │   ├── taxonomy.go         # class constants + taxonomy helpers
│   │   ├── source.go           # Source descriptor, FetchRequest, FetchResult
│   │   └── errors.go
│   ├── classify/               # scoring engine (pure, unit-testable)
│   │   ├── signals.go          # signal definitions + weights
│   │   ├── classifier.go       # score(), verdict
│   │   ├── lexicon.go          # vocabulary tables (regex lists)
│   │   └── rules.go            # user rule engine (expr-ish predicates)
│   ├── store/                  # SQLite persistence
│   │   ├── sqlite.go           # open, pragmas, connection
│   │   ├── migrate.go          # embedded SQL migrations
│   │   ├── opportunity.go      # CRUD + upsert + dedupe
│   │   ├── search.go           # FTS5 queries, filters, ranking
│   │   ├── watchlist.go        # stars / dismissals / verdicts
│   │   └── rules.go            # user rules persistence
│   ├── sources/                # source adapters (one dir per source)
│   │   ├── registry.go
│   │   ├── devpost.go
│   │   ├── unstop.go
│   │   ├── ctftime.go
│   │   ├── github.go
│   │   ├── mlh.go
│   │   ├── rss.go              # generic RSS/Atom
│   │   └── html_generic.go     # CSS-selector-driven scraper
│   ├── httpx/                  # HTTP client: cache, retry, ratelimit, UA, robots
│   ├── app/                    # use cases
│   │   ├── sync.go             # SyncEngine
│   │   ├── search.go           # SearchService
│   │   ├── watch.go            # WatchService + scheduler
│   │   ├── diff.go             # new/updated/closed detection
│   │   └── notify.go           # NotifyService
│   ├── notify/                 # notifier implementations
│   ├── tui/                    # bubbletea models & views
│   │   ├── app.go              # root model
│   │   ├── list.go             # results pane
│   │   ├── detail.go           # detail pane
│   │   ├── filter.go           # filter modal
│   │   ├── watch.go            # watch-mode model
│   │   ├── help.go
│   │   ├── keymap.go
│   │   └── theme.go            # lipgloss styles
│   ├── config/                 # load/validate/defaults
│   ├── clock/                  # Clock interface + real/fake
│   └── logging/                # leveled logger (slog wrapper)
├── docs/ARCHITECTURE.md
├── testdata/fixtures/<source>/ # recorded JSON/HTML for offline tests
└── Makefile
```

---

## 5. Domain Model

```go
type Opportunity struct {
    ID          string   // stable: sha1(source + "|" + sourceID) → 12 hex
    Source      string   // "devpost", "unstop", ...
    SourceID    string   // id in the source system
    URL         string   // canonicalized (utm stripped, no trailing slash)
    Title       string
    Org         string   // organizer / company
    Description string   // plain text (HTML stripped)
    Kind        Kind     // hackathon | buildathon | challenge | ... | job
    Status      Status   // upcoming | open | ongoing | ended | unknown
    Mode        Mode     // online | hybrid | offline | unknown
    Region      string   // "India", "Global", ...
    StartsAt    *time.Time
    EndsAt      *time.Time
    DeadlineAt  *time.Time   // registration / submission deadline
    Prize       Money
    TeamMin     int
    TeamMax     int
    Tags        []string
    Eligibility string
    Judging     string
    ImageURL    string

    // classification
    Score      float64
    Verdict    Verdict  // include | exclude | uncertain
    Reasons    []string
    ContentHash string  // for change detection

    FirstSeenAt time.Time
    LastSeenAt  time.Time
    UpdatedAt   time.Time
}
```

**Stable ID** = `sha1(source|sourceID)[:12]` — survives title changes. Fallback: sha1 of
canonical URL when the source has no ID.

**Content hash** = sha1 over the *semantic* fields (title, org, deadline, prize, team sizes,
description-normalized). Any change → item is "updated" (watch mode can alert on it).

**Dedup across sources**: the same hackathon often appears on Devpost + Unstop + Twitter. Dedup is
two-stage:
1. Exact: same canonical URL → merge, keep both source rows (`opportunity_sources` join table).
2. Fuzzy: normalized title + org + deadline-within-48h → "possible duplicate" flag shown in the UI
   (manual merge in Phase 5; auto-merge deferred because false positives are costly).

---

## 6. Data Model (SQLite)

```sql
CREATE TABLE opportunities (
  id            TEXT PRIMARY KEY,
  source        TEXT NOT NULL,
  source_id     TEXT NOT NULL,
  url           TEXT NOT NULL,
  title         TEXT NOT NULL,
  org           TEXT,
  description   TEXT,
  kind          TEXT NOT NULL,
  status        TEXT NOT NULL,
  mode          TEXT,
  region        TEXT,
  starts_at     TEXT,
  ends_at       TEXT,
  deadline_at   TEXT,
  prize_amount  REAL,
  prize_currency TEXT,
  prize_text    TEXT,
  team_min      INTEGER,
  team_max      INTEGER,
  tags          TEXT,          -- JSON array
  eligibility   TEXT,
  judging       TEXT,
  image_url     TEXT,
  score         REAL NOT NULL DEFAULT 0,
  verdict       TEXT NOT NULL DEFAULT 'uncertain',
  reasons       TEXT,          -- JSON array
  content_hash  TEXT NOT NULL,
  first_seen_at TEXT NOT NULL,
  last_seen_at  TEXT NOT NULL,
  updated_at    TEXT NOT NULL,
  is_new        INTEGER NOT NULL DEFAULT 1,
  user_verdict  TEXT           -- NULL | include | exclude  (user override)
);
CREATE INDEX idx_opp_kind_deadline ON opportunities(kind, deadline_at);
CREATE INDEX idx_opp_verdict_score  ON opportunities(verdict, score);
CREATE INDEX idx_opp_updated        ON opportunities(updated_at);

CREATE TABLE opportunity_sources (      -- many-to-many after dedup
  opportunity_id TEXT NOT NULL,
  source         TEXT NOT NULL,
  source_id      TEXT NOT NULL,
  url            TEXT NOT NULL,
  PRIMARY KEY (source, source_id)
);

CREATE TABLE opportunity_tags (
  opportunity_id TEXT NOT NULL,
  tag            TEXT NOT NULL,
  PRIMARY KEY (opportunity_id, tag)
);

-- FTS5 mirror, contentless-external
CREATE VIRTUAL TABLE opportunities_fts USING fts5(
  title, org, description, tags,
  content='opportunities', content_rowid='rowid', tokenize='porter unicode61'
);
-- triggers keep fts in sync on insert/update/delete

CREATE TABLE watchlist (opportunity_id TEXT PRIMARY KEY, note TEXT, added_at TEXT);
CREATE TABLE dismissals (opportunity_id TEXT PRIMARY KEY, dismissed_at TEXT);
CREATE TABLE rules (
  id TEXT PRIMARY KEY, expr TEXT NOT NULL, action TEXT NOT NULL,
  origin TEXT NOT NULL, created_at TEXT, enabled INTEGER DEFAULT 1
);
CREATE TABLE sync_runs (
  id INTEGER PRIMARY KEY, source TEXT, started_at TEXT, finished_at TEXT,
  status TEXT, fetched INTEGER, inserted INTEGER, updated INTEGER,
  skipped INTEGER, error TEXT, duration_ms INTEGER
);
CREATE TABLE notifications (id INTEGER PRIMARY KEY, opportunity_id TEXT, channel TEXT,
  kind TEXT, sent_at TEXT, ok INTEGER, error TEXT);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT);
```

Search uses FTS5 with `bm25()` ranking plus a recency/urgency boost applied in the query layer.

---

## 7. Source Adapter Contract

```go
// internal/domain/source.go
type Source interface {
    Name() string
    // Describe returns static metadata used by the UI and the classifier.
    Describe() SourceMeta
    // Fetch returns opportunities for the given request.
    Fetch(ctx context.Context, req FetchRequest) ([]Opportunity, error)
}

type SourceMeta struct {
    Name        string
    Homepage    string
    Trust       float64          // 0..1 default source trust
    DefaultKind Kind             // hint for the classifier (may be KindUnknown)
    Modes       []Mode
    RateLimit   RateLimit        // requests/min, min interval between calls
    Regions     []string
    Enabled     bool
    Docs        string
}

type FetchRequest struct {
    Since    time.Time   // incremental fetch if the source supports it
    Limit    int
    Page     int
    Cursor   string
    Query    string      // optional server-side filter
}
```

### 7.1 Verified source endpoints (probed during design)

| Source | Endpoint | Shape | Notes |
|---|---|---|---|
| **Devpost** | `GET https://devpost.com/api/hackathons?page=N&status[]=upcoming\|open` | JSON: `hackathons[]` with `title,url,organization_name,displayed_location.location,open_state,themes[].name,submission_period_dates,prize_amount,thumbnail_url` | No auth. Prize is HTML-ish string → parse to `Money`. `themes` → tags. |
| **Unstop** | `GET https://unstop.com/api/public/opportunity/search-result?opportunity=hackathons\|internships\|competitions&oppstatus=open&per_page=N&page=N` | JSON: `data.data[]` with `id,title,seo_url,organisation.name,region,status,end_date,prizes[],regnRequirements{min_team_size,max_team_size,eligibility,end_regn_dt},details(HTML),filters[],required_skills[],type,subtype` | **Key signal**: `type` = `hackathons` vs `jobs`; `subtype` = `internships`. `details` is HTML → strip to text. `prizes[].cash/currency` → money. |
| **CTFtime** | `GET https://ctftime.org/api/v1/events/?limit=N` | JSON array | Requires a browser-like `User-Agent` (403 otherwise). |
| **Codeforces** | `GET https://codeforces.com/api/contest.list` | JSON | `phase`, `startTimeSeconds`, `durationSeconds`. Class `contest`. |
| **GitHub** | `GET https://api.github.com/search/repositories?q=topic:hackathon` | JSON | For "awesome-list"/community listings; optional, off by default (noise). |
| **MLH** | `https://mlh.io/events/` | HTML/JS | Client-rendered; adapter will target its backing JSON endpoint (discovered at implementation time) — fallback: skip. |
| **Devfolio** | `https://devfolio.co/hackathons` (`__NEXT_DATA__`) | embedded JSON | Fragile; parse `__NEXT_DATA__` with fallback to `api.devfolio.co`. |
| **RSS/Atom** | any feed URL | XML | Generic escape hatch for org-specific feeds (e.g. a company's hackathon page that has an RSS). |
| **HTML generic** | any URL + CSS selectors | HTML | Escape hatch for pages without APIs. |

**Politeness & robustness**
- One shared `httpx.Client` with: per-host token bucket, exponential backoff with jitter (3 tries),
  `User-Agent: xgames/0.1 (+url)`, conditional requests (`ETag`/`If-Modified-Since` → 304 = no-op),
  and a hard timeout (20s). Robots.txt is respected for the HTML-generic adapter.
- Adapters must never panic on malformed payloads; a bad record is skipped and counted
  (`sync_runs.skipped`).
- Every adapter has **fixture-based tests** that never touch the network.

### 7.2 Adapter: Unstop (worked example)

```
GET /api/public/opportunity/search-result
    ?opportunity=hackathons      # 'hackathons' | 'competitions' | 'internships' | 'jobs'
    &oppstatus=open
    &per_page=20&page=1
    &sort=latest
```
Mapping:
- `title` → Title; `organisation.name` → Org; `seo_url` → URL.
- `type`/`subtype` → Kind hint: `hackathons` → `hackathon`; `competitions` → `competition`;
  `jobs`+`subtype=internships` → `internship` (then the classifier decides build vs job).
- `prizes[]` → sum `cash` per `currency` → `Money`; `others` → prize text.
- `regnRequirements.end_regn_dt` → DeadlineAt; `start_regn_dt` → StartsAt.
- `regnRequirements.min_team_size`/`max_team_size` → TeamMin/TeamMax.
- `regnRequirements.eligibility` (JSON string) → Eligibility (summarized).
- `details` (HTML) → Description (tags stripped, whitespace collapsed).
- `region` → Mode/Region; `status` → Status; `filters[]`/`required_skills[]` → Tags.

---

## 8. Search & Filtering

### 8.1 Query pipeline

```
user query ──▶ tokenize ──▶ FTS5 MATCH (prefix-matched, quoted phrases)
                     └─▶ filters (SQL WHERE) ──▶ sort ──▶ rank blend ──▶ page
```

- **Free-text + filters in one line** (implemented in `internal/app/query.go`):
  `ai buildathon kind:hackathon region:India prize:>50000 deadline:<30d -internship`.
  Supported filter fields: `kind`, `source`, `region`, `mode`, `status`, `tag`, `org`, `title`,
  `description`, `prize`, `deadline`. A `!` prefix negates a value (`kind:!internship`), `-term`
  excludes a free-text term, `prize:1000-50000` is a range.
- **Filters** (all combinable, persisted in config so they can be reused):
  `kind`, `source`, `status`, `mode`, `region`, `verdict`, `tags`, `prize_min`, `prize_max`,
  `deadline_before`, `deadline_after`, `opened_within`, `org`, `stipend_only`, `team_size`,
  `eligible_for` (student/fresher/professional).
- **Sorting**: `relevance` (default), `deadline`, `prize`, `newest`, `score`.
- **Ranking blend**: `final = 0.6*bm25_norm + 0.25*urgency + 0.15*recency` where
  `urgency = exp(-days_to_deadline / 7)` (closing soon ranks higher).

### 8.2 Deal with FTS5 + filters

FTS5 MATCH on `opportunities_fts` joined to `opportunities` by rowid; filters applied in the outer
query. The FTS table is kept in sync by triggers on `opportunities`. For Phase 1 we can also query
the base table with `LIKE` if FTS migration is delayed, but FTS5 is in the schema from the start.

---

## 9. Classification & Learning Loop

1. **Signal extraction** — pure functions over title/description/kind/structure.
2. **Scoring** — weighted sum, clamped to `[0,1]`, with per-group caps (documented in `signals.go`).
3. **Rules** — user rules evaluated last; `include`/`exclude` override the score.
4. **Feedback** — every user override is stored as a labelled sample. A future Phase 5 command
   (`xg rules tune`) can suggest weight adjustments; default behaviour stays deterministic and
   explainable (no ML in the hot path — the app must never be a black box to the user).

Deterministic, no network, unit-testable: `classify_test.go` with ~40 golden cases including
"Software Engineer Intern — apply now, CTC 12 LPA" (exclude) and "Razorpay AI Buildathon — build
an agent, ₹2L prize pool" (include).

---

## 10. Watch / Monitor

```
       ┌──────────┐  tick per source (cron-ish interval from config)
       │ Watcher  │──────────────────────────────┐
       └────┬─────┘                              ▼
            │                          ┌────────────────────┐
            │                          │  Source.Fetch()    │
            │                          └─────────┬──────────┘
            │                                    ▼
            │                          ┌────────────────────┐
            │                          │ classify + upsert  │
            │                          └─────────┬──────────┘
            │                                    ▼
            │                          ┌────────────────────┐
            │                          │  DiffService       │
            │                          │ new / updated /    │
            │                          │ deadline-soon      │
            │                          └─────────┬──────────┘
            │                                    ▼
            │                          ┌────────────────────┐
            └──────────────────────────│  NotifyService     │
                                       └────────────────────┘
```

- **Diff states**: `new` (first seen), `updated` (content hash changed), `closing_soon`
  (deadline within `remind_before`), `closed` (deadline passed / status ended).
- **Notification channels** (`notifier` interface, fan-out):
  - `desktop` → `notify-send` (Linux) / `osascript` (macOS) / toast (Windows).
  - `terminal` → bell + TUI toast.
  - `webhook` → Slack/Discord (JSON payload), generic HTTP.
  - `email` → SMTP via `net/smtp` (minimal MIME).
  - `file` → JSONL append (great for piping to other tooling).
- **Debounce**: one notification per (opportunity, channel, kind) — recorded in `notifications`.
- **Quiet hours** and **max-per-run** caps in config.
- **Watch filters**: a watch profile is just a saved search (`kind=hackathon prize_min>=50000
  region=India deadline_before=14d`) — one concept serves both the TUI and the watcher.

---

## 11. TUI UX Specification

### 11.1 Layout

```
┌────────────────────────────────────────────────────────────────────────────┐
│ xgames ▸ search: ai buildathon          [12/48]   sync: 2m ago  ● live    │  header
├──────────────────────────────────────────┬─────────────────────────────────┤
│ ● Razorpay AI Buildathon      hackathon  │  RAZORPAY AI BUILDATHON          │
│   ₹2,00,000 · closes in 4d · hybrid     │  by Razorpay · India             │
│   [devpost][unstop]  new                 │  ────────────────────────────   │
│                                          │  Build an agent... (details)    │
│   ○ HackWithInfy 2026          challenge │  ────────────────────────────   │
│   ...                                      │  Deadline   2026-11-06 00:00   │
│                                          │  Prize      ₹2,00,000           │
│                                          │  Team       2–4                 │
│                                          │  Eligible   students, freshers   │
│                                          │  ────────────────────────────   │
│                                          │  Why included:                  │
│                                          │   + source:kind=hackathon       │
│                                          │   + vocab:buildathon            │
│                                          │   + structure:deadline,prize     │
├──────────────────────────────────────────┴─────────────────────────────────┤
│ / search  ⏎ open  f filter  w watch  s sort  ★ star  x dismiss  ? help  q  │  footer
└────────────────────────────────────────────────────────────────────────────┘
```

### 11.2 Screens / modes

| Mode | Enter | Purpose |
|---|---|---|
| `browse` | default | results list + detail pane |
| `search` | `/` | inline query editing (incremental, debounced 150ms) |
| `filter` | `f` | modal filter builder (kind/source/deadline/prize/region) |
| `detail` | `⏎` | full-screen detail (scrollable, `j/k`, `g/G`) |
| `watch` | `w` | live monitor: incoming events stream, filter/edit watch profiles |
| `review` | `r` | uncertain items, one-key include/exclude (writes rules) |
| `help` | `?` | keymap + cheatsheet |

### 11.3 Keymap (default)

```
navigate    ↑/↓, j/k, PgUp/PgDn, g/G, ←/→ (pane switch)
search      /, Esc, Ctrl+U (clear), Ctrl+W (delete word)
actions     ⏎ open, o open in browser, c copy URL, y copy details
filter      f (modal), x clear filters, s cycle sort
watch       w start/stop watch, a add to watchlist, r refresh/sync now, R force resync
review      v toggle include/exclude, u undo
misc        ? help, q quit, Ctrl+C quit
```

### 11.4 Implementation notes

- `bubbletea` v1.3 + `bubbles` (list, viewport, textinput, spinner) + `lipgloss` v1 for styling.
- **Async discipline**: all network work happens in `tea.Cmd`s (goroutines) and returns typed
  messages; the model never blocks. `tea.Program` runs the store in a single writer goroutine to
  avoid `database is locked`.
- **Virtualized list**: `bubbles/list` renders only the visible window (handles 10k+ items).
- **Terminal capability detection**: 256-color/truecolor, unicode vs ASCII fallback for box
  drawing, and a `--ascii` flag.
- **No-color / no-unicode flags** for CI logs and dumb terminals.
- **Snapshot tests**: golden files of rendered output at fixed widths (80×24, 120×40) to catch
  layout regressions without a real terminal.

---

## 12. CLI Surface

```
xg                       # launch TUI (default command)
xg search [QUERY]        # one-shot search → table/JSON
xg sync                  # refresh sources into the store
xg watch                 # headless monitor (for cron/systemd)
xg show <id>             # print one opportunity with its reasons
xg sources               # list the sources and their state
xg rules list|add|rm     # manage the include/exclude rules
xg config path|init|show # configuration location and contents
```

Flags: `--kind`, `--source`, `--region`, `--min-prize`, `--max-prize`, `--deadline-before`,
`--deadline-after`, `--verdict`, `--all`, `--json`, `--limit`, `--sort`, `--db`, `--config`,
`--verbose`. List flags are repeatable (`--source a --source b`) and may appear before or after the
query (the CLI re-parses interspersed flags, unlike the stdlib default).

Exit codes: `0` ok, `1` runtime error, `2` usage / no results, `3` config error.

---

## 13. Configuration

`~/.config/x-games/config.yaml` (override with `XG_CONFIG`):

```yaml
data_dir: ~/.local/share/x-games
db_path: ~/.local/share/x-games/xgames.db

filters:
  default:
    kinds: [hackathon, buildathon, challenge, competition, fellowship]
    deadline_before: 30d
    exclude_jobish: true
  presets:
    ai-india:
      query: "ai OR llm OR agent"
      region: India
      min_prize: 50000

sources:
  devpost:  { enabled: true,  interval: 30m }
  unstop:   { enabled: true,  interval: 30m, types: [hackathons, competitions] }
  ctftime:  { enabled: false, interval: 2h }
  codeforces: { enabled: false }
  mlh:      { enabled: true,  interval: 6h }
  rss:      { enabled: true,  interval: 1h, feeds: [ ... ] }

watch:
  profiles:
    - name: ai-builds-india
      filter: { kinds: [hackathon, buildathon], region: India, min_prize: 50000 }
      notify: [desktop, webhook]
      remind_before: [72h, 24h]
  quiet_hours: "23:00-08:00"
  max_per_run: 10

notify:
  webhook: { url: "${SLACK_WEBHOOK}", format: slack }
  email:   { smtp: "smtp://user:pass@host:587", from: ..., to: [...] }

ui:
  theme: dark
  page_size: 50
  unicode: true
  color: true
```

Env overrides: `XG_CONFIG`, `XG_DB`, `XG_DATA_DIR`, `XG_NO_COLOR`, `XG_HTTP_PROXY`.

---

## 14. Error Handling, Observability, Performance

- **Errors**: typed errors in `domain` (`ErrNotFound`, `ErrSourceUnavailable`, `ErrRateLimited`).
  Sources fail soft: one source failing never aborts a sync run; the run is recorded as partial.
- **Logging**: `log/slog` with levels; file + stderr; `xg sync --verbose` dumps per-source timings.
- **Metrics (optional, phase 5)**: per-source fetch latency, items/second, classifier verdict
  histogram, notification counts → local Prometheus textfile, no network.
- **Performance targets**: TUI renders at 60fps with 10k items; a full sync of 5 sources completes in
  < 30s; incremental FTS queries < 20ms.
- **Concurrency**: bounded worker pool (1 request/host, max 4 hosts in parallel); SQLite in WAL mode
  with `busy_timeout=5000`.

---

## 15. Testing Strategy

| Level | What | How |
|---|---|---|
| Unit | classifier signals, rule engine, money/date parsing, URL canonicalization | table-driven, golden |
| Unit | store CRUD, FTS search, filters | temp-file SQLite |
| Contract | every source adapter vs. recorded fixtures (`testdata/fixtures/<source>/*.json`) | offline, deterministic |
| Integration | sync → classify → store → search | temp DB + fake source |
| Golden | TUI rendering at fixed width | snapshot files |
| Manual | live-source smoke test (`make smoke`) | opt-in, network required |

Coverage target: ≥ 80% on `domain`, `classify`, `store`; adapters covered by fixtures.

---

## 16. Build Phases

| Phase | Deliverable | Exit criteria |
|---|---|---|
| **P0 Skeleton** | repo layout, config, domain model, taxonomy, classifier + 40 golden tests | `go test ./...` green |
| **P1 Store** | SQLite schema+migrations, upsert/dedupe, FTS5 search + filters | CLI `xg search` works over seeded fixtures |
| **P2 Sources** | httpx client (cache/retry/ratelimit), Devpost + Unstop adapters, SyncEngine + diff | `xg sync` populates DB from live network |
| **P3 TUI** | list + detail + search + filters + sort + keymap | usable browser; golden snapshots |
| **P4 Watch** | watch service, profiles, notifications (desktop/webhook/file), deadline reminders | `xg watch` alerts on a new item |
| **P5 Hardening** | more adapters (CTFtime, MLH, Devfolio, RSS), rules editor in TUI, review mode, docs, install script | 5+ live sources, installable binary |

### Current status

| Phase | State |
|---|---|
| P0 Skeleton | **done** — domain, taxonomy, classifier (golden tests), config, logging |
| P1 Store | **done** — SQLite schema + migrations, upsert/dedupe, FTS5 search, filters, rules, watchlist |
| P2 Sources | **done** — HTTP client (cache/retry/rate limit), Devpost + Unstop + CTFtime adapters, sync engine, diffing |
| P3 TUI | **done** — list + detail + search + filter + sort + watchlist + live refresh + help |
| P4 Watch | **done** — watch service, profiles, notifications (terminal/desktop/webhook/file), deadline reminders |
| P5 Hardening | **partial** — fixtures + contract tests, README, Makefile; still to do: MLH/Devfolio/RSS adapters, rules editor in the TUI, review queue UI, install script |

Milestone PRs keep each phase independently shippable.

---

## 17. Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Source HTML/API changes break adapters | contract tests against fixtures + `--source x --test` health check; adapter failures are isolated, never fatal |
| Rate limiting / IP bans | per-host rate limit, backoff, conditional requests, source `interval` config, UA honesty |
| Classifier mislabels | explainable reasons, user overrides persisted as rules, "uncertain" review queue, feedback samples for tuning |
| Too much noise | default filters + presets + per-profile watch filters; scoring prefers curated sources |
| Terminal rendering issues | ascii/no-color flags, golden snapshot tests, minimum-width guard |
| SQLite write contention | WAL, single writer goroutine, `busy_timeout`, background sync off the UI path |
| "Is this a job or an event?" ambiguity | explicit source `kind` hints + structural signals; ambiguous items go to review, never silently dropped |

---

## 18. Open Questions (defaults chosen, easy to change)

1. **Regions**: default profile = Global + India? → **Global, with region filter chips in the TUI**.
2. **Prize currency**: normalize to INR + USD for sorting? → **store raw + `Money.Currency`, sort by numeric
   with a currency-aware threshold in config** (no FX rates offline).
3. **CTFs/contests**: keep visible by default? → **off by default** (category filter), on in presets.
4. **Internships**: include build-based internships? → **yes, but only when build-signal passes** (score ≥ 0.55).
5. **Paywall/heavily-JS sources**: MLH/Devfolio → parse embedded JSON (`__NEXT_DATA__`) when possible,
   otherwise mark unsupported and skip.
