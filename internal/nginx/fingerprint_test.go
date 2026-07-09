package nginx

import (
	"strings"
	"testing"

	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

// ============================================================
// Shared test helpers
// ============================================================

/* testLogger builds a throwaway logger writing under the test's temp dir. */
func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New("test", t.TempDir())
	if err != nil {
		t.Fatalf("could not create logger: %v", err)
	}
	t.Cleanup(func() { log.Close() })
	return log
}

/* markedConfig returns the full on-disk contents (marker + body) for a body. */
func markedConfig(body string) string {
	return markerLine(configFingerprint(body)) + "\n" + body
}

/* bodyFor renders the desired config body for the given settings. */
func bodyFor(t *testing.T, aliases []string, webRoot string, pt types.ProjectType, port int) string {
	t.Helper()
	body, err := New("example.com", aliases, webRoot, pt, port, 0, "", "", nil).desiredConfig()
	if err != nil {
		t.Fatalf("desiredConfig: %v", err)
	}
	return body
}

// ============================================================
// Fingerprint round-trip
// ============================================================

/*
TestFingerprintRoundTrip checks that a body written with its marker
parses back to the same fingerprint — the property planAction relies on
to recognise a config the agent generated.
*/
func TestFingerprintRoundTrip(t *testing.T) {
	body := bodyFor(t, nil, "/var/www/example", types.ProjectTypeClient, 0)
	want := configFingerprint(body)

	if got := parseFingerprint(markedConfig(body)); got != want {
		t.Fatalf("round-trip fingerprint = %q, want %q", got, want)
	}
}

/*
TestFingerprintSurvivesTrailingEdits checks that directives appended
below the marker — standing in for certbot's later 443/redirect edits —
do not change the parsed fingerprint, since only the first line is read.
This is what lets an SSL'd config still read as unchanged.
*/
func TestFingerprintSurvivesTrailingEdits(t *testing.T) {
	body := bodyFor(t, nil, "", types.ProjectTypeAPITS, 3000)
	want := configFingerprint(body)

	certbotEdited := markedConfig(body) + "\nserver {\n    listen 443 ssl; # managed by Certbot\n}\n"

	if got := parseFingerprint(certbotEdited); got != want {
		t.Fatalf("fingerprint after trailing edits = %q, want %q", got, want)
	}
}

// ============================================================
// parseFingerprint on missing / garbled markers
// ============================================================

/* TestParseFingerprintMissingOrGarbled checks unmarked or malformed first lines parse to "". */
func TestParseFingerprintMissingOrGarbled(t *testing.T) {
	// markerPrefix reconstructs the marker's fixed prefix (marker minus hash).
	markerPrefix := strings.TrimSuffix(markerLine("x"), "x")

	cases := map[string]string{
		"empty":                 "",
		"no marker":             "server {\n    listen 80;\n}",
		"marker not first line": "server {\n" + markerLine("abc123") + "\n}",
		"wrong prefix":          "# some other comment: abc123\nserver {}",
		"missing space":         "#managed-by: deploy-agent fingerprint: abc123\nserver {}",
		"no hash":               markerPrefix + "\nserver {}",
		"blank hash":            markerPrefix + "   \nserver {}",
	}

	for name, contents := range cases {
		if got := parseFingerprint(contents); got != "" {
			t.Errorf("%s: parseFingerprint = %q, want \"\"", name, got)
		}
	}
}

// ============================================================
// planAction decision matrix
// ============================================================

/*
TestPlanActionMatrix drives the full create / skip / regenerate table:
no file creates; a matching marker skips; any settings drift (port, web
root, alias) or a stale template body regenerates; certbot edits below
an intact marker still skip; and a config with no marker at all is
regenerated (adoption).
*/
func TestPlanActionMatrix(t *testing.T) {
	clientBody := bodyFor(t, nil, "/var/www/example", types.ProjectTypeClient, 0)
	clientNewRoot := bodyFor(t, nil, "/var/www/other", types.ProjectTypeClient, 0)
	clientNewAlias := bodyFor(t, []string{"www.example.com"}, "/var/www/example", types.ProjectTypeClient, 0)
	apiBody := bodyFor(t, nil, "", types.ProjectTypeAPITS, 3000)
	apiNewPort := bodyFor(t, nil, "", types.ProjectTypeAPITS, 4000)
	// The pre-IPv6 template body (no `listen [::]:80;`) — the concrete stale
	// body the whole change exists to fix.
	oldTemplate := strings.Replace(clientBody, "\n    listen [::]:80;", "", 1)

	tests := []struct {
		name     string
		exists   bool
		existing string
		want     string
		outcome  Outcome
	}{
		{"no file -> create", false, "", clientBody, OutcomeCreated},
		{"marker match -> unchanged", true, markedConfig(clientBody), clientBody, OutcomeUnchanged},
		{"certbot edits below marker -> unchanged", true, markedConfig(clientBody) + "\nlisten 443 ssl; # certbot", clientBody, OutcomeUnchanged},
		{"changed port -> regenerate", true, markedConfig(apiBody), apiNewPort, OutcomeRegenerated},
		{"changed web root -> regenerate", true, markedConfig(clientBody), clientNewRoot, OutcomeRegenerated},
		{"changed alias -> regenerate", true, markedConfig(clientBody), clientNewAlias, OutcomeRegenerated},
		{"stale template body -> regenerate", true, markedConfig(oldTemplate), clientBody, OutcomeRegenerated},
		{"no marker -> regenerate (adopt)", true, clientBody, clientBody, OutcomeRegenerated},
	}

	for _, tc := range tests {
		if got := planAction(tc.exists, tc.existing, tc.want); got != tc.outcome {
			t.Errorf("%s: planAction = %s, want %s", tc.name, got, tc.outcome)
		}
	}
}

// ============================================================
// Template IPv6 guarantee
// ============================================================

/*
TestTemplatesListenIPv6 asserts both generated bodies bind IPv6 port 80.
This is the whole point of the regen rollout: only once a site's config
carries `listen [::]:80;` will certbot add its `[::]:443` block, so IPv6
requests stop being served another vhost's certificate.
*/
func TestTemplatesListenIPv6(t *testing.T) {
	bodies := map[string]string{
		"client": bodyFor(t, nil, "/var/www/example", types.ProjectTypeClient, 0),
		"api":    bodyFor(t, nil, "", types.ProjectTypeAPITS, 3000),
	}
	for name, body := range bodies {
		if !strings.Contains(body, "listen [::]:80;") {
			t.Errorf("%s body missing IPv6 listen directive:\n%s", name, body)
		}
	}
}
