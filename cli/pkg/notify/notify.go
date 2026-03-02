// Package notify provides webhook notification support for YACMO.
// It can send alerts to Slack, Discord, or any generic webhook endpoint
// before and after chaos experiments run.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"
)

// Notifier sends chaos event notifications to configured webhooks.
type Notifier struct {
	cfg    config.NotifyConfig
	log    *logger.Logger
	client *http.Client
}

// New creates a new Notifier.
func New(cfg config.NotifyConfig, log *logger.Logger) *Notifier {
	return &Notifier{
		cfg: cfg,
		log: log,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Event types sent by the notifier.
const (
	EventChaosStarting  = "chaos_starting"
	EventChaosCompleted = "chaos_completed"
	EventExperimentDone = "experiment_done"
	EventChaosError     = "chaos_error"
)

// Payload is the JSON body sent to webhooks.
type Payload struct {
	Event     string    `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Details   any       `json:"details,omitempty"`
}

// slackPayload wraps the message for Slack's expected format.
type slackPayload struct {
	Text string `json:"text"`
}

// Send dispatches a notification to all configured webhooks.
func (n *Notifier) Send(ctx context.Context, event string, message string, details any) {
	if !n.cfg.Enabled {
		return
	}

	payload := Payload{
		Event:     event,
		Timestamp: time.Now(),
		Message:   message,
		Details:   details,
	}

	for _, wh := range n.cfg.Webhooks {
		go func(wh config.WebhookTarget) {
			if err := n.send(ctx, wh, payload); err != nil {
				n.log.Error("Notification to %s failed: %v", wh.Name, err)
			}
		}(wh)
	}
}

// send dispatches a single notification to one webhook.
func (n *Notifier) send(ctx context.Context, wh config.WebhookTarget, payload Payload) error {
	var body []byte
	var err error

	switch wh.Type {
	case "slack":
		// Slack expects {"text": "..."}
		icon := "🐒"
		if payload.Event == EventChaosError {
			icon = "🚨"
		}
		slackMsg := slackPayload{
			Text: fmt.Sprintf("%s *[YACMO]* `%s` — %s", icon, payload.Event, payload.Message),
		}
		body, err = json.Marshal(slackMsg)
	case "discord":
		// Discord accepts {"content": "..."}
		discordMsg := map[string]string{
			"content": fmt.Sprintf("🐒 **[YACMO]** `%s` — %s", payload.Event, payload.Message),
		}
		body, err = json.Marshal(discordMsg)
	default:
		// Generic webhook — send full payload
		body, err = json.Marshal(payload)
	}

	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add custom headers (e.g., auth tokens)
	for k, v := range wh.Headers {
		req.Header.Set(k, v)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}

	n.log.Debug("Notification sent to %s (%s): HTTP %d", wh.Name, wh.Type, resp.StatusCode)
	return nil
}
