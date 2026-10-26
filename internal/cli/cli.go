// Package cli implements the command line interface of XGames.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/ssd-81/x-games/internal/app"
	"github.com/ssd-81/x-games/internal/classify"
	"github.com/ssd-81/x-games/internal/config"
	"github.com/ssd-81/x-games/internal/domain"
	"github.com/ssd-81/x-games/internal/httpx"
	"github.com/ssd-81/x-games/internal/logging"
	"github.com/ssd-81/x-games/internal/notify"
	"github.com/ssd-81/x-games/internal/sources"
	"github.com/ssd-81/x-games/internal/store"
	"github.com/ssd-81/x-games/internal/tui"
)

// Run dispatches a command and returns the process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		return runTUI(nil)
	}
	switch args[0] {
	case "search":
		return runSearch(args[1:])
	case "sync":
		return runSync(args[1:])
	case "sources":
		return runSources(args[1:])
	case "watch":
		return runWatch(args[1:])
	case "show":
		return runShow(args[1:])
	case "tui":
		return runTUI(args[1:])
	case "config":
		return runConfig(args[1:])
	case "rules":
		return runRules(args[1:])
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		// Unknown command: treat it as a search query.
		return runSearch(args)
	}
}

func usage() {
	fmt.Println(`xgames - terminal radar for build-events

Usage:
  xg                     open the interactive TUI
  xg search [query]      search the local index
  xg sync                fetch new events from the sources
  xg sources             list the configured sources
  xg watch               monitor the sources and notify about new events
  xg show <id>           print one opportunity
  xg config [path|show]  show the configuration location
  xg rules list|add|rm   manage the include/exclude rules

Examples:
  xg search "ai buildathon" --kind hackathon --min-prize 50000
  xg sync --source devpost --source unstop
  xg search --json | jq '.[] | {title, deadline_at}'`)
}

// stringList is a repeatable string flag: --source a --source b.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*s = append(*s, p)
		}
	}
	return nil
}

// parseInterspersed parses flags that may appear before or after positional
// arguments. The standard library stops parsing at the first positional
// argument, which would make "xg search buildathon --limit 5" fail.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// --- shared helpers ---

type commonFlags struct {
	configPath string
	dbPath     string
	verbose    bool
}

func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.configPath, "config", "", "path to the configuration file")
	fs.StringVar(&c.dbPath, "db", "", "path to the database file")
	fs.BoolVar(&c.verbose, "verbose", false, "verbose output")
}

func (c *commonFlags) loadConfig() (config.Config, error) {
	cfg, err := config.Load(c.configPath)
	if err != nil {
		return cfg, err
	}
	if c.dbPath != "" {
		cfg.DBPath = c.dbPath
	}
	return cfg, nil
}

// --- search ---

func runSearch(args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	var (
		kindList   = fs.String("kind", "", "comma separated kinds (hackathon,buildathon,...)")
		sourceList = fs.String("source", "", "comma separated sources")
		regionList = fs.String("region", "", "comma separated regions")
		minPrize   = fs.Float64("min-prize", 0, "minimum prize amount")
		maxPrize   = fs.Float64("max-prize", 0, "maximum prize amount")
		before     = fs.String("deadline-before", "", "only events closing before this duration (e.g. 30d)")
		after      = fs.String("deadline-after", "", "only events closing after this duration")
		verdict    = fs.String("verdict", "include", "verdict filter: include, exclude, uncertain or any")
		sortBy     = fs.String("sort", "relevance", "sort order: relevance, deadline, prize, newest, score")
		limit      = fs.Int("limit", 30, "maximum number of results")
		all        = fs.Bool("all", false, "include excluded and uncertain items")
		asJSON     = fs.Bool("json", false, "print results as JSON")
	)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	query := strings.Join(positional, " ")

	cfg, err := common.loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 3
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer st.Close()

	f := store.Filter{
		Query:    query,
		Kinds:    splitList(*kindList),
		Sources:  splitList(*sourceList),
		Regions:  splitList(*regionList),
		MinPrize: *minPrize,
		MaxPrize: *maxPrize,
		Verdict:  *verdict,
		Sort:     *sortBy,
		Limit:    *limit,
	}
	if *all {
		f.Verdict = ""
	}
	if *before != "" {
		d, err := config.ParseDuration(*before)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: invalid --deadline-before:", err)
			return 2
		}
		t := time.Now().Add(d)
		f.DeadlineBefore = &t
	}
	if *after != "" {
		d, err := config.ParseDuration(*after)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: invalid --deadline-after:", err)
			return 2
		}
		t := time.Now().Add(d)
		f.DeadlineAfter = &t
	}

	res, err := st.Search(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		items := res.Items
		if items == nil {
			items = []*domain.Opportunity{}
		}
		if err := enc.Encode(items); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		return 0
	}
	if len(res.Items) == 0 {
		fmt.Println("no matching events. try 'xg sync' to fetch the latest listings.")
		return 2
	}
	printTable(res.Items)
	fmt.Printf("\n%d of %d matches\n", len(res.Items), res.Total)
	return 0
}

func printTable(items []*domain.Opportunity) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TITLE\tKIND\tPRIZE\tDEADLINE\tSOURCE")
	for _, o := range items {
		deadline := "-"
		if o.DeadlineAt != nil {
			deadline = o.DeadlineAt.Format("2006-01-02")
		}
		prize := o.Prize.String()
		if prize == "" {
			prize = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			truncate(o.Title, 48), o.Kind, truncate(prize, 14), deadline, o.Source)
	}
	w.Flush()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- sync ---

func runSync(args []string) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	var (
		sourceList stringList
		types      stringList
	)
	fs.Var(&sourceList, "source", "sources (default: all enabled)")
	fs.Var(&types, "types", "opportunity types (source specific)")
	limit := fs.Int("limit", 50, "items per page")
	pages := fs.Int("pages", 1, "pages per source")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := common.loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 3
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer st.Close()

	client := httpx.New(httpx.Options{Logger: logger(common.verbose)})
	registry := sources.Default(client)

	var selected []app.Source
	for _, s := range registry.All() {
		meta := s.Describe()
		if !cfg.SourceEnabled(meta.Name, meta.Enabled) {
			continue
		}
		if len(sourceList) > 0 && !contains(sourceList, meta.Name) {
			continue
		}
		selected = append(selected, s)
	}
	if len(selected) == 0 {
		fmt.Println("no sources enabled")
		return 2
	}

	cl := app.ClassifierWithRules(classify.New(classify.DefaultConfig()),
		app.LoadRules(st, cfg, logger(common.verbose)))
	engine := app.NewSyncEngine(st, cl, logger(common.verbose))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("syncing %d sources...\n", len(selected))
	results, err := engine.Sync(ctx, selected, app.SyncOptions{
		Limit: *limit,
		Pages: *pages,
		Types: types,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	for _, r := range results {
		status := "ok"
		if r.Error != "" {
			status = "error: " + r.Error
		}
		fmt.Printf("  %-10s fetched %-4d new %-4d updated %-4d include %-4d exclude %-4d uncertain %-4d %s\n",
			r.Source, r.Fetched, r.Inserted, r.Updated, r.Included, r.Excluded, r.Uncertain, status)
	}
	fmt.Println(app.SyncResultSummary(results))
	return 0
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// --- sources ---

func runSources(args []string) int {
	fs := flag.NewFlagSet("sources", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := common.loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 3
	}
	client := httpx.New(httpx.Options{})
	registry := sources.Default(client)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tENABLED\tKIND\tINTERVAL\tNOTE")
	for _, s := range registry.All() {
		meta := s.Describe()
		enabled := cfg.SourceEnabled(meta.Name, meta.Enabled)
		fmt.Fprintf(w, "%s\t%v\t%s\t%s\t%s\n", meta.Name, enabled, meta.DefaultKind,
			meta.Interval, meta.Note)
	}
	w.Flush()
	return 0
}

// --- show ---

func runShow(args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	asJSON := fs.Bool("json", false, "print as JSON")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "usage: xg show <id>")
		return 2
	}
	cfg, err := common.loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 3
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer st.Close()

	o, err := st.Get(positional[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return jsonErr(enc.Encode(o))
	}
	fmt.Printf("%s\n", o.Title)
	fmt.Printf("  by        %s\n", o.Org)
	fmt.Printf("  kind      %s (%s)\n", o.Kind, o.Status)
	fmt.Printf("  mode      %s  region %s\n", o.Mode, o.Region)
	if o.DeadlineAt != nil {
		fmt.Printf("  deadline  %s (%s)\n", o.DeadlineAt.Format(time.RFC3339), domain.FormatDeadline(o.DeadlineAt, time.Now()))
	}
	if !o.Prize.IsZero() {
		fmt.Printf("  prize     %s\n", o.Prize.String())
	}
	if o.TeamMin > 0 || o.TeamMax > 0 {
		fmt.Printf("  team      %d-%d\n", o.TeamMin, o.TeamMax)
	}
	if o.Eligibility != "" {
		fmt.Printf("  eligible  %s\n", o.Eligibility)
	}
	if len(o.Tags) > 0 {
		fmt.Printf("  tags      %s\n", strings.Join(o.Tags, ", "))
	}
	fmt.Printf("  source    %s %s\n", o.Source, o.URL)
	fmt.Printf("  score     %.2f (%s)\n", o.Score, o.Verdict)
	if len(o.Reasons) > 0 {
		fmt.Printf("  why       %s\n", strings.Join(o.Reasons, ", "))
	}
	if o.Description != "" {
		fmt.Printf("\n%s\n", truncate(o.Description, 1200))
	}
	return 0
}

func jsonErr(err error) int {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// --- config ---

func runConfig(args []string) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path := config.Path()
	if common.configPath != "" {
		path = common.configPath
	}
	switch {
	case len(fs.Args()) == 0 || fs.Arg(0) == "path":
		fmt.Println(path)
		return 0
	case fs.Arg(0) == "show":
		cfg, err := config.Load(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 3
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return 1
		}
		fmt.Println(string(data))
		return 0
	case fs.Arg(0) == "init":
		cfg := config.Default()
		if _, err := os.Stat(path); err == nil {
			fmt.Println("config already exists:", path)
			return 0
		}
		if err := cfg.Write(path); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println("wrote", path)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown config command:", fs.Arg(0))
		return 2
	}
}

// --- tui ---

func runTUI(args []string) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	query := fs.String("query", "", "initial search query")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := common.loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 3
	}
	if err := tui.Run(tui.Options{
		ConfigPath: common.configPath,
		DBPath:     cfg.DBPath,
		Query:      *query,
		Verbose:    common.verbose,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func logger(verbose bool) *slog.Logger {
	return logging.New(verbose)
}

// --- watch ---

func runWatch(args []string) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	var profileNames, channels stringList
	fs.Var(&profileNames, "profile", "watch profile to use (repeatable)")
	fs.Var(&channels, "channel", "notification channel: terminal, desktop, webhook, file")
	interval := fs.Duration("interval", 15*time.Minute, "sync interval")
	once := fs.Bool("once", false, "run a single cycle and exit")
	limit := fs.Int("limit", 50, "items per source per cycle")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := common.loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 3
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer st.Close()

	log := logger(common.verbose)
	client := httpx.New(httpx.Options{Logger: log})
	registry := sources.Default(client)

	var selected []app.Source
	for _, s := range registry.All() {
		meta := s.Describe()
		if !cfg.SourceEnabled(meta.Name, meta.Enabled) {
			continue
		}
		selected = append(selected, s)
	}

	profiles := buildProfiles(cfg, profileNames)
	if len(profiles) == 0 {
		profiles = []app.Profile{{
			Name:   "default",
			Filter: defaultProfileFilter(cfg),
		}}
	}

	notifier, err := buildNotifier(cfg, channels)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 3
	}

	cl := app.ClassifierWithRules(classify.New(classify.DefaultConfig()),
		app.LoadRules(st, cfg, log))
	engine := app.NewSyncEngine(st, cl, log)
	watcher := app.NewWatchService(st, engine, notifier, log)
	watcher.MaxPerRun = cfg.Watch.MaxPerRun
	watcher.QuietHours = cfg.Watch.QuietHours
	for _, r := range cfg.Watch.RemindBefore {
		if d, err := config.ParseDuration(r); err == nil {
			watcher.RemindBefore = append(watcher.RemindBefore, d)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runCycle := func() {
		report, err := watcher.RunOnce(ctx, selected, app.SyncOptions{Limit: *limit}, profiles)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return
		}
		fmt.Printf("[%s] synced %d · new %d · updated %d · closing %d · notified %d\n",
			time.Now().Format("15:04:05"), report.Synced, len(report.New), len(report.Updated),
			len(report.ClosingSoon), report.Notified)
		for _, e := range report.Errors {
			fmt.Fprintln(os.Stderr, "  warning:", e)
		}
	}

	runCycle()
	if *once {
		return 0
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	fmt.Printf("watching every %s (ctrl+c to stop)\n", *interval)
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
			runCycle()
		}
	}
}

// buildProfiles converts the configuration profiles into watch profiles.
func buildProfiles(cfg config.Config, names []string) []app.Profile {
	var out []app.Profile
	for _, p := range cfg.Watch.Profiles {
		if len(names) > 0 && !contains(names, p.Name) {
			continue
		}
		pq := app.ParseQuery(p.Filter.Query)
		f := pq.Filter(50, "relevance")
		f.Kinds = append(f.Kinds, p.Filter.Kinds...)
		if len(p.Filter.Regions) > 0 {
			f.Regions = append(f.Regions, p.Filter.Regions...)
		}
		if len(p.Filter.Sources) > 0 {
			f.Sources = append(f.Sources, p.Filter.Sources...)
		}
		if p.Filter.MinPrize > 0 {
			f.MinPrize = p.Filter.MinPrize
		}
		if p.Filter.MaxPrize > 0 {
			f.MaxPrize = p.Filter.MaxPrize
		}
		if p.Filter.DeadlineBefore != "" {
			if d, err := config.ParseDuration(p.Filter.DeadlineBefore); err == nil {
				t := time.Now().Add(d)
				f.DeadlineBefore = &t
			}
		}
		out = append(out, app.Profile{Name: p.Name, Filter: f})
	}
	return out
}

// defaultProfileFilter builds the fallback filter from the configuration.
func defaultProfileFilter(cfg config.Config) store.Filter {
	pq := app.ParseQuery(cfg.Filters.Default.Query)
	f := pq.Filter(50, "relevance")
	f.Kinds = append(f.Kinds, cfg.Filters.Default.Kinds...)
	if len(cfg.Filters.Default.Regions) > 0 {
		f.Regions = append(f.Regions, cfg.Filters.Default.Regions...)
	}
	if cfg.Filters.Default.MinPrize > 0 {
		f.MinPrize = cfg.Filters.Default.MinPrize
	}
	if cfg.Filters.Default.DeadlineBefore != "" {
		if d, err := config.ParseDuration(cfg.Filters.Default.DeadlineBefore); err == nil {
			t := time.Now().Add(d)
			f.DeadlineBefore = &t
		}
	}
	return f
}

// buildNotifier assembles the notification channels.
func buildNotifier(cfg config.Config, channels []string) (notify.Notifier, error) {
	if len(channels) == 0 {
		channels = []string{"terminal"}
	}
	var notifiers []notify.Notifier
	for _, c := range channels {
		switch strings.ToLower(c) {
		case "terminal", "console":
			notifiers = append(notifiers, notify.Terminal{})
		case "desktop":
			notifiers = append(notifiers, notify.Desktop{})
		case "webhook":
			if cfg.Notify.Webhook.URL == "" {
				return nil, fmt.Errorf("no webhook url configured (set notify.webhook.url)")
			}
			format := cfg.Notify.Webhook.Format
			if format == "" {
				format = "slack"
			}
			notifiers = append(notifiers, notify.Webhook{URL: cfg.Notify.Webhook.URL, Format: format})
		case "file":
			path := filepath.Join(cfg.DataDir, "notifications.jsonl")
			notifiers = append(notifiers, notify.File{Path: path})
		default:
			return nil, fmt.Errorf("unknown channel %q", c)
		}
	}
	return notify.Multi{Notifiers: notifiers}, nil
}

// --- rules ---

func runRules(args []string) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		return runRulesList(args[1:])
	case "add":
		return runRulesAdd(args[1:])
	case "rm", "remove", "delete":
		return runRulesRemove(args[1:])
	default:
		fmt.Fprintln(os.Stderr, "usage: xg rules list|add|rm")
		return 2
	}
}

func runRulesList(args []string) int {
	fs := flag.NewFlagSet("rules list", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := common.loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 3
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer st.Close()

	rules := app.LoadRules(st, cfg, logger(false))
	if len(rules) == 0 {
		fmt.Println("no rules defined")
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tACTION\tORIGIN\tENABLED\tEXPRESSION")
	for _, r := range rules {
		fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%s\n", r.ID, r.Action, r.Origin, r.Enabled, r.Expr)
	}
	w.Flush()
	return 0
}

func runRulesAdd(args []string) int {
	fs := flag.NewFlagSet("rules add", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	action := fs.String("action", "exclude", "include or exclude")
	id := fs.String("id", "", "rule id (generated when empty)")
	expr := fs.String("when", "", "the rule expression")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	when := *expr
	if when == "" && len(positional) > 0 {
		when = positional[0]
	}
	if err := app.ValidateRule(when, *action); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	// Fail fast when the expression does not compile.
	probe := classify.Rule{ID: "x", Expr: when}
	if _, evalErr := probe.Evaluate(&domain.Opportunity{}); evalErr != nil {
		fmt.Fprintln(os.Stderr, "error:", evalErr)
		return 2
	}
	cfg, err := common.loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 3
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer st.Close()

	ruleID := *id
	if ruleID == "" {
		ruleID = "rule-" + fmt.Sprint(time.Now().UnixNano()%100000)
	}
	rule := store.Rule{
		ID:      ruleID,
		Expr:    when,
		Action:  domain.Verdict(*action),
		Origin:  "db",
		Enabled: true,
	}
	if err := st.AddRule(rule); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Printf("added rule %s (%s)\n", ruleID, *action)
	return 0
}

func runRulesRemove(args []string) int {
	fs := flag.NewFlagSet("rules rm", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: xg rules rm <id>")
		return 2
	}
	cfg, err := common.loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 3
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer st.Close()
	if err := st.DeleteRule(fs.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Println("removed rule", fs.Arg(0))
	return 0
}
