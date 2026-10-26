// Package httpx provides the shared HTTP client used by all source adapters:
// a polite client with per-host rate limiting, retries with backoff,
// conditional requests (ETag / Last-Modified) and a small in-memory cache.
package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Options configures the client.
type Options struct {
	UserAgent      string
	Timeout        time.Duration
	Retries        int
	MaxBodyBytes   int64
	Logger         *slog.Logger
	MinInterval    time.Duration  // default politeness gap between calls to the same host
	RequestsPerMin map[string]int // per host overrides
}

// DefaultUserAgent identifies the crawler honestly.
const DefaultUserAgent = "xgames/0.1 (+https://github.com/ssd-81/x-games)"

// Client is a polite HTTP client.
type Client struct {
	http   *http.Client
	opts   Options
	logger *slog.Logger

	mu      sync.Mutex
	cache   map[string]cacheEntry
	limiter map[string]*hostLimiter
}

type cacheEntry struct {
	etag         string
	lastModified string
	body         []byte
	fetchedAt    time.Time
}

type hostLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

// New returns a client with sensible defaults.
func New(opts Options) *Client {
	if opts.UserAgent == "" {
		opts.UserAgent = DefaultUserAgent
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 20 * time.Second
	}
	if opts.Retries <= 0 {
		opts.Retries = 3
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 12 << 20
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Client{
		http:    &http.Client{Timeout: opts.Timeout},
		opts:    opts,
		logger:  opts.Logger,
		cache:   map[string]cacheEntry{},
		limiter: map[string]*hostLimiter{},
	}
}

// Get performs a GET request honouring the rate limit of the target host.
func (c *Client) Get(ctx context.Context, url string, headers map[string]string) (*http.Response, error) {
	if err := c.wait(ctx, url); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.opts.UserAgent)
	req.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.8")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	c.mu.Lock()
	entry, hasCache := c.cache[url]
	c.mu.Unlock()
	if hasCache {
		if entry.etag != "" {
			req.Header.Set("If-None-Match", entry.etag)
		}
		if entry.lastModified != "" {
			req.Header.Set("If-Modified-Since", entry.lastModified)
		}
	}

	var lastErr error
	for attempt := 0; attempt < c.opts.Retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusNotModified && hasCache {
			resp.Body.Close()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     resp.Header,
				Body:       io.NopCloser(strings.NewReader(string(entry.body))),
			}, nil
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return resp, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("request failed")
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

// GetJSON fetches a URL and decodes the JSON body into out.
func (c *Client) GetJSON(ctx context.Context, url string, out any) error {
	resp, err := c.Get(ctx, url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.opts.MaxBodyBytes))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	c.store(url, resp, body)
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode json from %s: %w", url, err)
	}
	return nil
}

// GetBytes fetches a URL and returns the raw body.
func (c *Client) GetBytes(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	resp, err := c.Get(ctx, url, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.opts.MaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	c.store(url, resp, body)
	return body, nil
}

// GetString fetches a URL and returns the body as a string.
func (c *Client) GetString(ctx context.Context, url string) (string, error) {
	b, err := c.GetBytes(ctx, url, nil)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Client) store(url string, resp *http.Response, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[url] = cacheEntry{
		etag:         resp.Header.Get("ETag"),
		lastModified: resp.Header.Get("Last-Modified"),
		body:         body,
		fetchedAt:    time.Now(),
	}
}

// wait blocks until the host rate limit allows another request.
func (c *Client) wait(ctx context.Context, url string) error {
	host := hostOf(url)
	c.mu.Lock()
	lim, ok := c.limiter[host]
	if !ok {
		interval := c.opts.MinInterval
		if interval <= 0 {
			interval = 1 * time.Second
		}
		if rpm, ok := c.opts.RequestsPerMin[host]; ok && rpm > 0 {
			interval = time.Duration(int(time.Minute) / rpm)
		}
		lim = &hostLimiter{interval: interval}
		c.limiter[host] = lim
	}
	c.mu.Unlock()

	lim.mu.Lock()
	now := time.Now()
	if lim.next.After(now) {
		wait := lim.next.Sub(now)
		lim.next = lim.next.Add(lim.interval)
		lim.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		return nil
	}
	lim.next = now.Add(lim.interval)
	lim.mu.Unlock()
	return nil
}

func hostOf(rawURL string) string {
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}
