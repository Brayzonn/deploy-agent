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
	body, err := New("api.example.com", nil, "", types.ProjectTypeAPIJS, 3000, 0, "", nil).desiredConfig()
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
	body, err := New("api.example.com", nil, "", types.ProjectTypeAPIJS, 3000, 5, "", nil).desiredConfig()
	if err != nil {
		t.Fatalf("desiredConfig: %v", err)
	}
	if !strings.Contains(body, "client_max_body_size 5m;") {
		t.Fatalf("expected client_max_body_size 5m directive, got:\n%s", body)
	}
}
