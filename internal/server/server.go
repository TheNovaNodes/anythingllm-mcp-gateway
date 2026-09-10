package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/alm"
	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/fusion"
	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/lexical"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"golang.org/x/sync/errgroup"
)

// Server coordinates the MCP gateway tools.
type Server struct {
	mcpServer        *mcpserver.MCPServer
	almClient        *alm.Client
	lexDB            *lexical.DB
	defaultTopK      int
	maxTopK          int
	vectorScoreThr   float64
	rrfK             int
	allowedOrgs      []string
	minPureVectorSim float64
	vectorWeight     float64
	lexicalWeight    float64
	synergyBonus     float64
}

// Config holds configuration parameters for the gateway server.
type Config struct {
	DefaultTopK      int
	MaxTopK          int
	VectorScoreThr   float64
	RRFK             int
	AllowedOrgs      []string
	MinPureVectorSim float64
	VectorWeight     float64
	LexicalWeight    float64
	SynergyBonus     float64
}

// NewServer initializes a new MCP Server with the 3 core search & memory tools.
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

	minPureVectorSim := cfg.MinPureVectorSim
	if minPureVectorSim <= 0 {
		if envMin := os.Getenv("MG_MIN_VECTOR_SIMILARITY"); envMin != "" {
			if parsed, err := strconv.ParseFloat(envMin, 64); err == nil && parsed > 0 {
				minPureVectorSim = parsed
			}
		}
	}
	if minPureVectorSim <= 0 {
		minPureVectorSim = fusion.DefaultMinPureVectorSimilarity
	}

	allowedOrgs := cfg.AllowedOrgs
	if len(allowedOrgs) == 0 {
		if envOrgs := os.Getenv("MG_ALLOWED_ORGS"); envOrgs != "" {
			for _, o := range strings.Split(envOrgs, ",") {
				if oClean := strings.TrimSpace(o); oClean != "" {
					allowedOrgs = append(allowedOrgs, oClean)
				}
			}
		}
	}

	vecWeight := cfg.VectorWeight
	if vecWeight <= 0 {
		if envW := os.Getenv("MG_VECTOR_WEIGHT"); envW != "" {
			if parsed, err := strconv.ParseFloat(envW, 64); err == nil && parsed > 0 {
				vecWeight = parsed
			}
		}
	}
	if vecWeight <= 0 {
		vecWeight = 1.0
	}

	lexWeight := cfg.LexicalWeight
	if lexWeight <= 0 {
		if envW := os.Getenv("MG_LEXICAL_WEIGHT"); envW != "" {
			if parsed, err := strconv.ParseFloat(envW, 64); err == nil && parsed > 0 {
				lexWeight = parsed
			}
		}
	}
	if lexWeight <= 0 {
		lexWeight = 1.0
	}

	synergy := cfg.SynergyBonus
	if synergy <= 0 {
		if envS := os.Getenv("MG_HYBRID_SYNERGY"); envS != "" {
			if parsed, err := strconv.ParseFloat(envS, 64); err == nil && parsed >= 0 {
				synergy = parsed
			}
		}
	}
	if synergy <= 0 {
		synergy = 0.25
	}

	mcpSrv := mcpserver.NewMCPServer(
		"anythingllm-mcp-gateway",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
	)

	s := &Server{
		mcpServer:        mcpSrv,
		almClient:        almClient,
		lexDB:            lexDB,
		defaultTopK:      cfg.DefaultTopK,
		maxTopK:          cfg.MaxTopK,
		vectorScoreThr:   cfg.VectorScoreThr,
		rrfK:             cfg.RRFK,
		allowedOrgs:      allowedOrgs,
		minPureVectorSim: minPureVectorSim,
		vectorWeight:     vecWeight,
		lexicalWeight:    lexWeight,
		synergyBonus:     synergy,
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
			mcp.WithNumber("vector_weight", mcp.Description("Weight multiplier for vector retrieval layer in RRF (default: 1.0)")),
			mcp.WithNumber("lexical_weight", mcp.Description("Weight multiplier for lexical FTS5 layer in RRF (default: 1.0)")),
			mcp.WithNumber("min_vector_similarity", mcp.Description("Cosine similarity cutoff for pure-vector hits (default: 0.55)")),
			mcp.WithNumber("max_context_chars", mcp.Description("Maximum characters for paragraph context expansion (default: 4000)")),
			mcp.WithString("group_by", mcp.Description("Optional grouping strategy ('chunk' or 'document', default: 'chunk')")),
		),
		s.handleSearchMemory,
	)

	// 2. get_document
	s.mcpServer.AddTool(
		mcp.NewTool("get_document",
			mcp.WithDescription("Fetch the full raw text content of a document by doc_id or path."),
			mcp.WithString("doc_id", mcp.Required(), mcp.Description("Document path or canonical ID")),
			mcp.WithString("workspace", mcp.Description("Optional workspace slug to scope document lookup")),
			mcp.WithNumber("max_chars", mcp.Description("Maximum characters to retrieve (default: 20000)")),
		),
		s.handleGetDocument,
	)

	// 3. gateway_health
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
	tierFilter := strings.ToLower(strings.TrimSpace(req.GetString("tier", "")))

	vectorWeight := req.GetFloat("vector_weight", s.vectorWeight)
	if vectorWeight <= 0 {
		vectorWeight = s.vectorWeight
	}
	lexicalWeight := req.GetFloat("lexical_weight", s.lexicalWeight)
	if lexicalWeight <= 0 {
		lexicalWeight = s.lexicalWeight
	}
	minPureVectorSim := req.GetFloat("min_vector_similarity", s.minPureVectorSim)
	if minPureVectorSim <= 0 {
		minPureVectorSim = s.minPureVectorSim
	}
	maxContextChars := req.GetInt("max_context_chars", 4000)
	if maxContextChars <= 0 {
		maxContextChars = 4000
	}
	groupBy := strings.ToLower(strings.TrimSpace(req.GetString("group_by", "chunk")))

	// Bounded execution timeout: max 6 seconds total
	searchCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	var vectorHits []alm.VectorHit
	var lexicalHits []lexical.LexicalHit
	var vecErr, lexErr error

	// 1. Lexical Search Layer (Runs first to discover candidate workspaces - FTS5-First Routing)
	if s.lexDB != nil && s.lexDB.IsAvailable() {
		hits, err := s.lexDB.Search(searchCtx, cleanQuery, workspace, topK*3)
		if err != nil {
			lexErr = err
		} else {
			lexicalHits = hits
		}
	}

	// 2. Vector Search Layer with FTS5-First workspace candidate pruning and query keyword slug matching
	var slugs []string
	if workspace != "" {
		if len(s.allowedOrgs) > 0 {
			org := fusion.DeriveOrgFromWorkspace(workspace, "")
			if !fusion.IsOrgAllowed(org, s.allowedOrgs) {
				return mcp.NewToolResultError(fmt.Sprintf("Access denied: workspace '%s' belongs to unauthorized organization", workspace)), nil
			}
		}
		slugs = []string{workspace}
	} else {
		seenWS := make(map[string]bool)

		// A. Extract distinct workspaces from top lexical matches
		for _, h := range lexicalHits {
			wsClean := strings.TrimSpace(h.Workspace)
			if wsClean != "" && !seenWS[wsClean] {
				seenWS[wsClean] = true
				slugs = append(slugs, wsClean)
				if len(slugs) >= 5 { // cap at top 5 most relevant workspaces from lexical
					break
				}
			}
		}

		// B. Match query keywords directly against known workspace slugs
		allSlugs, err := s.almClient.GetWorkspaceSlugs(searchCtx)
		if err != nil {
			vecErr = err
		} else if len(allSlugs) > 0 {
			qWords := strings.FieldsFunc(strings.ToLower(cleanQuery), func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsNumber(r)
			})
			for _, w := range qWords {
				if utf8.RuneCountInString(w) < 3 || w == "mcp" || w == "the" || w == "and" {
					continue
				}
				for _, ws := range allSlugs {
					wsLower := strings.ToLower(ws)
					if strings.Contains(wsLower, w) && !seenWS[ws] {
						seenWS[ws] = true
						slugs = append(slugs, ws)
						if len(slugs) >= 6 {
							break
						}
					}
				}
				if len(slugs) >= 6 {
					break
				}
			}

			// Also include default workspace if configured and not already included
			if defWS := s.almClient.DefaultWorkspace(); defWS != "" && !seenWS[defWS] && defWS != "default" && len(slugs) < 6 {
				seenWS[defWS] = true
				slugs = append(slugs, defWS)
			}

			// C. Fallback if still no candidate workspaces discovered
			if len(slugs) == 0 {
				if len(allSlugs) > 6 {
					slugs = allSlugs[:6]
				} else {
					slugs = allSlugs
				}
			}
		}

		// Filter candidate workspaces by allowed orgs if configured
		if len(s.allowedOrgs) > 0 {
			var filteredSlugs []string
			for _, slug := range slugs {
				org := fusion.DeriveOrgFromWorkspace(slug, "")
				if fusion.IsOrgAllowed(org, s.allowedOrgs) {
					filteredSlugs = append(filteredSlugs, slug)
				}
			}
			slugs = filteredSlugs
		}
	}

	// 3. Parallel Vector Search across candidate workspaces
	if len(slugs) > 0 {
		var hitMu sync.Mutex
		var vecGroup errgroup.Group
		var errCount int
		var errMu sync.Mutex

		for _, slug := range slugs {
			sSlug := slug
			vecGroup.Go(func() error {
				hits, err := s.almClient.SearchWorkspaceVectors(searchCtx, sSlug, cleanQuery, topK*2, s.vectorScoreThr)
				if err != nil {
					errMu.Lock()
					errCount++
					errMu.Unlock()
					return nil // individual workspace error does not abort group
				}
				hitMu.Lock()
				vectorHits = append(vectorHits, hits...)
				hitMu.Unlock()
				return nil
			})
		}
		_ = vecGroup.Wait()

		if errCount == len(slugs) && vecErr == nil {
			vecErr = fmt.Errorf("all %d candidate workspaces failed vector search", len(slugs))
		}

		// Sort vectorHits descending by VectorScore before RRF rank assignment
		sort.Slice(vectorHits, func(i, j int) bool {
			return vectorHits[i].VectorScore > vectorHits[j].VectorScore
		})
	}

	// 3. Multi-tenant Org Scoping Filter
	if len(s.allowedOrgs) > 0 {
		vectorHits = fusion.FilterVectorHitsByOrg(vectorHits, s.allowedOrgs)
		lexicalHits = fusion.FilterLexicalHitsByOrg(lexicalHits, s.allowedOrgs)
	}

	// 4. Fusion & Deduplication with workspace prioritization boost, synergy bonus, and pure-vector similarity cutoff
	merged := fusion.RRFMergeWithOptions(vectorHits, lexicalHits, fusion.RRFOptions{
		TopK:             topK,
		RRFK:             s.rrfK,
		MinPureVectorSim: minPureVectorSim,
		VectorWeight:     vectorWeight,
		LexicalWeight:    lexicalWeight,
		SynergyBonus:     s.synergyBonus,
		Query:            cleanQuery,
	})

	// 5. Tier Filtering (#10)
	if tierFilter != "" {
		var tierFiltered []fusion.SearchResultItem
		for _, it := range merged {
			if strings.EqualFold(it.Tier, tierFilter) || it.Tier == "" {
				tierFiltered = append(tierFiltered, it)
			}
		}
		merged = tierFiltered
	}

	// 5.5 Document-level Grouping (#39)
	if groupBy == "document" {
		merged = fusion.GroupByDocument(merged)
	}

	// 6. Context Assembly
	if expandCtx && s.lexDB != nil && s.lexDB.IsAvailable() {
		merged = fusion.ExpandContext(searchCtx, s.lexDB, merged, maxContextChars)
	}

	// 7. Token Budgeting
	finalResults, totalTokens := fusion.TrimToTokenBudget(merged, tokenBudget)
	if finalResults == nil {
		finalResults = []fusion.SearchResultItem{}
	}

	degraded := (vecErr != nil) || (lexErr != nil) || (!s.lexDB.IsAvailable())

	output := map[string]interface{}{
		"query":                  cleanQuery,
		"count":                  len(finalResults),
		"results":                finalResults,
		"degraded":               degraded,
		"layers":                 map[string]int{"vector": len(vectorHits), "lexical": len(lexicalHits)},
		"weights":                map[string]float64{"vector": vectorWeight, "lexical": lexicalWeight, "synergy": s.synergyBonus},
		"total_estimated_tokens": totalTokens,
		"latency_ms":             time.Since(t0).Milliseconds(),
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal search results: %v", err)), nil
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

	workspace := req.GetString("workspace", "")
	if workspace != "" && len(s.allowedOrgs) > 0 {
		reqOrg := fusion.DeriveOrgFromWorkspace(workspace, "")
		if !fusion.IsOrgAllowed(reqOrg, s.allowedOrgs) {
			return mcp.NewToolResultError(fmt.Sprintf("Access denied: workspace '%s' belongs to unauthorized organization", workspace)), nil
		}
	}

	doc, err := s.lexDB.GetDocument(docCtx, docID, workspace, maxChars)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Error retrieving document '%s': %v", docID, err)), nil
	}

	if doc.Found && len(s.allowedOrgs) > 0 {
		effectiveWS := doc.Workspace
		if effectiveWS == "" {
			effectiveWS = workspace
		}
		org := fusion.DeriveOrgFromWorkspace(effectiveWS, doc.DocID)
		if !fusion.IsOrgAllowed(org, s.allowedOrgs) {
			return mcp.NewToolResultError(fmt.Sprintf("Access denied: document '%s' belongs to unauthorized organization", docID)), nil
		}
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
	vectorFunctional := false
	workspaces, err := s.almClient.GetWorkspaceSlugs(healthCtx)
	if err == nil {
		vectorReachable = true
		if len(workspaces) > 0 {
			probeWS := workspaces[0]
			if defWS := s.almClient.DefaultWorkspace(); defWS != "" {
				for _, ws := range workspaces {
					if ws == defWS {
						probeWS = defWS
						break
					}
				}
			}
			probeCtx, probeCancel := context.WithTimeout(healthCtx, 1500*time.Millisecond)
			defer probeCancel()
			_, searchErr := s.almClient.SearchWorkspaceVectors(probeCtx, probeWS, "healthcheck probe", 1, 0.0)
			if searchErr == nil {
				vectorFunctional = true
			}
		} else {
			vectorFunctional = true
		}
	}

	lexicalReachable := s.lexDB != nil && s.lexDB.IsAvailable()

	health := map[string]interface{}{
		"ok":       vectorReachable && vectorFunctional && lexicalReachable,
		"degraded": !vectorReachable || !vectorFunctional || !lexicalReachable,
		"vector_layer": map[string]interface{}{
			"reachable":  vectorReachable,
			"functional": vectorFunctional,
			"workspaces": len(workspaces),
		},
		"lexical_layer": map[string]interface{}{
			"reachable": lexicalReachable,
		},
	}

	data, _ := json.MarshalIndent(health, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}
