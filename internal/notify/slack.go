package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

type SlackNotifier struct {
	webhookURL string
	log        *logger.Logger
}

// ============================================================
// Construction
// ============================================================

/* NewSlack creates a SlackNotifier; with an empty webhook URL every notify call is a no-op. */
func NewSlack(webhookURL string, log *logger.Logger) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		log:        log,
	}
}

// ============================================================
// Notifications
// ============================================================

/*
NotifyFailure posts a deployment-failure message to the configured
Slack webhook. Notification problems are logged and swallowed — a
broken webhook must never change a deployment's outcome.
*/
func (s *SlackNotifier) NotifyFailure(ctx *types.DeploymentContext, deployErr error) {
	message := fmt.Sprintf(
		":rotating_light: *Deployment FAILED* — %s\n>Branch: `%s` | Commit: `%.7s` | Pushed by: %s\n>Deployment ID: `%s`\n```%v```",
		ctx.RepoName, ctx.Branch, ctx.Commit, ctx.Pusher, ctx.DeploymentID, deployErr,
	)
	s.post(message)
}

/* post sends a plain-text payload to the webhook, if one is configured. */
func (s *SlackNotifier) post(message string) {
	if s.webhookURL == "" {
		return
	}

	payload, err := json.Marshal(map[string]string{"text": message})
	if err != nil {
		s.log.Warningf("Could not build Slack payload: %v", err)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(s.webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		s.log.Warningf("Slack notification failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.log.Warningf("Slack notification failed: webhook returned %d", resp.StatusCode)
		return
	}

	s.log.Info("Failure notification sent to Slack")
}
