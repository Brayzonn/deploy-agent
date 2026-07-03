package health

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

// ============================================================
// Helpers
// ============================================================

/* newTestChecker returns a HealthChecker probing healthURL with the given timeout. */
func newTestChecker(t *testing.T, healthURL string, timeoutSeconds int) *HealthChecker {
	t.Helper()
	log, err := logger.New("test", filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })

	return New("", 0, "test-app", types.ProjectTypeDocker, healthURL, timeoutSeconds, log)
}

// ============================================================
// Tests
// ============================================================

/* TestCheckHTTPUsesConfiguredURL checks the configured URL is probed verbatim, not a hardcoded path. */
func TestCheckHTTPUsesConfiguredURL(t *testing.T) {
	var gotPath atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := newTestChecker(t, server.URL+"/api/v1/health", 5)
	if err := checker.CheckHTTP(); err != nil {
		t.Fatalf("check against healthy server failed: %v", err)
	}
	if got := gotPath.Load(); got != "/api/v1/health" {
		t.Fatalf("probed path %q, want the configured /api/v1/health", got)
	}
}

/* TestCheckHTTPRetriesUntilHealthy checks a server that recovers within the timeout passes. */
func TestCheckHTTPRetriesUntilHealthy(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := newTestChecker(t, server.URL, 30)
	if err := checker.CheckHTTP(); err != nil {
		t.Fatalf("check against recovering server failed: %v", err)
	}
	if calls.Load() < 3 {
		t.Fatalf("server called %d times, want at least 3 (two failures then success)", calls.Load())
	}
}

/* TestCheckHTTPFailsAfterTimeout checks a persistently broken server fails once the timeout passes. */
func TestCheckHTTPFailsAfterTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := newTestChecker(t, server.URL, 1)
	if err := checker.CheckHTTP(); err == nil {
		t.Fatal("check against broken server succeeded, want timeout failure")
	}
}
