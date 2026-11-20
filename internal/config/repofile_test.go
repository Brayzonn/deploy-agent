package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================
// Helpers
// ============================================================

/* writeReposFile writes content to a temp repos file and points the loader at it. */
func writeReposFile(t *testing.T, content string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "repos.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("could not write repos file: %v", err)
	}
	t.Setenv(reposFileEnvVar, path)
}

// ============================================================
// Tests
// ============================================================

/* TestGetRepoConfigFromFile checks a valid repos file is loaded with defaults applied. */
func TestGetRepoConfigFromFile(t *testing.T) {
	writeReposFile(t, `{
		"my-app": {
			"repo_dir": "/home/deploy/my-app",
			"web_root": "/var/www/html/my-app",
			"project_type": "CLIENT",
			"client_dir": ".",
			"domain": "my-app.example.com"
		}
	}`)

	cfg, err := GetRepoConfig("my-app", "Brayzonn")
	if err != nil {
		t.Fatalf("valid repos file rejected: %v", err)
	}
	if cfg.Name != "my-app" {
		t.Errorf("Name = %q, want key default %q", cfg.Name, "my-app")
	}
	if cfg.DeployBranch != "main" {
		t.Errorf("DeployBranch = %q, want default %q", cfg.DeployBranch, "main")
	}
	if cfg.Domain != "my-app.example.com" {
		t.Errorf("Domain = %q, want %q", cfg.Domain, "my-app.example.com")
	}

	if _, err := GetRepoConfig("example-app", "Brayzonn"); err == nil {
		t.Error("repo absent from the file was accepted, want allowlist rejection")
	}
}

/* TestReposFileRejectsBadConfigs checks malformed files and unsafe values fail loudly. */
func TestReposFileRejectsBadConfigs(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"unknown field", `{"my-app": {"repo_dir": "/home/deploy/my-app", "project_type": "CLIENT", "helth_check_url": "x"}}`},
		{"invalid JSON", `{"my-app": `},
		{"unknown project type", `{"my-app": {"repo_dir": "/home/deploy/my-app", "project_type": "WASM"}}`},
		{"relative repo dir", `{"my-app": {"repo_dir": "home/deploy/my-app", "project_type": "CLIENT"}}`},
		{"repo dir traversal", `{"my-app": {"repo_dir": "/home/deploy/../../etc", "project_type": "CLIENT"}}`},
		{"web root outside served parent", `{"my-app": {"repo_dir": "/home/deploy/my-app", "project_type": "CLIENT", "web_root": "/etc/nginx"}}`},
		{"web root escaping via traversal", `{"my-app": {"repo_dir": "/home/deploy/my-app", "project_type": "CLIENT", "web_root": "/var/www/html/../.."}}`},
		{"client web root outside served parent", `{"my-app": {"repo_dir": "/home/deploy/my-app", "project_type": "DOCKER", "client_web_root": "/root"}}`},
		{"name not matching key", `{"my-app": {"name": "other-app", "repo_dir": "/home/deploy/my-app", "project_type": "CLIENT"}}`},
		{"null config", `{"my-app": null}`},
		{"docker without health check url", `{"my-app": {"repo_dir": "/home/deploy/my-app", "project_type": "DOCKER", "use_docker": true}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeReposFile(t, tc.content)
			if _, err := GetRepoConfig("my-app", "Brayzonn"); err == nil {
				t.Errorf("%s was accepted, want rejection", tc.name)
			}
		})
	}
}

/* TestReposFileMissing checks a missing repos file is a clear error — there is no built-in fallback. */
func TestReposFileMissing(t *testing.T) {
	t.Setenv(reposFileEnvVar, filepath.Join(t.TempDir(), "does-not-exist.json"))

	_, err := GetRepoConfig("example-app", "Brayzonn")
	if err == nil {
		t.Fatal("missing repos file was accepted, want error")
	}
	if !strings.Contains(err.Error(), "repos.example.json") {
		t.Errorf("missing-file error should point at the install instructions, got: %v", err)
	}
}
