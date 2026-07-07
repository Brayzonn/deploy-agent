package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brayzonn/deploy-agent/internal/run"
)

// ============================================================
// Fixtures
// ============================================================

/*
stubDefaultServer points the sites dirs at a temp directory and wires
the recorder into run, optionally pre-creating the catch-all file so
the create-once no-op path can be exercised.
*/
func stubDefaultServer(t *testing.T, rec *recorder, existing bool) {
	t.Helper()

	tmp := t.TempDir()
	available := filepath.Join(tmp, "sites-available")
	enabled := filepath.Join(tmp, "sites-enabled")
	if err := os.MkdirAll(available, 0755); err != nil {
		t.Fatalf("could not create sites-available: %v", err)
	}
	if err := os.MkdirAll(enabled, 0755); err != nil {
		t.Fatalf("could not create sites-enabled: %v", err)
	}
	t.Cleanup(SetSitesDirsForTest(available, enabled))

	if existing {
		if err := os.WriteFile(filepath.Join(available, defaultTLSSite), []byte(defaultTLSConfig), 0644); err != nil {
			t.Fatalf("could not create existing catch-all: %v", err)
		}
	}

	t.Cleanup(run.Stub(rec.run))
}

// ============================================================
// Default TLS catch-all
// ============================================================

/*
TestDefaultTLSConfigBody checks the catch-all body carries the pieces
the design rests on: default_server on both stacks and the
handshake rejection (no certificate needed).
*/
func TestDefaultTLSConfigBody(t *testing.T) {
	for _, want := range []string{
		"listen 443 ssl default_server;",
		"listen [::]:443 ssl default_server;",
		"ssl_reject_handshake on;",
	} {
		if !strings.Contains(defaultTLSConfig, want) {
			t.Errorf("catch-all body missing %q:\n%s", want, defaultTLSConfig)
		}
	}
}

/*
TestEnsureDefaultServerCreates checks a missing catch-all is written,
enabled, tested, and reloaded, in that order.
*/
func TestEnsureDefaultServerCreates(t *testing.T) {
	rec := &recorder{}
	stubDefaultServer(t, rec, false)

	if err := EnsureDefaultServer(testLogger(t)); err != nil {
		t.Fatalf("EnsureDefaultServer: %v", err)
	}

	teeIdx, lnIdx, testIdx, reloadIdx := rec.indexOf("tee"), rec.indexOf("ln -s"),
		rec.indexOf("nginx -t"), rec.indexOf("systemctl reload nginx")
	if teeIdx < 0 || lnIdx < 0 || testIdx < 0 || reloadIdx < 0 {
		t.Fatalf("create path should tee, ln, nginx -t and reload, ran: %v", rec.calls)
	}
	if !(teeIdx < lnIdx && lnIdx < testIdx && testIdx < reloadIdx) {
		t.Errorf("create order should be tee → ln → nginx -t → reload, ran: %v", rec.calls)
	}
	if !rec.called(defaultTLSSite) {
		t.Errorf("commands should target the %s site, ran: %v", defaultTLSSite, rec.calls)
	}
}

/*
TestEnsureDefaultServerNoOpWhenExists checks an existing catch-all is
left completely alone — create-once means not a single command runs.
*/
func TestEnsureDefaultServerNoOpWhenExists(t *testing.T) {
	rec := &recorder{}
	stubDefaultServer(t, rec, true)

	if err := EnsureDefaultServer(testLogger(t)); err != nil {
		t.Fatalf("EnsureDefaultServer: %v", err)
	}

	if len(rec.calls) != 0 {
		t.Errorf("existing catch-all must be a no-op, ran: %v", rec.calls)
	}
}

/*
TestEnsureDefaultServerTestFailureRemoves checks a failed nginx test
(old nginx, or a competing default_server) removes what this run
created — config and symlink — and surfaces the error.
*/
func TestEnsureDefaultServerTestFailureRemoves(t *testing.T) {
	rec := &recorder{failTest: true}
	stubDefaultServer(t, rec, false)

	if err := EnsureDefaultServer(testLogger(t)); err == nil {
		t.Fatal("expected EnsureDefaultServer to fail when nginx -t fails")
	}

	removed := 0
	for _, c := range rec.calls {
		if strings.Contains(c, "rm -f") && strings.Contains(c, defaultTLSSite) {
			removed++
		}
	}
	if removed != 2 {
		t.Errorf("failed create should remove config AND symlink (2 rm calls), got %d, ran: %v", removed, rec.calls)
	}
	if rec.called("systemctl reload nginx") {
		t.Errorf("failed create must not reload, ran: %v", rec.calls)
	}
}
