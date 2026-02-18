package ssl

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/Brayzonn/deploy-agent/internal/logger"
)

type SSLManager struct {
	domain        string
	domainAliases []string
	email         string 
	log           *logger.Logger
}

func New(domain string, domainAliases []string, email string, log *logger.Logger) *SSLManager {
	return &SSLManager{
		domain:        domain,
		domainAliases: domainAliases,
		email:         email,
		log:           log,
	}
}

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

	cmd := exec.Command("sudo", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		if strings.Contains(string(output), "too many certificates") {
			s.log.Warning("Let's Encrypt rate limit reached")
			return nil
		}
		return fmt.Errorf("certbot failed: %w\nOutput: %s", err, string(output))
	}

	if strings.Contains(string(output), "Certificate not yet due for renewal") {
		s.log.Info("SSL certificate already exists and is valid")
	} else {
		s.log.Success("SSL certificate obtained successfully")
	}
	
	s.log.Infof("Certificate location: /etc/letsencrypt/live/%s/", s.domain)
	return nil
}

func (s *SSLManager) RenewCertificate() error {
	s.log.Info("Renewing SSL certificate...")

	cmd := exec.Command("sudo", "certbot", "renew", "--nginx", "--non-interactive")
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("failed to renew certificate: %w\nOutput: %s", err, string(output))
	}

	s.log.Success("SSL certificate renewed successfully")
	return nil
}

func (s *SSLManager) Setup() error {
	return s.RequestCertificate()
}