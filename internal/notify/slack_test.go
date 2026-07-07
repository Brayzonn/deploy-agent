package notify

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

// ============================================================
// Fixtures
// ============================================================

/* testLogger builds a logger writing under the test's temp dir. */
func testLogger(t *testing.T) *logger.Logger {
	t.Helper()

	log, err := logger.New("test", filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatalf("could not create logger: %v", err)
	}
	t.Cleanup(func() { log.Close() })
	return log
}

/* testContext builds a deployment context with recognizable field values. */
func testContext() *types.DeploymentContext {
	return &types.DeploymentContext{
		RepoName:     "example-repo",
		Branch:       "main",
		Commit:       "0123456789abcdef",
		Pusher:       "someone",
		DeploymentID: "deploy-42",
	}
}

/*
captureWebhook runs an httptest server that records each posted body
and returns the server plus a pointer to the received payloads.
*/
func captureWebhook(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()

	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

// ============================================================
// Notifications
// ============================================================

/*
TestNotifyStartPostsPayload checks the start message reaches the
webhook and carries the fields that make it a useful deploy-feed
entry and channel health check.
*/
func TestNotifyStartPostsPayload(t *testing.T) {
	srv, bodies := captureWebhook(t)

	NewSlack(srv.URL, testLogger(t)).NotifyStart(testContext())

	if len(*bodies) != 1 {
		t.Fatalf("expected 1 webhook post, got %d", len(*bodies))
	}
	for _, want := range []string{"Deployment started", "example-repo", "deploy-42", "0123456"} {
		if !strings.Contains((*bodies)[0], want) {
			t.Errorf("start payload missing %q: %s", want, (*bodies)[0])
		}
	}
}

/*
TestNotifyFailurePostsPayload checks the failure message carries the
deploy error and the deployment ID that correlates it with the start
message.
*/
func TestNotifyFailurePostsPayload(t *testing.T) {
	srv, bodies := captureWebhook(t)

	NewSlack(srv.URL, testLogger(t)).NotifyFailure(testContext(), errors.New("health check exploded"))

	if len(*bodies) != 1 {
		t.Fatalf("expected 1 webhook post, got %d", len(*bodies))
	}
	for _, want := range []string{"Deployment FAILED", "health check exploded", "deploy-42"} {
		if !strings.Contains((*bodies)[0], want) {
			t.Errorf("failure payload missing %q: %s", want, (*bodies)[0])
		}
	}
}

/*
TestEmptyURLSendsNothing checks an unset webhook URL never attempts a
request (the warn-and-skip path) and never panics.
*/
func TestEmptyURLSendsNothing(t *testing.T) {
	srv, bodies := captureWebhook(t)
	_ = srv // a server exists, but the notifier is not pointed at it

	n := NewSlack("", testLogger(t))
	n.NotifyStart(testContext())
	n.NotifyFailure(testContext(), errors.New("boom"))

	if len(*bodies) != 0 {
		t.Fatalf("empty URL must send nothing, got %d posts", len(*bodies))
	}
}

/*
TestNonOKResponseSwallowed checks a webhook error response (Slack's
invalid_token style) is swallowed — notification problems must never
change a deployment's outcome.
*/
func TestNonOKResponseSwallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("no_service"))
	}))
	t.Cleanup(srv.Close)

	NewSlack(srv.URL, testLogger(t)).NotifyStart(testContext())
}
