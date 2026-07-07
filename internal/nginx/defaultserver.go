package nginx

import (
	"github.com/Brayzonn/deploy-agent/internal/logger"
)

// ============================================================
// Default TLS catch-all
// ============================================================

/* defaultTLSSite is the fixed sites-available name of the catch-all vhost. */
const defaultTLSSite = "default-tls"

/*
defaultTLSConfig refuses the TLS handshake for any SNI that no vhost
matches (ssl_reject_handshake, nginx >= 1.19.4) instead of serving
whichever certificate nginx falls back to — the wrong-cert leak for a
domain that has a DNS record pointing here but no matching server
block. Rejecting at the handshake needs no certificate of its own.
*/
const defaultTLSConfig = `server {
    listen 443 ssl default_server;
    listen [::]:443 ssl default_server;
    ssl_reject_handshake on;
}`

/*
EnsureDefaultServer writes the catch-all vhost once if it is missing:
write, enable, test, reload — removing whatever it created if the
nginx test fails (an nginx older than 1.19.4, or a pre-existing 443
default_server elsewhere in the tree, would reject it). Create-once by
design: only one default_server is allowed per socket, so this must
NOT live in the per-domain template, and an existing file is left
completely alone — never fingerprint-checked or regenerated. Callers
must hold the global edge lock, like all nginx mutations.
*/
func EnsureDefaultServer(log *logger.Logger) error {
	s := &Site{domain: defaultTLSSite, log: log}

	if s.ConfigExists() {
		return nil
	}

	log.Info("Creating default TLS catch-all server (unmatched SNI will be refused)...")

	if err := s.writeConfig(defaultTLSConfig); err != nil {
		return err
	}

	enabledSite, err := s.EnableSite()
	if err != nil {
		s.removeCreated(true, false)
		return err
	}

	if err := s.TestConfig(); err != nil {
		log.Warning("Removing the just-created default TLS catch-all so it can't break future deploys...")
		s.removeCreated(true, enabledSite)
		return err
	}

	if err := s.Reload(); err != nil {
		return err
	}

	log.Success("Default TLS catch-all created")
	return nil
}
