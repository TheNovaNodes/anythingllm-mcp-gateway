package etl

import (
	"testing"
)

func TestIsBlacklisted(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/root/projects/repo/.git/HEAD", true},
		{"/root/projects/repo/node_modules/package.json", true},
		{"/root/projects/repo/.venv/lib/python3.12/site.py", true},
		{"/root/projects/repo/.agents/bot/file.md", true},
		{"/root/projects/repo/scratch/test.md", true},
		{"/root/projects/repo/tmp/data.json", true},
		{"/root/projects/repo/.pytest_cache/v/cache", true},
		{"/root/projects/TheNovaNodes/agent-vault-f04/readme.txt", true},
		{"/root/projects/TheNovaNodes/searxng-poc/main.go", true},
		{"/root/projects/repo/docs/README.md", false},
		{"/root/projects/repo/ARCHITECTURE.md", false},
		{"/root/projects/repo/src/main.go", false},
	}

	for _, tc := range tests {
		got := IsBlacklisted(tc.path)
		if got != tc.expected {
			t.Errorf("IsBlacklisted(%q) = %v; expected %v", tc.path, got, tc.expected)
		}
	}
}

func TestSlugFromRepo(t *testing.T) {
	tests := []struct {
		account  string
		repo     string
		expected string
	}{
		{"TheNovaNodes", "anythingllm-mcp-control", "thenovanodes-anythingllm-mcp-control"},
		{"Nova_Nodes", "searxng_gateway", "nova-nodes-searxng-gateway"},
		{" Org Name ", "Repo Name", "org-name-repo-name"},
	}

	for _, tc := range tests {
		got := SlugFromRepo(tc.account, tc.repo)
		if got != tc.expected {
			t.Errorf("SlugFromRepo(%q, %q) = %q; expected %q", tc.account, tc.repo, got, tc.expected)
		}
	}
}
