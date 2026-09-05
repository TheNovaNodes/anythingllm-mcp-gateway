package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/alm"
	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/fusion"
	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/lexical"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"golang.org/x/sync/errgroup"
)

// Server coordinates the MCP gateway tools.
type Server struct {
	mcpServer      *mcpserver.MCPServer
	almClient      *alm.Client
	lexDB          *lexical.DB
	defaultTopK    int
	maxTopK        int
	vectorScoreThr float64
	rrfK           int
}

// Config holds configuration parameters for the gateway server.
type Config struct {
	DefaultTopK    int
	MaxTopK        int
	VectorScoreThr float64
	RRFK           int
}

// NewServer initializes a new MCP Server with the 4 core search & memory tools.
func NewServer(almClient *alm.Client, lexDB *lexical.DB, cfg Config) *Server {
	if cfg.DefaultTopK <= 0 {
		cfg.DefaultTopK = 5
	}
	if cfg.MaxTopK <= 0 {
		cfg.MaxTopK = 25
	}
	if cfg.VectorScoreThr <= 0 {
		cfg.VectorScoreThr = 0.13
	}
	if cfg.RRFK <= 0 {
		cfg.RRFK = 60
	}

	mcpSrv := mcpserver.NewMCPServer(
		"anythingllm-mcp-gateway",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
	)

	s := &Server{
		mcpServer:      mcpSrv,
		almClient:      almClient,
		lexDB:          lexDB,
		defaultTopK:    cfg.DefaultTopK,
		maxTopK:        cfg.MaxTopK,
		vectorScoreThr: cfg.VectorScoreThr,
		rrfK:           cfg.RRFK,
	}

	s.registerTools()
	return s
}

// MCPServer returns the underlying MCP server.
func (s *Server) MCPServer() *mcpserver.MCPServer {
	return s.mcpServer
}

func (s *Server) registerTools() {
	// 1. search_memory
	s.mcpServer.AddTool(
		mcp.NewTool("search_memory",
			mcp.WithDescription("Hybrid semantic search across laboratory memory (vector + lexical FTS5, RRF fusion)."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query in natural language or keywords")),
			mcp.WithNumber("top_k", mcp.Description("Number of results to return (default: 5, max: 25)")),
			mcp.WithString("workspace", mcp.Description("Optional workspace slug to restrict vector search to")),
			mcp.WithBoolean("expand_context", mcp.Description("Whether to expand matched chunks with surrounding document paragraphs (default: true)")),
			mcp.WithNumber("max_token_budget", mcp.Description("Optional token budget limit to trim response cleanly")),
			mcp.WithString("tier", mcp.Description("Optional memory tier filter ('episodic', 'semantic', 'procedural')")),
		),
		s.handleSearchMemory,
	)

	// 2. store_memory
	s.mcpServer.AddTool(
		mcp.NewTool("store_memory",
			mcp.WithDescription("Store new facts, notes, and architectural decisions into semantic memory."),
			mcp.WithString("content", mcp.Required(), mcp.Description("Text content of the fact or note to store")),
			mcp.WithString("title", mcp.Description("Title/filename for the memory (e.g. 'auth_architecture.md')")),
			mcp.WithString("workspace", mcp.Description("Target workspace slug (default: configured default workspace)")),
			mcp.WithString("tier", mcp.Description("Memory hierarchy tier ('episodic', 'semantic', 'procedural')")),
		),
		s.handleStoreMemory,
	)

	// 3. get_document
	s.mcpServer.AddTool(
		mcp.NewTool("get_document",
			mcp.WithDescription("Fetch the full raw text content of a document by doc_id or path."),
			mcp.WithString("doc_id", mcp.Required(), mcp.Description("Document path or canonical ID")),
			mcp.WithNumber("max_chars", mcp.Description("Maximum characters to retrieve (default: 20000)")),
		),
		s.handleGetDocument,
	)

	// 4. gateway_health
	s.mcpServer.AddTool(
		mcp.NewTool("gateway_health",
			mcp.WithDescription("Check health and operational state of vector and lexical retrieval layers."),
		),
		s.handleGatewayHealth,
	)
}

func (s *Server) handleSearchMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	t0 := time.Now()

	query, err := req.RequireString("query")
	if err != nil || strings.TrimSpace(query) == "" {
		return mcp.NewToolResultError("Argument 'query' is required and cannot be empty"), nil
	}
	cleanQuery := strings.TrimSpace(query)

	topK := req.GetInt("top_k", s.defaultTopK)
	if topK <= 0 {
		topK = s.defaultTopK
	}
	if topK > s.maxTopK {
		topK = s.maxTopK
	}

	workspace := req.GetString("workspace", "")
	expandCtx := req.GetBool("expand_context", true)
	tokenBudget := req.GetInt("max_token_budget", 0)

	// Bounded execution timeout: max 6 seconds total
	searchCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	var vectorHits []alm.VectorHit
	var lexicalHits []lexical.LexicalHit
	var vecErr, lexErr error

	var g errgroup.Group

	// 1. Vector Search Layer
	g.Go(func() error {
		var slugs []string
		if workspace != "" {
			slugs = []string{workspace}
		} else {
			discovered, err := s.almClient.GetWorkspaceSlugs(searchCtx)
			if err != nil {
				vecErr = err
				return nil
			}
			slugs = discovered
		}

		if len(slugs) == 0 {
			return nil
		}

		var hitMu sync.Mutex
		var vecGroup errgroup.Group

		for _, slug := range slugs {
			sSlug := slug
			vecGroup.Go(func() error {
				hits, err := s.almClient.SearchWorkspaceVectors(searchCtx, sSlug, cleanQuery, topK*2, s.vectorScoreThr)
				if err != nil {
					return nil // individual workspace error does not abort group
				}
				hitMu.Lock()
				vectorHits = append(vectorHits, hits...)
				hitMu.Unlock()
				return nil
			})
		}
		_ = vecGroup.Wait()
		return nil
	})

	// 2. Lexical Search Layer
	g.Go(func() error {
		if s.lexDB != nil && s.lexDB.IsAvailable() {
			hits, err := s.lexDB.Search(searchCtx, cleanQuery, topK*2)
			if err != nil {
				lexErr = err
			} else {
				lexicalHits = hits
			}
		}
		return nil
	})

	_ = g.Wait()

	// 3. Fusion & Deduplication
	merged := fusion.RRFMerge(vectorHits, lexicalHits, topK, s.rrfK)

	// 4. Context Assembly
	if expandCtx && s.lexDB != nil && s.lexDB.IsAvailable() {
		merged = fusion.ExpandContext(searchCtx, s.lexDB, merged, 4000)
	}

	// 5. Token Budgeting
	finalResults, totalTokens := fusion.TrimToTokenBudget(merged, tokenBudget)

	degraded := (vecErr != nil) || (lexErr != nil) || (!s.lexDB.IsAvailable())

	output := map[string]interface{}{
		"query":                  cleanQuery,
		"count":                  len(finalResults),
		"results":                finalResults,
		"degraded":               degraded,
		"layers":                 map[string]int{"vector": len(vectorHits), "lexical": len(lexicalHits)},
		"total_estimated_tokens": totalTokens,
		"latency_ms":             time.Since(t0).Milliseconds(),
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal search results: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleStoreMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	content, err := req.RequireString("content")
	if err != nil || strings.TrimSpace(content) == "" {
		return mcp.NewToolResultError("Argument 'content' is required and cannot be empty"), nil
	}

	title := req.GetString("title", "")
	workspace := req.GetString("workspace", "")
	tier := req.GetString("tier", "semantic")

	var metadata map[string]interface{}
	if argsMap, ok := req.Params.Arguments.(map[string]interface{}); ok {
		if m, ok := argsMap["metadata"].(map[string]interface{}); ok {
			metadata = m
		}
	}

	storeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	res, err := s.almClient.StoreMemory(storeCtx, content, title, workspace, tier, metadata)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to store memory: %v", err)), nil
	}

	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format store response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleGetDocument(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	docID, err := req.RequireString("doc_id")
	if err != nil || strings.TrimSpace(docID) == "" {
		return mcp.NewToolResultError("Argument 'doc_id' is required and cannot be empty"), nil
	}

	maxChars := req.GetInt("max_chars", 20000)
	if maxChars <= 0 {
		maxChars = 20000
	}

	docCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if s.lexDB == nil || !s.lexDB.IsAvailable() {
		res := map[string]interface{}{
			"doc_id": docID,
			"found":  false,
			"error":  "lexical database unavailable",
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	}

	doc, err := s.lexDB.GetDocument(docCtx, docID, maxChars)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Error retrieving document '%s': %v", docID, err)), nil
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format document response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleGatewayHealth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	healthCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	vectorReachable := false
	workspaces, err := s.almClient.GetWorkspaceSlugs(healthCtx)
	if err == nil {
		vectorReachable = true
	}

	lexicalReachable := s.lexDB != nil && s.lexDB.IsAvailable()

	health := map[string]interface{}{
		"ok":       vectorReachable,
		"degraded": !vectorReachable || !lexicalReachable,
		"vector_layer": map[string]interface{}{
			"reachable":  vectorReachable,
			"workspaces": len(workspaces),
		},
		"lexical_layer": map[string]interface{}{
			"reachable": lexicalReachable,
		},
	}

	data, _ := json.MarshalIndent(health, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}
