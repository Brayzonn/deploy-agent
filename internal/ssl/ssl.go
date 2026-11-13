package ssl

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/run"
)

const certbotTimeout = 5 * time.Minute

/*
ErrRateLimited signals that certbot hit a Let's Encrypt rate limit, so
no certificate was issued or renewed this run. It is returned rather
than swallowed so the caller can decide what it means: on a fresh site,
carry on HTTP-only; on a regenerated config it means certbot did NOT
re-add the 443 block, so the site must be restored to its pre-deploy
SSL state instead of being left HTTPS-down.
*/
var ErrRateLimited = errors.New("Let's Encrypt rate limit reached")

type Cert struct {
	domain        string
	domainAliases []string
	email         string
	log           *logger.Logger
}

// ============================================================
// Construction
// ============================================================

/* New creates a Cert for the given domain and its aliases. */
func New(domain string, domainAliases []string, email string, log *logger.Logger) *Cert {
	return &Cert{
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
RequestCertificate runs certbot for the domain and its aliases. An
"already valid" response is treated as success; a Let's Encrypt
rate-limit response returns ErrRateLimited so the caller can decide
whether that is safe to carry on from.
*/
func (s *Cert) RequestCertificate() error {
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
		"--expand",
		"--redirect",
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
			return ErrRateLimited
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
func (s *Cert) RenewCertificate() error {
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
func (s *Cert) Setup() error {
	return s.RequestCertificate()
}
