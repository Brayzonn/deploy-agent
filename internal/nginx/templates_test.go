package nginx

import (
	"strings"
	"testing"

	"github.com/Brayzonn/deploy-agent/pkg/types"
)

/*
TestGenerateAPIConfigOmitsMaxUploadByDefault checks that leaving
nginxMaxUpload unset renders no client_max_body_size directive, so
nginx's own 1m default applies.
*/
func TestGenerateAPIConfigOmitsMaxUploadByDefault(t *testing.T) {
	body, err := New("api.example.com", nil, "", types.ProjectTypeAPIJS, 3000, 0, "", "", nil).desiredConfig()
	if err != nil {
		t.Fatalf("desiredConfig: %v", err)
	}
	if strings.Contains(body, "client_max_body_size") {
		t.Fatalf("expected no client_max_body_size directive when unset, got:\n%s", body)
	}
}

/*
TestGenerateAPIConfigIncludesMaxUpload checks that a positive
nginxMaxUpload renders as a client_max_body_size directive in MB.
*/
func TestGenerateAPIConfigIncludesMaxUpload(t *testing.T) {
	body, err := New("api.example.com", nil, "", types.ProjectTypeAPIJS, 3000, 5, "", "", nil).desiredConfig()
	if err != nil {
		t.Fatalf("desiredConfig: %v", err)
	}
	if !strings.Contains(body, "client_max_body_size 5m;") {
		t.Fatalf("expected client_max_body_size 5m directive, got:\n%s", body)
	}
}

/*
TestGenerateClientConfigSpaMetaProxy checks that a client site with
spaMetaProxyPath set and a valid port emits the @spa_fallback proxy
branch instead of the plain index.html fallback.
*/
func TestGenerateClientConfigSpaMetaProxy(t *testing.T) {
	body, err := New("nestra.me", nil, "/var/www/nestra.me", types.ProjectTypeClient, 9800, 0, "/api/v1/pages", "", nil).desiredConfig()
	if err != nil {
		t.Fatalf("desiredConfig: %v", err)
	}
	if !strings.Contains(body, "try_files $uri @spa_fallback;") {
		t.Fatalf("expected try_files $uri @spa_fallback;, got:\n%s", body)
	}
	if !strings.Contains(body, "location @spa_fallback {") {
		t.Fatalf("expected location @spa_fallback block, got:\n%s", body)
	}
	if !strings.Contains(body, "proxy_pass http://localhost:9800/api/v1/pages$request_uri;") {
		t.Fatalf("expected proxy_pass to the pages endpoint, got:\n%s", body)
	}
	if strings.Contains(body, "try_files $uri $uri/ /index.html;") {
		t.Fatalf("did not expect the plain SPA fallback, got:\n%s", body)
	}
}

/*
TestGenerateClientConfigSpaMetaProxyRequiresPort checks that
spaMetaProxyPath is ignored when there's no port to proxy to, falling
back to the plain index.html SPA behavior so a misconfigured site
still produces a valid config.
*/
func TestGenerateClientConfigSpaMetaProxyRequiresPort(t *testing.T) {
	body, err := New("nestra.me", nil, "/var/www/nestra.me", types.ProjectTypeClient, 0, 0, "/api/v1/pages", "", nil).desiredConfig()
	if err != nil {
		t.Fatalf("desiredConfig: %v", err)
	}
	if !strings.Contains(body, "try_files $uri $uri/ /index.html;") {
		t.Fatalf("expected the plain SPA fallback when port is unset, got:\n%s", body)
	}
	if strings.Contains(body, "@spa_fallback") {
		t.Fatalf("did not expect the proxy fallback when port is unset, got:\n%s", body)
	}
}
