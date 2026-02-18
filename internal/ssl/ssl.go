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

func (s *SSLManager) CertExists() bool {
	certPath := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", s.domain)
	
	cmd := exec.Command("sudo", "test", "-f", certPath)
	err := cmd.Run()
	return err == nil
}

//  request SSL certificate from Let's Encrypt
func (s *SSLManager) RequestCertificate() error {
	if s.CertExists() {
		s.log.Info("SSL certificate already exists, skipping")
		return nil
	}

	s.log.Info("Requesting NEW SSL certificate from Let's Encrypt...")

	// Build domain list: -d domain -d alias1 -d alias2
	domains := []string{"-d", s.domain}
	for _, alias := range s.domainAliases {
		domains = append(domains, "-d", alias)
	}

	// Build certbot command
	args := []string{
		"certbot",
		"--nginx",
		"--non-interactive",
		"--agree-tos",
	}

	// Add email 
	if s.email != "" {
		args = append(args, "--email", s.email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}

	// Add domains
	args = append(args, domains...)

	cmd := exec.Command("sudo", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Check if it's a rate limit error
		if strings.Contains(string(output), "too many certificates") {
			s.log.Warning("Let's Encrypt rate limit reached. Certificate will be requested later.")
			return nil
		}

		return fmt.Errorf("failed to request SSL certificate: %w\nOutput: %s", err, string(output))
	}

	s.log.Success("SSL certificate obtained successfully")
	s.log.Infof("Certificate location: /etc/letsencrypt/live/%s/", s.domain)
	return nil
}

//  renew existing certificate
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

//  requests certificate 	
func (s *SSLManager) Setup() error {
	return s.RequestCertificate()
}