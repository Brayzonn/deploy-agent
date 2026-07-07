package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

/*
NewSlack creates a SlackNotifier; with an empty webhook URL every
notify call warns that notifications are disabled and sends nothing.
*/
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
NotifyStart posts a deployment-started message to the configured Slack
webhook. Beyond the deploy feed it doubles as a per-deploy health check
of the alert channel: it exercises the exact env-var → HTTP → Slack
path the failure alert uses and logs the outcome, so a broken or
missing webhook is visible in every deploy's log instead of only on
the failure that needed it.
*/
func (s *SlackNotifier) NotifyStart(ctx *types.DeploymentContext) {
	message := fmt.Sprintf(
		":rocket: *Deployment started* — %s\n>Branch: `%s` | Commit: `%.7s` | Pushed by: %s\n>Deployment ID: `%s`",
		ctx.RepoName, ctx.Branch, ctx.Commit, ctx.Pusher, ctx.DeploymentID,
	)
	s.post("start", message)
}

/*
NotifyFailure posts a deployment-failure message to the configured
Slack webhook, formatted to correlate with the start message via the
deployment ID.
*/
func (s *SlackNotifier) NotifyFailure(ctx *types.DeploymentContext, deployErr error) {
	message := fmt.Sprintf(
		":rotating_light: *Deployment FAILED* — %s\n>Branch: `%s` | Commit: `%.7s` | Pushed by: %s\n>Deployment ID: `%s`\n```%v```",
		ctx.RepoName, ctx.Branch, ctx.Commit, ctx.Pusher, ctx.DeploymentID, deployErr,
	)
	s.post("failure", message)
}

/*
post sends a plain-text payload to the webhook and logs the outcome
either way — kind names the notification in the log lines. A missing
webhook URL warns loudly rather than skipping silently, so an unset
SLACK_WEBHOOK_URL shows up in every deploy log; a non-OK response is
logged with its body, since Slack puts the actual reason there
(invalid_token, no_service, ...). All problems are logged and
swallowed — a broken webhook must never change a deployment's outcome.
*/
func (s *SlackNotifier) post(kind, message string) {
	if s.webhookURL == "" {
		s.log.Warning("Slack notifications DISABLED — SLACK_WEBHOOK_URL not set")
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
		s.log.Warningf("Slack %s notification failed: %v", kind, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		s.log.Warningf("Slack %s notification failed: webhook returned %d: %s", kind, resp.StatusCode, body)
		return
	}

	s.log.Infof("Slack %s notification sent", kind)
}
