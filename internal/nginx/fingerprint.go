package nginx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Brayzonn/deploy-agent/pkg/types"
)

// ============================================================
// Outcome
// ============================================================

/*
Outcome names what Setup decided to do with a site's config: create a
brand-new one, leave an up-to-date one alone, or regenerate a stale or
adopted one. It drives the coordination in the executor's edge step,
which restores/reloads differently per outcome.
*/
type Outcome string

const (
	OutcomeCreated     Outcome = "created"
	OutcomeUnchanged   Outcome = "unchanged"
	OutcomeRegenerated Outcome = "regenerated"
)

// ============================================================
// Fingerprint marker
// ============================================================

/*
fingerprintPrefix is the first-line marker deploy-agent writes above
every config it owns. The hash after it covers only the agent-generated
body, so certbot's later 443/redirect edits (which change directives,
not this comment) never change the fingerprint — a marker match still
means "the body is current."
*/
const fingerprintPrefix = "# managed-by: deploy-agent fingerprint: "

// ============================================================
// Pure primitives
// ============================================================

/*
desiredConfig renders the config body the site should have, choosing a
static-file template for client projects and a reverse-proxy template
for everything else. Pure — no I/O — so the decision path is testable.
*/
func (n *Site) desiredConfig() (string, error) {
	switch n.projectType {
	case types.ProjectTypeClient:
		return n.generateClientConfig(), nil
	case types.ProjectTypeAPIJS, types.ProjectTypeAPITS, types.ProjectTypeDocker:
		return n.generateAPIConfig(), nil
	default:
		return "", fmt.Errorf("unsupported project type for nginx: %s", n.projectType)
	}
}

/* configFingerprint returns the sha256 hex of the config body only. */
func configFingerprint(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

/* markerLine builds the first-line fingerprint comment for the given hash. */
func markerLine(hash string) string {
	return fingerprintPrefix + hash
}

/*
parseFingerprint reads the fingerprint hash off the first line of a
config's contents, returning "" when the marker is absent or malformed
(a pre-existing or manually-written config).
*/
func parseFingerprint(contents string) string {
	first := contents
	if i := strings.IndexByte(contents, '\n'); i >= 0 {
		first = contents[:i]
	}
	first = strings.TrimRight(first, "\r")

	if !strings.HasPrefix(first, fingerprintPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(first, fingerprintPrefix))
}

/*
planAction is the whole decision table as one pure function: no file on
disk → created; the on-disk marker matches the wanted body → unchanged;
the marker differs OR is missing (stale template/settings, or a
pre-existing/manual config being adopted) → regenerated. Keeping this
side-effect-free is the seam that makes the full outcome matrix testable
without root-owned /etc/nginx paths.
*/
func planAction(exists bool, existing, want string) Outcome {
	if !exists {
		return OutcomeCreated
	}
	if parseFingerprint(existing) == configFingerprint(want) {
		return OutcomeUnchanged
	}
	return OutcomeRegenerated
}
