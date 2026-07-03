package health

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/pm2"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

type HealthChecker struct {
	domain      string
	port        int
	appName     string
	projectType types.ProjectType
	healthURL   string
	timeout     time.Duration
	log         *logger.Logger
}

// ============================================================
// Construction
// ============================================================

/*
New creates a HealthChecker for the given deployment target. When
healthURL is set it is probed verbatim; otherwise a URL is derived
from the domain. timeoutSeconds bounds the whole HTTP check,
defaulting to 30 when unset.
*/
func New(domain string, port int, appName string, projectType types.ProjectType, healthURL string, timeoutSeconds int, log *logger.Logger) *HealthChecker {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}

	return &HealthChecker{
		domain:      domain,
		port:        port,
		appName:     appName,
		projectType: projectType,
		healthURL:   healthURL,
		timeout:     time.Duration(timeoutSeconds) * time.Second,
		log:         log,
	}
}

// ============================================================
// Health checks
// ============================================================

/*
CheckHTTP polls the health URL until it returns 200 or the
configured timeout passes. The configured HealthCheckURL is used
verbatim when set; otherwise client projects are probed at "/" and
everything else at "/api/v1/health/simple" on the domain. Each
request has its own 10s limit so a hung server can't stall the
check forever.
*/
func (h *HealthChecker) CheckHTTP() error {
	url := h.healthURL
	if url == "" {
		if h.domain == "" {
			h.log.Warning("No health check URL or domain configured, skipping HTTP check")
			return nil
		}
		if h.projectType == types.ProjectTypeClient {
			url = fmt.Sprintf("http://%s/", h.domain)
		} else {
			url = fmt.Sprintf("http://%s/api/v1/health/simple", h.domain)
		}
	}

	h.log.Infof("Performing HTTP health check against %s (timeout %s)...", url, h.timeout)

	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(h.timeout)

	var lastErr error
	for attempt := 1; ; attempt++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				h.log.Successf("HTTP health check passed (Status: %d)", resp.StatusCode)
				return nil
			}
			lastErr = fmt.Errorf("expected 200, got %d", resp.StatusCode)
		} else {
			lastErr = err
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("HTTP check failed after %s: %w", h.timeout, lastErr)
		}

		h.log.Infof("HTTP check attempt %d failed (%v), retrying in 2s...", attempt, lastErr)
		time.Sleep(2 * time.Second)
	}
}

/*
CheckPM2 checks whether the PM2 app is online. No-ops for client
projects and for projects with no configured app name.
*/
func (h *HealthChecker) CheckPM2() error {
	if h.projectType == types.ProjectTypeClient {
		return nil
	}

	if h.appName == "" {
		h.log.Warning("No app name configured, skipping PM2 check")
		return nil
	}

	h.log.Info("Performing PM2 health check...")

	pm2Mgr := pm2.New(h.appName, "", h.log)

	status, err := pm2Mgr.GetStatus()
	if err != nil {
		return fmt.Errorf("PM2 check failed: %w", err)
	}

	if status != "online" {
		return fmt.Errorf("PM2 app is %s, expected online", status)
	}

	h.log.Success("PM2 health check passed (Status: online)")
	return nil
}

/*
Check runs the health checks appropriate to the project type: none
for client deployments, HTTP-only for Docker, and PM2 plus HTTP for
everything else.
*/
func (h *HealthChecker) Check() error {
	switch h.projectType {
	case types.ProjectTypeClient:
		h.log.Info("Client deployment complete (no health checks required)")
		return nil

	case types.ProjectTypeDocker:
		if err := h.CheckHTTP(); err != nil {
			return err
		}

	default: // server
		if err := h.CheckPM2(); err != nil {
			return err
		}
		if err := h.CheckHTTP(); err != nil {
			return err
		}
	}

	h.log.Success("All health checks passed!")
	return nil
}
