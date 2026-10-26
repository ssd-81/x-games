// Package notify delivers notifications about new and closing build-events.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ssd-81/x-games/internal/domain"
)

// Kind describes why a notification was sent.
type Kind string

const (
	KindNew      Kind = "new"
	KindUpdated  Kind = "updated"
	KindClosing  Kind = "closing"
	KindReminder Kind = "reminder"
)

// httpClient is shared by the webhook notifier.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// Message is a notification payload.
type Message struct {
	Kind        Kind
	Opportunity *domain.Opportunity
	Title       string
	Body        string
	URL         string
	SentAt      time.Time
}

// Notifier delivers messages to one channel.
type Notifier interface {
	// Name returns the channel identifier.
	Name() string
	// Send delivers a message.
	Send(ctx context.Context, msg Message) error
}

// Desktop sends desktop notifications through notify-send (Linux),
// osascript (macOS) or the PowerShell toast API (Windows).
type Desktop struct {
	AppName string
}

// Name returns the channel identifier.
func (d Desktop) Name() string { return "desktop" }

// Send delivers a desktop notification.
func (d Desktop) Send(ctx context.Context, msg Message) error {
	app := d.AppName
	if app == "" {
		app = "xgames"
	}
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`display notification %q with title %q`, msg.Body, msg.Title)
		return runCommand(ctx, "osascript", "-e", script)
	case "windows":
		return runCommand(ctx, "powershell", "-NoProfile", "-Command",
			fmt.Sprintf("[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier(%q).Show((New-Object Windows.UI.Notifications.ToastNotification))", app))
	default:
		if _, err := exec.LookPath("notify-send"); err != nil {
			return fmt.Errorf("notify-send not available: %w", err)
		}
		return runCommand(ctx, "notify-send", "--app-name", app, msg.Title, msg.Body)
	}
}

// Webhook posts messages to a Slack or Discord compatible webhook.
type Webhook struct {
	URL    string
	Format string // "slack" (default), "discord" or "generic"
}

// Name returns the channel identifier.
func (w Webhook) Name() string { return "webhook" }

// Send posts the message to the webhook.
func (w Webhook) Send(ctx context.Context, msg Message) error {
	if w.URL == "" {
		return fmt.Errorf("webhook url is not configured")
	}
	text := msg.Title + "\n" + msg.Body
	if msg.URL != "" {
		text += "\n" + msg.URL
	}
	var payload []byte
	switch strings.ToLower(w.Format) {
	case "discord":
		payload, _ = json.Marshal(map[string]any{"content": text})
	case "generic":
		payload, _ = json.Marshal(map[string]any{
			"kind": string(msg.Kind), "title": msg.Title, "body": msg.Body,
			"url": msg.URL, "sent_at": msg.SentAt,
		})
	default: // slack
		payload, _ = json.Marshal(map[string]any{"text": text})
	}
	req, err := newPostRequest(ctx, w.URL, payload)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// File appends the notifications to a JSONL file. It is handy for piping the
// feed into other tools.
type File struct {
	Path string
}

// Name returns the channel identifier.
func (f File) Name() string { return "file" }

// Send appends the message to the file.
func (f File) Send(ctx context.Context, msg Message) error {
	if f.Path == "" {
		return fmt.Errorf("file path is not configured")
	}
	data, err := json.Marshal(map[string]any{
		"kind": string(msg.Kind), "title": msg.Title, "body": msg.Body,
		"url": msg.URL, "sent_at": msg.SentAt,
		"opportunity": msg.Opportunity,
	})
	if err != nil {
		return err
	}
	fh, err := os.OpenFile(f.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fh.Close()
	if _, err := fh.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// Terminal writes the notification to stderr with a bell.
type Terminal struct {
	Out *os.File
}

// Name returns the channel identifier.
func (t Terminal) Name() string { return "terminal" }

// Send writes the notification to the terminal.
func (t Terminal) Send(ctx context.Context, msg Message) error {
	out := t.Out
	if out == nil {
		out = os.Stderr
	}
	_, err := fmt.Fprintf(out, "\a🔔 %s\n%s\n%s\n\n", msg.Title, msg.Body, msg.URL)
	return err
}

// Multi fans a message out to several channels.
type Multi struct {
	Notifiers []Notifier
}

// Name returns the channel identifier.
func (m Multi) Name() string { return "multi" }

// Send delivers the message to every channel, collecting the errors.
func (m Multi) Send(ctx context.Context, msg Message) error {
	var errs []string
	for _, n := range m.Notifiers {
		if err := n.Send(ctx, msg); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", n.Name(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func newPostRequest(ctx context.Context, url string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "xgames/0.1")
	return req, nil
}
