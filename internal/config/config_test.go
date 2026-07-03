package config

import (
	"strings"
	"testing"
)

// ============================================================
// Helpers
// ============================================================

/* webhookEnv returns a valid set of webhook values for tests to mutate. */
func webhookEnv() map[string]string {
	return map[string]string{
		"GITHUB_REPO_NAME":      "notifykit",
		"GITHUB_BRANCH":         "main",
		"GITHUB_REPO_OWNER":     "Brayzonn",
		"GITHUB_PUSHER":         "Brayzonn",
		"GITHUB_COMMIT":         strings.Repeat("a", 40),
		"GITHUB_REPO_FULL_NAME": "Brayzonn/notifykit",
	}
}

// ============================================================
// Tests
// ============================================================

/* TestValidateWebhookValues checks that unsafe webhook values are rejected and safe ones pass. */
func TestValidateWebhookValues(t *testing.T) {
	if err := validateWebhookValues(webhookEnv()); err != nil {
		t.Fatalf("valid values rejected: %v", err)
	}

	valid := webhookEnv()
	valid["GITHUB_BRANCH"] = "feature/new-thing"
	valid["GITHUB_REPO_NAME"] = "URL-Shortener-App"
	valid["GITHUB_REPO_FULL_NAME"] = "Brayzonn/URL-Shortener-App"
	if err := validateWebhookValues(valid); err != nil {
		t.Fatalf("feature branch / hyphenated name rejected: %v", err)
	}

	rejected := []struct {
		key   string
		value string
		why   string
	}{
		{"GITHUB_REPO_NAME", "../../../etc", "path traversal in repo name"},
		{"GITHUB_REPO_NAME", "a/b", "path separator in repo name"},
		{"GITHUB_REPO_NAME", "..", "bare traversal as repo name"},
		{"GITHUB_REPO_OWNER", "../root", "path traversal in owner"},
		{"GITHUB_BRANCH", "--upload-pack=evil", "git flag injection via branch"},
		{"GITHUB_BRANCH", "a/../b", "path traversal in branch"},
		{"GITHUB_BRANCH", "a b", "whitespace in branch"},
		{"GITHUB_COMMIT", "abc", "short commit hash"},
		{"GITHUB_COMMIT", strings.Repeat("z", 40), "non-hex commit hash"},
		{"GITHUB_REPO_FULL_NAME", "someone-else/notifykit", "full name not matching owner/name"},
	}

	for _, tc := range rejected {
		env := webhookEnv()
		env[tc.key] = tc.value
		if err := validateWebhookValues(env); err == nil {
			t.Errorf("%s: %s=%q was accepted, want rejection", tc.why, tc.key, tc.value)
		}
	}
}

/* TestGetRepoConfigAllowlist checks unknown repos are rejected and known ones get a deploy branch. */
func TestGetRepoConfigAllowlist(t *testing.T) {
	if _, err := GetRepoConfig("not-a-real-repo", "Brayzonn"); err == nil {
		t.Error("unknown repo was accepted, want allowlist rejection")
	}

	cfg, err := GetRepoConfig("notifykit", "Brayzonn")
	if err != nil {
		t.Fatalf("known repo rejected: %v", err)
	}
	if cfg.DeployBranch != "main" {
		t.Errorf("DeployBranch = %q, want default %q", cfg.DeployBranch, "main")
	}
}
