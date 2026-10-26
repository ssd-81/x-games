# XGames

**Terminal radar for build-events** — hackathons, buildathons, fellowships, dev residencies and
other events you compete in by *building something*.

XGames deliberately ignores standard job listings. A "Software Engineer Intern" posting is not an
event; a "Razorpay AI Buildathon" is. Every item is classified with an explainable scoring model,
so you always know why something is (or is not) in your feed.

```
xgames ▸ ai buildathon                     42/118                 synced 12s ago
┌──────────────────────────────────────────┬──────────────────────────────────┐
│ ▸ Razorpay AI Buildathon  hackathon      │  Razorpay AI Buildathon          │
│   ₹2.0L · closes in 4d · hybrid          │  by Razorpay · India             │
│   ★new                                   │                                  │
│                                          │  deadline  2026-11-06 (4d)      │
│   O AI Build Challenge 2026  challenge   │  prize     ₹2,00,000             │
│   $ 5,000 · closes in 9d · online        │  team      2-4                   │
│                                          │  eligible  students, freshers    │
│                                          │  why: +kind:buildathon           │
│                                          │       +build-vocab:build         │
└──────────────────────────────────────────┴──────────────────────────────────┘
j/k move · / search · f filter · ⏎ open · r sync · s sort · a star · x dismiss · ? help · q quit
```

---

## Install

```bash
go install github.com/ssd-81/x-games/cmd/xg@latest
# or from a checkout
make build      # -> ./bin/xg
```

No cgo required (SQLite is the pure Go `modernc.org/sqlite` build).

## Quick start

```bash
xg sync                 # fetch the latest events from every enabled source
xg                      # open the interactive TUI
xg search "ai buildathon" --min-prize 50000
```

## The search language

One line mixes free text with filters:

```
ai buildathon kind:hackathon region:India prize:>50000 deadline:<30d
```

| Filter | Meaning | Example |
|---|---|---|
| `kind:` | event class | `kind:buildathon`, `kind:!internship` |
| `source:` | origin | `source:unstop` |
| `region:` | geography | `region:India` |
| `mode:` | online / hybrid / offline | `mode:online` |
| `tag:` | tag match | `tag:AI` |
| `org:` | organizer | `org:razorpay` |
| `prize:` | amount | `prize:>50000`, `prize:1000-50000` |
| `deadline:` | closing window | `deadline:<14d`, `deadline:>7d` |
| `-word` | exclude a term | `ai -internship` |
| `title:` / `description:` | field scoped text | `title:buildathon` |

## Commands

| Command | Purpose |
|---|---|
| `xg` | interactive TUI (list + detail + live refresh) |
| `xg search [query]` | one-shot search (`--json` for scripting) |
| `xg sync` | fetch from the sources (`--source`, `--limit`, `--types`) |
| `xg watch` | monitor in the background and notify (`--interval`, `--channel`) |
| `xg sources` | list the sources and their state |
| `xg show <id>` | print one event with its classification reasons |
| `xg config [path\|show\|init]` | configuration location and contents |

## Sources

| Source | Endpoint | Notes |
|---|---|---|
| **Devpost** | `devpost.com/api/hackathons` | the largest global hackathon list |
| **Unstop** | `unstop.com/api/public/opportunity/search-result` | Indian hackathons, buildathons, internships, competitions |
| **CTFtime** | `ctftime.org/api/v1/events/` | capture the flag events (disabled by default) |

Adding a source means implementing one interface (`domain.Source`) and registering it — the UI,
the store and the search layer never change. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Configuration

`~/.config/x-games/config.yaml` (create with `xg config init`):

```yaml
filters:
  default:
    kinds: [hackathon, buildathon, challenge, competition, fellowship]
  presets:
    ai-india:
      query: "ai OR llm OR agent"
      regions: [India]
      min_prize: 50000

sources:
  devpost: { enabled: true, interval: 30m }
  unstop:  { enabled: true, interval: 30m, types: [hackathons, competitions] }
  ctftime: { enabled: false }

watch:
  profiles:
    - name: ai-builds-india
      filter: { kinds: [hackathon, buildathon], regions: [India], min_prize: 50000 }
      notify: [desktop, terminal]
  max_per_run: 10
  remind_before: [72h, 24h]
  quiet_hours: "23:00-08:00"

notify:
  webhook: { url: "${SLACK_WEBHOOK}", format: slack }
```

Environment overrides: `XG_CONFIG`, `XG_DB`, `XG_DATA_DIR`, `XG_NO_COLOR`.

## How the job filter works

XGames scores every item instead of blocking job boards by name:

| Signal group | Effect |
|---|---|
| explicit kind from the source (`type=hackathons`) | decisive, ±0.45 |
| build vocabulary (`hackathon`, `ship`, `demo day`, `team of`) | up to +0.36 |
| event vocabulary (`prize`, `judging`, `mentorship`, `stipend`) | up to +0.20 |
| event structure (deadline, prize, team size, eligibility, judging) | up to +0.25 |
| job vocabulary (`CTC`, `full-time`, `interview process`, `apply now`) | up to −0.45 |
| job without a deadline | −0.10 |

Verdicts: `include` (≥ 0.55), `exclude` (≤ 0.15), `uncertain` in between. Uncertain items are
hidden by default but stay in the index — press `v` in the TUI to review them. Every reason is
shown in the detail pane and can be overridden by a rule:

```yaml
rules:
  - id: keep-fintech
    when: "org =~ /razorpay/ and kind == buildathon"
    action: include
```

## Development

```bash
make test        # unit + contract tests (offline)
make build       # build ./bin/xg
make fmt vet     # formatting and vet
make smoke       # live end-to-end check against the real sources
```

## License

MIT
