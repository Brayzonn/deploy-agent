package nginx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brayzonn/deploy-agent/internal/run"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

// ============================================================
// Fixtures
// ============================================================

/*
recorder scripts every command a Site shells out to and records each
call so the run.Stub tests can assert on the exact sequence. cat answers
with catOut (the scripted on-disk config the decision path reads), and
`nginx -t` fails when failTest is set, to drive the restore path.
*/
type recorder struct {
	catOut   string
	failTest bool

	calls []string // every command as "name arg arg..."
}

func (r *recorder) run(dir, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))

	switch {
	case name == "sudo" && len(args) >= 1 && args[0] == "cat":
		return r.catOut, nil
	case name == "sudo" && len(args) >= 2 && args[0] == "nginx" && args[1] == "-t":
		if r.failTest {
			return "nginx: configuration file test failed", fmt.Errorf("nginx -t failed")
		}
		return "", nil
	}
	return "", nil
}

/* called reports whether any recorded command contains the substring. */
func (r *recorder) called(substr string) bool {
	for _, c := range r.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

/* indexOf returns the position of the first call containing substr, or -1. */
func (r *recorder) indexOf(substr string) int {
	for i, c := range r.calls {
		if strings.Contains(c, substr) {
			return i
		}
	}
	return -1
}

/*
newTestSite builds a Site whose sites-available/sites-enabled dirs live
under a temp directory (so the os.Stat existence check is testable) and
wires the recorder into run. When existing is true, a config file is
created at the site's available path so ConfigExists reports true.
*/
func newTestSite(t *testing.T, pt types.ProjectType, rec *recorder, existing bool) *Site {
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
		if err := os.WriteFile(filepath.Join(available, "example.com"), []byte(rec.catOut), 0644); err != nil {
			t.Fatalf("could not create existing config: %v", err)
		}
	}

	t.Cleanup(run.Stub(rec.run))

	return New("example.com", nil, "/var/www/html/example.com", pt, 3000, 0, "", filepath.Join(tmp, "backups"), testLogger(t))
}

// ============================================================
// Setup command sequences
// ============================================================

/*
TestSetupCreatedSequence checks a brand-new site runs today's command
sequence — tee, ln, nginx -t, reload — and never backs anything up.
*/
func TestSetupCreatedSequence(t *testing.T) {
	rec := &recorder{}
	site := newTestSite(t, types.ProjectTypeClient, rec, false)

	res, err := site.Setup()
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if res.Outcome != OutcomeCreated {
		t.Fatalf("Outcome = %q, want created", res.Outcome)
	}

	for _, want := range []string{"tee", "ln -s", "nginx -t", "systemctl reload nginx"} {
		if !rec.called(want) {
			t.Errorf("created path should run %q, ran: %v", want, rec.calls)
		}
	}
	if rec.called("cp") {
		t.Errorf("created path should not back up (cp), ran: %v", rec.calls)
	}
}

/*
TestSetupUnchangedSequence checks a current config with an existing
symlink is left completely alone: no tee (no write) and no reload.
*/
func TestSetupUnchangedSequence(t *testing.T) {
	rec := &recorder{}
	// Precompute the marked body so the scripted `cat` matches the wanted body.
	probe := New("example.com", nil, "/var/www/html/example.com", types.ProjectTypeClient, 3000, 0, "", "", nil)
	want, _ := probe.desiredConfig()
	rec.catOut = markerLine(configFingerprint(want)) + "\n" + want

	site := newTestSite(t, types.ProjectTypeClient, rec, true)
	// Pre-create the symlink so EnableSite is a no-op → nothing changes.
	if err := os.WriteFile(site.enabledPath(), []byte("link"), 0644); err != nil {
		t.Fatalf("could not create enabled marker: %v", err)
	}

	res, err := site.Setup()
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if res.Outcome != OutcomeUnchanged {
		t.Fatalf("Outcome = %q, want unchanged", res.Outcome)
	}

	if rec.called("tee") {
		t.Errorf("unchanged path must not write config (tee), ran: %v", rec.calls)
	}
	if rec.called("systemctl reload nginx") {
		t.Errorf("unchanged path with existing symlink must not reload, ran: %v", rec.calls)
	}
}

/*
TestSetupRegeneratedSequence checks a stale config is backed up, then
rewritten and tested, but NOT reloaded (deferred-reload design), in that
order. It also asserts the newly-written body carries the IPv6 listen
line — what makes certbot then emit the matching [::]:443 block.
*/
func TestSetupRegeneratedSequence(t *testing.T) {
	rec := &recorder{}
	rec.catOut = "server {\n    listen 80;\n}" // no marker → regenerated (adoption)

	site := newTestSite(t, types.ProjectTypeClient, rec, true)

	res, err := site.Setup()
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if res.Outcome != OutcomeRegenerated {
		t.Fatalf("Outcome = %q, want regenerated", res.Outcome)
	}
	if res.BackupPath == "" {
		t.Fatal("regenerated Setup should return a BackupPath")
	}

	cpIdx, teeIdx, testIdx := rec.indexOf("cp"), rec.indexOf("tee"), rec.indexOf("nginx -t")
	if cpIdx < 0 || teeIdx < 0 || testIdx < 0 {
		t.Fatalf("regenerated path should cp, tee and nginx -t, ran: %v", rec.calls)
	}
	if !(cpIdx < teeIdx && teeIdx < testIdx) {
		t.Errorf("regenerated order should be cp → tee → nginx -t, ran: %v", rec.calls)
	}
	if rec.called("systemctl reload nginx") {
		t.Errorf("regenerated path must NOT reload (deferred-reload), ran: %v", rec.calls)
	}

	want, _ := site.desiredConfig()
	if !strings.Contains(want, "listen [::]:80;") {
		t.Errorf("regenerated body missing the IPv6 listen directive:\n%s", want)
	}
}

/*
TestSetupRegeneratedTestFailureRestores checks that when `nginx -t`
fails on a regenerated config, the backup is copied back (restore) and
Setup surfaces the error.
*/
func TestSetupRegeneratedTestFailureRestores(t *testing.T) {
	rec := &recorder{failTest: true}
	rec.catOut = "server {\n    listen 80;\n}" // no marker → regenerated

	site := newTestSite(t, types.ProjectTypeClient, rec, true)

	res, err := site.Setup()
	if err == nil {
		t.Fatal("expected Setup to fail when nginx -t fails")
	}
	if res.Outcome != OutcomeRegenerated {
		t.Fatalf("Outcome = %q, want regenerated", res.Outcome)
	}

	// The restore copies the backup file BACK over the live config:
	// `sudo cp <backupPath> <availablePath>`, i.e. a call ending in the domain.
	restored := false
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "sudo cp ") && strings.HasSuffix(c, "/example.com") {
			restored = true
		}
	}
	if !restored {
		t.Errorf("failed regenerate should restore the backup (cp back to config), ran: %v", rec.calls)
	}
}
