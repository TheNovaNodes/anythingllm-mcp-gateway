package alm

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestLiveContract verifies compatibility with a live AnythingLLM instance on port 3002.
func TestLiveContract(t *testing.T) {
	baseURL := os.Getenv("MG_ALM_BASE")
	if baseURL == "" {
		baseURL = os.Getenv("ANYTHINGLLM_BASE_URL")
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:3002/api/v1"
	}

	apiKey := os.Getenv("MG_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("ANYTHINGLLM_API_KEY")
	}
	if apiKey == "" {
		apiKey = "8f1cde5a87c74a0d9dd742c2c77884c9-anythingllm"
	}

	// Quick TCP probe with 1s timeout
	probeClient := &http.Client{Timeout: 1 * time.Second}
	resp, err := probeClient.Get("http://127.0.0.1:3002/api/v1/workspaces")
	if err != nil {
		t.Skipf("Skipping live contract test: AnythingLLM at 127.0.0.1:3002 unreachable (%v)", err)
	}
	resp.Body.Close()

	client := NewClient(ClientConfig{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		DefaultWS: "default",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. GetWorkspaceSlugs live check
	slugs, err := client.GetWorkspaceSlugs(ctx)
	if err != nil {
		t.Fatalf("Live contract failure on GetWorkspaceSlugs: %v", err)
	}
	t.Logf("Live contract passed: retrieved %d workspace slugs from %s", len(slugs), baseURL)

	// 2. Vector search probe (against first available workspace)
	if len(slugs) > 0 {
		targetSlug := slugs[0]
		hits, err := client.SearchWorkspaceVectors(ctx, targetSlug, "system configuration", 3, 0.0)
		if err != nil {
			t.Fatalf("Live contract failure on SearchWorkspaceVectors for slug %s: %v", targetSlug, err)
		}
		t.Logf("Live contract passed: searched workspace %s, returned %d vector hits", targetSlug, len(hits))
	}
}
