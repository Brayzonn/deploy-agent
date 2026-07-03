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
	log         *logger.Logger
}

// ============================================================
// Construction
// ============================================================

/* New creates a HealthChecker for the given deployment target. */
func New(domain string, port int, appName string, projectType types.ProjectType, log *logger.Logger) *HealthChecker {
	return &HealthChecker{
		domain:      domain,
		port:        port,
		appName:     appName,
		projectType: projectType,
		log:         log,
	}
}

// ============================================================
// Health checks
// ============================================================

/*
CheckHTTP polls the deployed site over HTTP, retrying up to 5 times
with a 2s delay, until it gets a 200 or exhausts its attempts.
Client projects are probed at "/"; everything else is probed at the
hardcoded "/api/v1/health/simple" path.
*/
func (h *HealthChecker) CheckHTTP() error {
	if h.domain == "" {
		h.log.Warning("No domain configured, skipping HTTP check")
		return nil
	}

	h.log.Info("Performing HTTP health check...")

	var url string
	if h.projectType == types.ProjectTypeClient {
		url = fmt.Sprintf("http://%s/", h.domain)
	} else {
		url = fmt.Sprintf("http://%s/api/v1/health/simple", h.domain)
	}

	maxAttempts := 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := http.Get(url)
		if err != nil {
			if attempt < maxAttempts {
				h.log.Infof("HTTP check attempt %d/%d failed, retrying in 2s...", attempt, maxAttempts)
				time.Sleep(2 * time.Second)
				continue
			}
			return fmt.Errorf("HTTP check failed after %d attempts: %w", maxAttempts, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			h.log.Successf("HTTP health check passed (Status: %d)", resp.StatusCode)
			return nil
		}

		if attempt < maxAttempts {
			h.log.Infof("HTTP returned %d, retrying in 2s...", resp.StatusCode)
			time.Sleep(2 * time.Second)
			continue
		}

		return fmt.Errorf("HTTP check failed: expected 200, got %d", resp.StatusCode)
	}

	return fmt.Errorf("HTTP check failed after %d attempts", maxAttempts)
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
