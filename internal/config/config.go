// Package config loads and validates the XGames configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration object.
type Config struct {
	DataDir string            `yaml:"data_dir"`
	DBPath  string            `yaml:"db_path"`
	Filters Filters           `yaml:"filters"`
	Sources map[string]Source `yaml:"sources"`
	Rules   []Rule            `yaml:"rules"`
	Watch   Watch             `yaml:"watch"`
	Notify  Notify            `yaml:"notify"`
	UI      UI                `yaml:"ui"`
}

// Filters holds the default filters and the saved presets.
type Filters struct {
	Default Filter            `yaml:"default"`
	Presets map[string]Filter `yaml:"presets"`
}

// Filter is a saved query.
type Filter struct {
	Query          string   `yaml:"query"`
	Kinds          []string `yaml:"kinds"`
	Sources        []string `yaml:"sources"`
	Regions        []string `yaml:"regions"`
	MinPrize       float64  `yaml:"min_prize"`
	MaxPrize       float64  `yaml:"max_prize"`
	DeadlineBefore string   `yaml:"deadline_before"`
	ExcludeJobish  bool     `yaml:"exclude_jobish"`
}

// Rule is a user defined include/exclude rule.
type Rule struct {
	ID     string `yaml:"id"`
	When   string `yaml:"when"`
	Action string `yaml:"action"`
}

// Source configures a single source.
type Source struct {
	Enabled  *bool    `yaml:"enabled"`
	Interval string   `yaml:"interval"`
	Types    []string `yaml:"types"`
	Limit    int      `yaml:"limit"`
	Pages    int      `yaml:"pages"`
}

// Watch configures the monitoring behaviour.
type Watch struct {
	Profiles     []Profile `yaml:"profiles"`
	QuietHours   string    `yaml:"quiet_hours"`
	MaxPerRun    int       `yaml:"max_per_run"`
	RemindBefore []string  `yaml:"remind_before"`
}

// Profile is a saved watch filter plus its notification channels.
type Profile struct {
	Name   string   `yaml:"name"`
	Filter Filter   `yaml:"filter"`
	Notify []string `yaml:"notify"`
}

// Notify configures the notification channels.
type Notify struct {
	Webhook Webhook `yaml:"webhook"`
	Email   Email   `yaml:"email"`
	Desktop bool    `yaml:"desktop"`
}

// Webhook is a Slack/Discord compatible webhook.
type Webhook struct {
	URL    string `yaml:"url"`
	Format string `yaml:"format"`
}

// Email is a minimal SMTP configuration.
type Email struct {
	SMTP string   `yaml:"smtp"`
	From string   `yaml:"from"`
	To   []string `yaml:"to"`
}

// UI holds the terminal preferences.
type UI struct {
	Theme    string `yaml:"theme"`
	PageSize int    `yaml:"page_size"`
	Unicode  *bool  `yaml:"unicode"`
	Color    *bool  `yaml:"color"`
	ShowAll  *bool  `yaml:"show_all"`
}

// Default returns the built-in configuration.
func Default() Config {
	showAll := false
	unicode, color := true, true
	return Config{
		DataDir: defaultDataDir(),
		Filters: Filters{
			Default: Filter{
				Kinds:          []string{"hackathon", "buildathon", "challenge", "competition", "fellowship", "residency", "internship"},
				DeadlineBefore: "60d",
				ExcludeJobish:  true,
			},
			Presets: map[string]Filter{
				"ai-india": {
					Query:          "ai OR llm OR agent OR machine learning",
					Regions:        []string{"India"},
					MinPrize:       10000,
					DeadlineBefore: "45d",
					ExcludeJobish:  true,
				},
				"all-builds": {
					Kinds: []string{"hackathon", "buildathon", "challenge", "competition"},
				},
			},
		},
		Sources: map[string]Source{},
		Watch: Watch{
			MaxPerRun:    10,
			RemindBefore: []string{"72h", "24h"},
			QuietHours:   "23:00-08:00",
		},
		UI: UI{
			Theme:    "dark",
			PageSize: 50,
			Unicode:  &unicode,
			Color:    &color,
			ShowAll:  &showAll,
		},
	}
}

func defaultDataDir() string {
	if v := os.Getenv("XG_DATA_DIR"); v != "" {
		return v
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "x-games")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "x-games")
}

// Path returns the configuration file path.
func Path() string {
	if v := os.Getenv("XG_CONFIG"); v != "" {
		return v
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "x-games", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "x-games", "config.yaml")
}

// Load reads the configuration file and applies the defaults and environment
// overrides. A missing file is not an error.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = Path()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg.withEnv(), nil
		}
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(cfg.DataDir, "xgames.db")
	}
	return cfg.withEnv(), nil
}

// withEnv applies environment variable overrides.
func (c Config) withEnv() Config {
	if v := os.Getenv("XG_DB"); v != "" {
		c.DBPath = v
	}
	if v := os.Getenv("XG_DATA_DIR"); v != "" && c.DBPath == "" {
		c.DBPath = filepath.Join(v, "xgames.db")
	}
	if c.DBPath == "" {
		c.DBPath = filepath.Join(c.DataDir, "xgames.db")
	}
	return c
}

// SourceEnabled reports whether a source should be used.
func (c Config) SourceEnabled(name string, def bool) bool {
	src, ok := c.Sources[name]
	if !ok || src.Enabled == nil {
		return def
	}
	return *src.Enabled
}

// SourceInterval returns the refresh interval for a source.
func (c Config) SourceInterval(name string, def time.Duration) time.Duration {
	src, ok := c.Sources[name]
	if !ok || src.Interval == "" {
		return def
	}
	if d, err := time.ParseDuration(src.Interval); err == nil {
		return d
	}
	return def
}

// ParseDuration parses durations like "30m", "2h", "1d".
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if strings.HasSuffix(s, "d") {
		var days float64
		if _, err := fmt.Sscanf(strings.TrimSuffix(s, "d"), "%g", &days); err == nil {
			return time.Duration(days * float64(24*time.Hour)), nil
		}
	}
	if strings.HasSuffix(s, "w") {
		var weeks float64
		if _, err := fmt.Sscanf(strings.TrimSuffix(s, "w"), "%g", &weeks); err == nil {
			return time.Duration(weeks * float64(7*24*time.Hour)), nil
		}
	}
	return time.ParseDuration(s)
}

// Write writes the configuration to disk.
func (c Config) Write(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
