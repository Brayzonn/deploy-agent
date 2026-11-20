package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brayzonn/deploy-agent/internal/config"
	"github.com/Brayzonn/deploy-agent/internal/lock"
	"github.com/Brayzonn/deploy-agent/internal/logger"
	"github.com/Brayzonn/deploy-agent/internal/nginx"
	"github.com/Brayzonn/deploy-agent/internal/run"
	"github.com/Brayzonn/deploy-agent/pkg/types"
)

// ============================================================
// Fixtures
// ============================================================

/*
edgeStub scripts the nginx + certbot commands setupEdge shells out to.
`cat` answers with catOut (the scripted on-disk config that drives the
outcome decision); certbot returns certbotOut and fails when certbotErr
is set, so the regenerated restore path can be exercised.
*/
type edgeStub struct {
	catOut     string
	certbotOut string
	certbotErr bool

	calls []string
}

func (s *edgeStub) run(dir, name string, args ...string) (string, error) {
	s.calls = append(s.calls, name+" "+strings.Join(args, " "))

	if name == "sudo" && len(args) >= 1 {
		switch args[0] {
		case "cat":
			return s.catOut, nil
		case "certbot":
			if s.certbotErr {
				return s.certbotOut, fmt.Errorf("certbot exploded")
			}
			return s.certbotOut, nil
		}
	}
	return "", nil
}

/*
restoredConfig reports whether a config was restored from backup — the
restore copies the backup file BACK over the live config, i.e. a
`sudo cp <backup> .../example.com` whose call ends in the domain.
*/
func (s *edgeStub) restoredConfig() bool {
	for _, c := range s.calls {
		if strings.HasPrefix(c, "sudo cp ") && strings.HasSuffix(c, "/example.com") {
			return true
		}
	}
	return false
}

/*
newEdgeExecutor builds a minimal Executor whose nginx sites dirs point
at a temp directory and wires the stub into run. When configExists is
true a config file is created at the site's available path so the
outcome decision sees a pre-existing config to regenerate.
*/
func newEdgeExecutor(t *testing.T, stub *edgeStub, configExists bool) *Executor {
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
	t.Cleanup(nginx.SetSitesDirsForTest(available, enabled))

	if configExists {
		if err := os.WriteFile(filepath.Join(available, "example.com"), []byte(stub.catOut), 0644); err != nil {
			t.Fatalf("could not create existing config: %v", err)
		}
	}

	log, err := logger.New("test", filepath.Join(tmp, "logs"))
	if err != nil {
		t.Fatalf("could not create logger: %v", err)
	}
	t.Cleanup(func() { log.Close() })

	t.Cleanup(run.Stub(stub.run))

	cfg := &config.Config{
		StateDir:  filepath.Join(tmp, "state"),
		BackupDir: filepath.Join(tmp, "backups"),
		SSLEmail:  "ops@example.com",
	}
	ctx := &types.DeploymentContext{Config: &types.RepoConfig{}}

	return New(ctx, cfg, log)
}

// ============================================================
// Edge coordination
// ============================================================

/*
TestSetupEdgeRegeneratedCertbotErrorRestores checks that when a
regenerated config's certbot step fails, the pre-deploy config is
restored so the site is never left HTTPS-down.
*/
func TestSetupEdgeRegeneratedCertbotErrorRestores(t *testing.T) {
	stub := &edgeStub{
		catOut:     "server {\n    listen 80;\n}",
		certbotOut: "some transient certbot failure",
		certbotErr: true,
	}
	e := newEdgeExecutor(t, stub, true)

	e.setupEdge("example.com", nil, "/var/www/html/example.com", types.ProjectTypeClient, 3000, 0, "")

	if !stub.restoredConfig() {
		t.Fatalf("regenerated + certbot error should restore the backup, ran: %v", stub.calls)
	}
}

/*
TestSetupEdgeRegeneratedRateLimitRestores checks a rate limit on a
regenerated config triggers the same restore — certbot did NOT re-add
the 443 block, so carrying on would leave the site HTTPS-down.
*/
func TestSetupEdgeRegeneratedRateLimitRestores(t *testing.T) {
	stub := &edgeStub{
		catOut:     "server {\n    listen 80;\n}",
		certbotOut: "too many certificates already issued",
		certbotErr: true,
	}
	e := newEdgeExecutor(t, stub, true)

	e.setupEdge("example.com", nil, "/var/www/html/example.com", types.ProjectTypeClient, 3000, 0, "")

	if !stub.restoredConfig() {
		t.Fatalf("regenerated + rate limit should restore the backup, ran: %v", stub.calls)
	}
}

/*
TestSetupEdgeCreatedRateLimitNoRestore checks that a rate limit on a
brand-new (created) site is warn-only: nothing to restore, so no
config is copied back.
*/
func TestSetupEdgeCreatedRateLimitNoRestore(t *testing.T) {
	stub := &edgeStub{
		certbotOut: "too many certificates already issued",
		certbotErr: true,
	}
	e := newEdgeExecutor(t, stub, false)

	e.setupEdge("example.com", nil, "/var/www/html/example.com", types.ProjectTypeClient, 3000, 0, "")

	if !stub.called("certbot") {
		t.Fatalf("created path should still request a certificate, ran: %v", stub.calls)
	}
	if stub.restoredConfig() {
		t.Fatalf("created + rate limit must NOT restore anything, ran: %v", stub.calls)
	}
}

/* called reports whether any recorded command contains the substring. */
func (s *edgeStub) called(substr string) bool {
	for _, c := range s.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

/*
TestSetupEdgeEnsuresDefaultTLS checks the edge step creates the
default TLS catch-all (create-once, before the site's own setup) so an
unmatched SNI is refused instead of served another site's cert.
*/
func TestSetupEdgeEnsuresDefaultTLS(t *testing.T) {
	stub := &edgeStub{}
	e := newEdgeExecutor(t, stub, false)

	e.setupEdge("example.com", nil, "/var/www/html/example.com", types.ProjectTypeClient, 3000, 0, "")

	if !stub.called("sites-available/default-tls") {
		t.Fatalf("setupEdge should create the default TLS catch-all, ran: %v", stub.calls)
	}
}

// ============================================================
// Edge lock serialization
// ============================================================

/*
TestSetupEdgeWaitsForEdgeLock checks setupEdge serializes on the global
edge lock: while another holder has nginx-edge.lock, setupEdge blocks
before touching any nginx/certbot state, and proceeds to completion
once the lock is released.
*/
func TestSetupEdgeWaitsForEdgeLock(t *testing.T) {
	stub := &edgeStub{}
	e := newEdgeExecutor(t, stub, false)

	held, err := lock.AcquireEdge(e.cfg.StateDir, e.log)
	if err != nil {
		t.Fatalf("could not pre-acquire edge lock: %v", err)
	}

	done := make(chan struct{})
	go func() {
		e.setupEdge("example.com", nil, "/var/www/html/example.com", types.ProjectTypeClient, 3000, 0, "")
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("setupEdge finished while the edge lock was still held")
	case <-time.After(100 * time.Millisecond):
	}

	held.Release()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("setupEdge did not proceed after the edge lock was released")
	}

	if !stub.called("certbot") {
		t.Fatalf("setupEdge should have completed the edge step after acquiring the lock, ran: %v", stub.calls)
	}
}
