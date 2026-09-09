package alm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanChunk(t *testing.T) {
	in := "<document_metadata>\npath: /foo/bar\n</document_metadata>\n\npassage: Hello world\u2026 test"
	expected := "Hello world  test"
	actual := CleanChunk(in)
	if actual != expected {
		t.Errorf("CleanChunk() = %q; expected %q", actual, expected)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	if NormalizeBaseURL("http://localhost:3002") != "http://localhost:3002/api/v1" {
		t.Error("failed basic normalization")
	}
	if NormalizeBaseURL("http://localhost:3002/api") != "http://localhost:3002/api/v1" {
		t.Error("failed /api normalization")
	}
	if NormalizeBaseURL("http://localhost:3002/api/v1/") != "http://localhost:3002/api/v1" {
		t.Error("failed trailing slash normalization")
	}
}

func TestClient_SearchWorkspaceVectors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workspace/test-ws/vector-search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := VectorSearchResponse{
			Results: []RawVectorResult{
				{
					ID:    "doc-1",
					Text:  "passage: Result text",
					Score: 0.85,
					Metadata: map[string]interface{}{
						"title":   "Title 1",
						"docpath": "path/to/doc.md",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(ClientConfig{
		BaseURL: ts.URL,
		APIKey:  "test-key",
	})

	hits, err := client.SearchWorkspaceVectors(context.Background(), "test-ws", "my query", 5, 0.1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].DocID != "path/to/doc.md" || hits[0].Text != "Result text" {
		t.Errorf("unexpected hit: %+v", hits[0])
	}
}


func TestClient_TokenFileReload(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token.txt")
	os.WriteFile(tokenPath, []byte("tok-v1"), 0600)

	client := NewClient(ClientConfig{
		BaseURL:   "http://localhost:3002",
		TokenFile: tokenPath,
	})

	if tok := client.getToken(); tok != "tok-v1" {
		t.Errorf("expected tok-v1, got %q", tok)
	}

	// Update token file
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(tokenPath, []byte("tok-v2"), 0600)

	if tok := client.getToken(); tok != "tok-v2" {
		t.Errorf("expected tok-v2, got %q", tok)
	}
}

func TestClient_SPAGuard(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<!DOCTYPE html><html><body>SPA</body></html>"))
	}))
	defer ts.Close()

	client := NewClient(ClientConfig{
		BaseURL: ts.URL,
	})

	_, err := client.GetWorkspaceSlugs(context.Background())
	if !errors.Is(err, ErrNonJSONResponse) {
		t.Errorf("expected ErrNonJSONResponse, got %v", err)
	}
}
