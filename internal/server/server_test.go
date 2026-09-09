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
				Workspaces: []alm.Workspace{
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
		CREATE VIRTUAL TABLE docs_fts USING fts5(path, title, workspace UNINDEXED, content);
		INSERT INTO docs_fts(path, title, workspace, content) VALUES
			('protocols/doc1.md', 'Title 1', 'thenovanodes-vault', 'Full content paragraph 1\n\nFull content paragraph 2'),
			('protocols/doc2.md', 'Title 2', 'thenovanodes-vault', 'Lexical only document content'),
			('protocols/secret.md', 'Secret Title', 'thedoctormes-hue-doctorm-unify-protocol', 'Top secret document content');
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

	// 4. Access Control (BAC) - allowed orgs filter
	srvScoped := NewServer(srv.almClient, lexDB, Config{
		AllowedOrgs: []string{"thenovanodes"},
	})
	// protocols/doc1.md belongs to thenovanodes-vault, so allowed
	resAllowed, errAllowed := srvScoped.handleGetDocument(ctx, req)
	if errAllowed != nil || resAllowed.IsError {
		t.Errorf("expected access allowed for thenovanodes doc: %+v", resAllowed)
	}

	// Direct request for unauthorized document by doc_id
	reqSecret := mcp.CallToolRequest{}
	reqSecret.Params.Arguments = map[string]interface{}{
		"doc_id": "protocols/secret.md",
	}
	resSecret, _ := srvScoped.handleGetDocument(ctx, reqSecret)
	if !resSecret.IsError {
		t.Error("expected access denied error when retrieving document from unauthorized org")
	}

	// Request with explicit unauthorized workspace parameter
	reqUnauthorized := mcp.CallToolRequest{}
	reqUnauthorized.Params.Arguments = map[string]interface{}{
		"doc_id":    "protocols/doc1.md",
		"workspace": "thedoctormes-hue-doctorm-unify-protocol",
	}
	resUnauthorized, _ := srvScoped.handleGetDocument(ctx, reqUnauthorized)
	if !resUnauthorized.IsError {
		t.Error("expected access denied error for unauthorized workspace parameter")
	}
}

func TestServer_SearchMemory_BAC(t *testing.T) {
	srv, ts, lexDB := setupTestEnvironment(t)
	defer ts.Close()
	defer lexDB.Close()

	ctx := context.Background()

	srvScoped := NewServer(srv.almClient, lexDB, Config{
		AllowedOrgs: []string{"thenovanodes"},
	})

	// Querying unauthorized workspace directly should fail fast
	reqForbidden := mcp.CallToolRequest{}
	reqForbidden.Params.Arguments = map[string]interface{}{
		"query":     "test query",
		"workspace": "thedoctormes-hue-doctorm-unify-protocol",
	}
	resForbidden, _ := srvScoped.handleSearchMemory(ctx, reqForbidden)
	if !resForbidden.IsError {
		t.Error("expected access denied error when searching forbidden workspace")
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

func TestServer_ToolsPruned(t *testing.T) {
	srv, ts, lexDB := setupTestEnvironment(t)
	defer ts.Close()
	defer lexDB.Close()

	// Verify that store_memory is NOT registered on the MCP server
	// Only search_memory, get_document, and gateway_health should be exposed
	mcpSrv := srv.MCPServer()
	if mcpSrv == nil {
		t.Fatal("expected non-nil MCPServer")
	}

	tools := mcpSrv.ListTools()
	if len(tools) != 3 {
		t.Fatalf("expected exactly 3 exposed tools, got %d: %+v", len(tools), tools)
	}

	expected := []string{"search_memory", "get_document", "gateway_health"}
	for _, exp := range expected {
		if _, ok := tools[exp]; !ok {
			t.Errorf("expected tool %q to be registered", exp)
		}
	}

	if _, ok := tools["store_memory"]; ok {
		t.Errorf("forbidden tool 'store_memory' is still registered on MCPServer")
	}
}

