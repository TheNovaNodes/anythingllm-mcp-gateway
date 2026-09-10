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
	"strings"
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

func TestClient_Accessors(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: "http://example.com", DefaultWS: "default-ws"})
	if client.BaseURL() != "http://example.com/api/v1" {
		t.Errorf("unexpected BaseURL: %s", client.BaseURL())
	}
	if client.DefaultWorkspace() != "default-ws" {
		t.Errorf("unexpected DefaultWorkspace: %s", client.DefaultWorkspace())
	}
}

func TestClient_doExecute_Errors(t *testing.T) {
	// Test 401 Unauthorized
	ts401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`Unauthorized`))
	}))
	defer ts401.Close()
	c401 := NewClient(ClientConfig{BaseURL: ts401.URL})
	req401, _ := http.NewRequest("GET", ts401.URL, nil)
	_, code401, err := c401.doExecute(req401)
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
	if code401 != 401 {
		t.Errorf("expected 401, got %d", code401)
	}

	// Test 404 Not Found
	ts404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`Not Found`))
	}))
	defer ts404.Close()
	c404 := NewClient(ClientConfig{BaseURL: ts404.URL})
	req404, _ := http.NewRequest("GET", ts404.URL, nil)
	_, code404, err := c404.doExecute(req404)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if code404 != 404 {
		t.Errorf("expected 404, got %d", code404)
	}

	// Test 500 Internal Server Error
	ts500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`Internal Error`))
	}))
	defer ts500.Close()
	c500 := NewClient(ClientConfig{BaseURL: ts500.URL})
	req500, _ := http.NewRequest("GET", ts500.URL, nil)
	_, code500, err := c500.doExecute(req500)
	if err == nil || !strings.Contains(err.Error(), "HTTP error 500") {
		t.Errorf("expected 500 error, got %v", err)
	}
	if code500 != 500 {
		t.Errorf("expected 500, got %d", code500)
	}

    // Context timeout test
	cTimeout := NewClient(ClientConfig{BaseURL: "http://localhost:12345"}) // Should fail to connect
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	reqTimeout, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost:12345", nil)
	_, _, err = cTimeout.doExecute(reqTimeout)
	if err == nil {
		t.Errorf("expected network error")
	}
}

func TestClient_SearchWorkspaceVectors_EdgeCases(t *testing.T) {
	// Test empty hits (noise filtered out)
	tsEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := VectorSearchResponse{
			Results: []RawVectorResult{
				{ID: "noise", Text: "noise", Score: 0.99, Distance: 0.9}, // Should be filtered out
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer tsEmpty.Close()
	cEmpty := NewClient(ClientConfig{BaseURL: tsEmpty.URL})
	hits, err := cEmpty.SearchWorkspaceVectors(context.Background(), "ws1", "query", 5, 0.1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits after noise filtering, got %d", len(hits))
	}
}

func TestClient_Endpoints_Errors(t *testing.T) {
	c := NewClient(ClientConfig{BaseURL: "http://localhost:12345"})
	
	// CreateWorkspace empty name
	_, err := c.CreateWorkspace(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "workspace name cannot be empty") {
		t.Errorf("expected empty name error, got %v", err)
	}

	// UploadRawText empty text
	_, err = c.UploadRawText(context.Background(), "  ", nil)
	if err == nil || !strings.Contains(err.Error(), "textContent cannot be empty") {
		t.Errorf("expected empty text error, got %v", err)
	}

	// UpdateEmbeddings empty slug
	err = c.UpdateEmbeddings(context.Background(), "  ", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "workspace slug cannot be empty") {
		t.Errorf("expected empty slug error, got %v", err)
	}
}
func TestClient_GetWorkspaceSlugs(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workspaces" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"workspaces": [{"slug": "ws1"}, {"slug": "ws2"}]}`))
	}))
	defer ts.Close()

	client := NewClient(ClientConfig{BaseURL: ts.URL})
	slugs, err := client.GetWorkspaceSlugs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(slugs) != 2 || slugs[0] != "ws1" || slugs[1] != "ws2" {
		t.Errorf("unexpected slugs: %v", slugs)
	}
}

func TestClient_ListWorkspaces(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workspaces" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"workspaces": [{"slug": "ws1", "name": "Workspace 1"}]}`))
	}))
	defer ts.Close()

	client := NewClient(ClientConfig{BaseURL: ts.URL})
	workspaces, err := client.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].Slug != "ws1" || workspaces[0].Name != "Workspace 1" {
		t.Errorf("unexpected workspaces: %v", workspaces)
	}
}

func TestClient_CreateWorkspace(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workspace/new" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"workspace": {"slug": "new-ws", "name": "New WS"}}`))
	}))
	defer ts.Close()

	client := NewClient(ClientConfig{BaseURL: ts.URL})
	ws, err := client.CreateWorkspace(context.Background(), "New WS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws == nil || ws.Slug != "new-ws" || ws.Name != "New WS" {
		t.Errorf("unexpected workspace: %v", ws)
	}
}

func TestClient_UploadRawText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/document/raw-text" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"documents": [{"location": "loc123", "title": "my title"}]}`))
	}))
	defer ts.Close()

	client := NewClient(ClientConfig{BaseURL: ts.URL})
	loc, err := client.UploadRawText(context.Background(), "some content", map[string]interface{}{"title": "my title"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loc != "loc123" {
		t.Errorf("unexpected location: %s", loc)
	}
}

func TestClient_UpdateEmbeddings(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workspace/ws-slug/update-embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success": true}`))
	}))
	defer ts.Close()

	client := NewClient(ClientConfig{BaseURL: ts.URL})
	err := client.UpdateEmbeddings(context.Background(), "ws-slug", []string{"add1"}, []string{"del1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_doExecute_BodyErrors(t *testing.T) {
	// Test malformed JSON returned in response where unexpected JSON parsing might fail.
	tsHTML := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html><html><body>Error</body></html>`))
	}))
	defer tsHTML.Close()
	cHTML := NewClient(ClientConfig{BaseURL: tsHTML.URL})
	reqHTML, _ := http.NewRequest("GET", tsHTML.URL, nil)
	_, _, err := cHTML.doExecute(reqHTML)
	if !errors.Is(err, ErrNonJSONResponse) {
		t.Errorf("expected ErrNonJSONResponse, got %v", err)
	}
}

func TestClient_Endpoints_ParseErrors(t *testing.T) {
	tsInvalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"workspaces": invalid json}`))
	}))
	defer tsInvalid.Close()

	c := NewClient(ClientConfig{BaseURL: tsInvalid.URL})
	
	_, err := c.GetWorkspaceSlugs(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to parse workspaces list") {
		t.Errorf("expected parse error for GetWorkspaceSlugs, got %v", err)
	}

	_, err = c.ListWorkspaces(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to parse workspaces JSON") {
		t.Errorf("expected parse error for ListWorkspaces, got %v", err)
	}

	_, err = c.CreateWorkspace(context.Background(), "test")
	if err == nil || !strings.Contains(err.Error(), "failed to parse created workspace JSON") {
		t.Errorf("expected parse error for CreateWorkspace, got %v", err)
	}
}

func TestClient_GetWorkspaceSlugs_MapFile(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "mapfile*.json")
	defer os.Remove(tmpFile.Name())
	tmpFile.Write([]byte(`{"ws1": "val1", "ws2": "val2"}`))
	tmpFile.Close()

	c := NewClient(ClientConfig{MapFile: tmpFile.Name()})
	slugs, err := c.GetWorkspaceSlugs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(slugs) != 2 {
		t.Errorf("expected 2 slugs from map file, got %d", len(slugs))
	}
}

func TestClient_SearchWorkspaceVectors_Errors(t *testing.T) {
    // Timeout getting semaphore
	c := NewClient(ClientConfig{BaseURL: "http://localhost", MaxInflight: 1})
    // Acquire the only semaphore slot
    c.sem.Acquire(context.Background(), 1)
    
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
    defer cancel()
    
    _, err := c.SearchWorkspaceVectors(ctx, "ws1", "query", 5, 0.1)
    if err == nil || !strings.Contains(err.Error(), "vector search concurrency queue timeout") {
        t.Errorf("expected semaphore timeout error, got %v", err)
    }
}

func TestClient_SearchWorkspaceVectors_ContextCancel(t *testing.T) {
	c := NewClient(ClientConfig{BaseURL: "http://localhost", MaxInflight: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before even calling

	_, err := c.SearchWorkspaceVectors(ctx, "ws1", "query", 5, 0.1)
	if err == nil || !strings.Contains(err.Error(), "vector search concurrency queue timeout") {
		t.Errorf("expected semaphore timeout error from pre-cancelled context, got %v", err)
	}
}

func TestClient_SearchWorkspaceVectors_InflightCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer ts.Close()

	c := NewClient(ClientConfig{BaseURL: ts.URL, MaxInflight: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := c.SearchWorkspaceVectors(ctx, "ws1", "query", 5, 0.1)
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected context deadline exceeded error, got %v", err)
	}
}
