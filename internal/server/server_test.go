package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/alm"
	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/lexical"
	"github.com/mark3labs/mcp-go/mcp"
)

func setupTestEnvironment(t *testing.T) (*Server, *httptest.Server, *lexical.DB) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/workspaces":
			json.NewEncoder(w).Encode(alm.WorkspacesEnvelope{
				Workspaces: []struct {
					Slug string `json:"slug"`
					Name string `json:"name"`
				}{
					{Slug: "ws-test", Name: "WS Test"},
				},
			})
		case "/api/v1/workspace/ws-test/vector-search":
			json.NewEncoder(w).Encode(alm.VectorSearchResponse{
				Results: []alm.RawVectorResult{
					{
						ID:       "doc1",
						Text:     "Vector hit content",
						Score:    0.9,
						Metadata: map[string]interface{}{"title": "Title 1", "docpath": "protocols/doc1.md"},
					},
				},
			})
		case "/api/v1/document/raw-text":
			json.NewEncoder(w).Encode(alm.RawUploadResponse{
				Success: true,
				Documents: []alm.RawUploadDocument{
					{ID: "uploaded-1", Location: "/tmp/doc.json"},
				},
			})
		case "/api/v1/workspace/ws-test/update-embeddings":
			w.Write([]byte(`{"success": true}`))
		default:
			http.NotFound(w, r)
		}
	}))

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_lex.db")
	dbInit, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	dbInit.Exec(`
		CREATE VIRTUAL TABLE docs_fts USING fts5(path, title, content);
		INSERT INTO docs_fts(path, title, content) VALUES
			('protocols/doc1.md', 'Title 1', 'Full content paragraph 1\n\nFull content paragraph 2'),
			('protocols/doc2.md', 'Title 2', 'Lexical only document content');
	`)
	dbInit.Close()

	lexDB, _ := lexical.NewDB(dbPath, 0.0)

	almClient := alm.NewClient(alm.ClientConfig{
		BaseURL:   ts.URL,
		APIKey:    "test-key",
		DefaultWS: "ws-test",
	})

	srv := NewServer(almClient, lexDB, Config{})
	return srv, ts, lexDB
}

func TestServer_SearchMemory(t *testing.T) {
	srv, ts, lexDB := setupTestEnvironment(t)
	defer ts.Close()
	defer lexDB.Close()

	ctx := context.Background()

	// 1. Successful search
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"query": "content",
	}
	res, err := srv.handleSearchMemory(ctx, req)
	if err != nil || res.IsError {
		t.Fatalf("handleSearchMemory failed: %v, res: %+v", err, res)
	}

	// 2. Search with empty query error
	reqEmpty := mcp.CallToolRequest{}
	resEmpty, _ := srv.handleSearchMemory(ctx, reqEmpty)
	if !resEmpty.IsError {
		t.Error("expected error on empty query")
	}

	// 3. Search with token budget
	reqBudget := mcp.CallToolRequest{}
	reqBudget.Params.Arguments = map[string]interface{}{
		"query":            "content",
		"max_token_budget": 5,
	}
	resBudget, err := srv.handleSearchMemory(ctx, reqBudget)
	if err != nil || resBudget.IsError {
		t.Fatalf("handleSearchMemory with budget failed: %v", err)
	}
}

func TestServer_StoreMemory(t *testing.T) {
	srv, ts, lexDB := setupTestEnvironment(t)
	defer ts.Close()
	defer lexDB.Close()

	ctx := context.Background()

	// 1. Success
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"content":   "Fact about system",
		"title":     "fact.md",
		"workspace": "ws-test",
	}
	res, err := srv.handleStoreMemory(ctx, req)
	if err != nil || res.IsError {
		t.Fatalf("handleStoreMemory failed: %v, res: %+v", err, res)
	}

	// 2. Empty content error
	reqEmpty := mcp.CallToolRequest{}
	resEmpty, _ := srv.handleStoreMemory(ctx, reqEmpty)
	if !resEmpty.IsError {
		t.Error("expected error on empty content")
	}
}

func TestServer_GetDocument(t *testing.T) {
	srv, ts, lexDB := setupTestEnvironment(t)
	defer ts.Close()
	defer lexDB.Close()

	ctx := context.Background()

	// 1. Document found
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"doc_id": "protocols/doc1.md"}
	res, err := srv.handleGetDocument(ctx, req)
	if err != nil || res.IsError {
		t.Fatalf("handleGetDocument failed: %v", err)
	}

	// 2. Empty doc_id error
	reqEmpty := mcp.CallToolRequest{}
	resEmpty, _ := srv.handleGetDocument(ctx, reqEmpty)
	if !resEmpty.IsError {
		t.Error("expected error on empty doc_id")
	}

	// 3. Unavailable lexical DB
	srvNilDB := NewServer(srv.almClient, nil, Config{})
	resNil, _ := srvNilDB.handleGetDocument(ctx, req)
	if resNil.IsError {
		t.Error("expected graceful non-error report on unavailable DB")
	}
}

func TestServer_GatewayHealth(t *testing.T) {
	srv, ts, lexDB := setupTestEnvironment(t)
	defer ts.Close()
	defer lexDB.Close()

	ctx := context.Background()

	res, err := srv.handleGatewayHealth(ctx, mcp.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("handleGatewayHealth failed: %v", err)
	}
}
