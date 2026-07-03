package ssl

import (
	"fmt"
	"strings"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/run"
)

const certbotTimeout = 5 * time.Minute

type SSLManager struct {
	domain        string
	domainAliases []string
	email         string
	log           *logger.Logger
}

// ============================================================
// Construction
// ============================================================

/* New creates an SSLManager for the given domain and its aliases. */
func New(domain string, domainAliases []string, email string, log *logger.Logger) *SSLManager {
	return &SSLManager{
		domain:        domain,
		domainAliases: domainAliases,
		email:         email,
		log:           log,
	}
}

// ============================================================
// Certificate management
// ============================================================

/*
RequestCertificate runs certbot for the domain and its aliases. A
Let's Encrypt rate-limit response and an "already valid" response
are both treated as success rather than failure.
*/
func (s *SSLManager) RequestCertificate() error {
	s.log.Info("Setting up SSL certificate...")

	domains := []string{"-d", s.domain}
	for _, alias := range s.domainAliases {
		domains = append(domains, "-d", alias)
	}

	args := []string{
		"certbot",
		"--nginx",
		"--non-interactive",
		"--agree-tos",
		"--keep-until-expiring",
	}

	if s.email != "" {
		args = append(args, "--email", s.email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}

	args = append(args, domains...)

	output, err := run.Cmd(certbotTimeout, "", "sudo", args...)
	if err != nil {
		if strings.Contains(output, "too many certificates") {
			s.log.Warning("Let's Encrypt rate limit reached")
			return nil
		}
		return fmt.Errorf("certbot failed: %w\nOutput: %s", err, output)
	}

	if strings.Contains(output, "Certificate not yet due for renewal") {
		s.log.Info("SSL certificate already exists and is valid")
	} else {
		s.log.Success("SSL certificate obtained successfully")
	}

	s.log.Infof("Certificate location: /etc/letsencrypt/live/%s/", s.domain)
	return nil
}

/* RenewCertificate runs certbot renew for the domain. */
func (s *SSLManager) RenewCertificate() error {
	s.log.Info("Renewing SSL certificate...")

	output, err := run.Cmd(certbotTimeout, "", "sudo", "certbot", "renew", "--nginx", "--non-interactive")
	if err != nil {
		return fmt.Errorf("failed to renew certificate: %w\nOutput: %s", err, output)
	}

	s.log.Success("SSL certificate renewed successfully")
	return nil
}

// ============================================================
// Orchestration
// ============================================================

/* Setup requests a certificate for the domain. */
func (s *SSLManager) Setup() error {
	return s.RequestCertificate()
}
