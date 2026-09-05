package alm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

var (
	ErrNonJSONResponse = errors.New("non-JSON response from AnythingLLM (possibly SPA HTML fallback)")
	ErrUnauthorized    = errors.New("unauthorized (invalid or missing API key)")
	ErrNotFound        = errors.New("resource not found")
)

var (
	docMetaRegex   = regexp.MustCompile(`(?is)<document_metadata>.*?</document_metadata>`)
	chunkPrefRegex = regexp.MustCompile(`(?i)^\s*(passage|query|search_document|search_query)\s*:\s*`)
)

// CleanChunk removes internal AnythingLLM wrappers and metadata tags.
func CleanChunk(text string) string {
	if text == "" {
		return ""
	}
	t := docMetaRegex.ReplaceAllString(text, "")
	t = chunkPrefRegex.ReplaceAllString(t, "")
	t = strings.ReplaceAll(t, "\u2026", " ")
	return strings.TrimSpace(t)
}

// Client manages communication with AnythingLLM REST API.
type Client struct {
	baseURL      string
	apiKey       string
	tokenFile    string
	httpClient   *http.Client
	maxInflight  int64
	sem          *semaphore.Weighted
	defaultWS    string
	mapFile      string
	tokenLock    sync.RWMutex
	cachedToken  string
	tokenModTime time.Time
}

// NormalizeBaseURL ensures URL has /api/v1 prefix and no trailing slash.
func NormalizeBaseURL(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	u = strings.TrimRight(u, "/")
	if u == "" {
		return "http://127.0.0.1:3002/api/v1"
	}
	if !strings.HasSuffix(u, "/api/v1") && !strings.HasSuffix(u, "/v1") {
		if strings.HasSuffix(u, "/api") {
			u = u + "/v1"
		} else {
			u = u + "/api/v1"
		}
	}
	return u
}

// ClientConfig holds configuration for the AnythingLLM client.
type ClientConfig struct {
	BaseURL     string
	APIKey      string
	TokenFile   string
	DefaultWS   string
	MapFile     string
	MaxInflight int64
	Timeout     time.Duration
}

// NewClient initializes an AnythingLLM client with connection pooling and semaphore throttle.
func NewClient(cfg ClientConfig) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.MaxInflight <= 0 {
		cfg.MaxInflight = 4
	}
	if cfg.DefaultWS == "" {
		cfg.DefaultWS = "default"
	}

	return &Client{
		baseURL:     NormalizeBaseURL(cfg.BaseURL),
		apiKey:      strings.TrimSpace(cfg.APIKey),
		tokenFile:   strings.TrimSpace(cfg.TokenFile),
		defaultWS:   cfg.DefaultWS,
		mapFile:     cfg.MapFile,
		maxInflight: cfg.MaxInflight,
		sem:         semaphore.NewWeighted(cfg.MaxInflight),
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}
}

// BaseURL returns the normalized base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// DefaultWorkspace returns the configured fallback workspace.
func (c *Client) DefaultWorkspace() string {
	return c.defaultWS
}

// getToken retrieves the Bearer token dynamically (from env/file with reload on mtime change).
func (c *Client) getToken() string {
	c.tokenLock.RLock()
	if c.cachedToken != "" && c.tokenFile == "" {
		tok := c.cachedToken
		c.tokenLock.RUnlock()
		return tok
	}
	c.tokenLock.RUnlock()

	c.tokenLock.Lock()
	defer c.tokenLock.Unlock()

	// 1. Direct API key
	if c.apiKey != "" {
		c.cachedToken = c.apiKey
		return c.cachedToken
	}

	// 2. Token file
	if c.tokenFile != "" {
		if fi, err := os.Stat(c.tokenFile); err == nil {
			if fi.ModTime().After(c.tokenModTime) || c.cachedToken == "" {
				data, err := os.ReadFile(c.tokenFile)
				if err == nil {
					c.cachedToken = strings.TrimSpace(string(data))
					c.tokenModTime = fi.ModTime()
				}
			}
		}
	}

	if c.cachedToken == "" {
		c.cachedToken = os.Getenv("ANYTHINGLLM_API_KEY")
		if c.cachedToken == "" {
			c.cachedToken = os.Getenv("MG_API_KEY")
		}
	}

	return c.cachedToken
}

// newRequest creates an HTTP request with auth and JSON headers.
func (c *Client) newRequest(ctx context.Context, method, path string, body interface{}) (*http.Request, error) {
	fullURL := fmt.Sprintf("%s%s", c.baseURL, path)
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	token := c.getToken()
	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	return req, nil
}

// doExecute executes an HTTP request with SPA guard.
func (c *Client) doExecute(req *http.Request) ([]byte, int, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("network error connecting to AnythingLLM at %s: %w", req.URL.String(), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	trimmed := strings.TrimSpace(string(body))

	if strings.Contains(contentType, "text/html") || strings.HasPrefix(trimmed, "<!doctype") || strings.HasPrefix(trimmed, "<html") {
		return body, resp.StatusCode, fmt.Errorf("%w: HTTP %d returned HTML instead of JSON", ErrNonJSONResponse, resp.StatusCode)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return body, resp.StatusCode, fmt.Errorf("%w: HTTP %d: %s", ErrUnauthorized, resp.StatusCode, trimmed)
	}

	if resp.StatusCode == http.StatusNotFound {
		return body, resp.StatusCode, fmt.Errorf("%w: HTTP 404 for %s", ErrNotFound, req.URL.Path)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, resp.StatusCode, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, trimmed)
	}

	return body, resp.StatusCode, nil
}

// SearchWorkspaceVectors performs vector search on a single workspace.
func (c *Client) SearchWorkspaceVectors(ctx context.Context, slug, query string, topK int, threshold float64) ([]VectorHit, error) {
	if err := c.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("vector search concurrency queue timeout: %w", err)
	}
	defer c.sem.Release(1)

	path := fmt.Sprintf("/workspace/%s/vector-search", url.PathEscape(slug))
	payload := map[string]interface{}{
		"query":          query,
		"topN":           topK,
		"scoreThreshold": threshold,
	}

	req, err := c.newRequest(ctx, http.MethodPost, path, payload)
	if err != nil {
		return nil, err
	}

	body, _, err := c.doExecute(req)
	if err != nil {
		return nil, err
	}

	var envelope VectorSearchResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse vector search response: %w", err)
	}

	hits := make([]VectorHit, 0, len(envelope.Results))
	for _, r := range envelope.Results {
		title := "?"
		docID := r.ID
		if r.Metadata != nil {
			if t, ok := r.Metadata["title"].(string); ok && t != "" {
				title = t
			} else if ds, ok := r.Metadata["docSource"].(string); ok && ds != "" {
				title = ds
			}
			for _, k := range []string{"docpath", "path", "url", "chunkSource"} {
				if v, ok := r.Metadata[k].(string); ok && v != "" {
					docID = strings.TrimPrefix(strings.ReplaceAll(v, "file://", ""), "/")
					break
				}
			}
		}
		if docID == "" {
			docID = title
		}

		hits = append(hits, VectorHit{
			DocID:       docID,
			Title:       title,
			Workspace:   slug,
			Text:        CleanChunk(r.Text),
			VectorScore: r.Score,
		})
	}

	return hits, nil
}

// GetWorkspaceSlugs retrieves active workspace slugs (from local map file or API).
func (c *Client) GetWorkspaceSlugs(ctx context.Context) ([]string, error) {
	if c.mapFile != "" {
		if data, err := os.ReadFile(c.mapFile); err == nil {
			var parsed map[string]interface{}
			if err := json.Unmarshal(data, &parsed); err == nil && len(parsed) > 0 {
				slugs := make([]string, 0, len(parsed))
				for k := range parsed {
					slugs = append(slugs, k)
				}
				return slugs, nil
			}
		}
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/workspaces", nil)
	if err != nil {
		return nil, err
	}

	body, _, err := c.doExecute(req)
	if err != nil {
		return nil, err
	}

	var envelope WorkspacesEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse workspaces list: %w", err)
	}

	slugs := make([]string, 0, len(envelope.Workspaces))
	for _, ws := range envelope.Workspaces {
		if ws.Slug != "" {
			slugs = append(slugs, ws.Slug)
		}
	}

	return slugs, nil
}

// StoreMemory uploads raw text to AnythingLLM and synchronizes embeddings to the workspace.
func (c *Client) StoreMemory(ctx context.Context, content, title, workspace, tier string, metadata map[string]interface{}) (*StoreResult, error) {
	cleanContent := strings.TrimSpace(content)
	if cleanContent == "" {
		return nil, errors.New("content cannot be empty")
	}

	targetWS := strings.TrimSpace(workspace)
	if targetWS == "" {
		targetWS = c.defaultWS
	}

	cleanTitle := strings.TrimSpace(title)
	if cleanTitle == "" {
		cleanTitle = fmt.Sprintf("memory_%d.txt", time.Now().Unix())
	}

	meta := make(map[string]interface{})
	for k, v := range metadata {
		meta[k] = v
	}
	meta["title"] = cleanTitle
	if tier == "" {
		tier = "semantic"
	}
	meta["tier"] = tier
	meta["stored_at"] = time.Now().UTC().Format(time.RFC3339)

	// 1. Upload raw text
	uploadReq, err := c.newRequest(ctx, http.MethodPost, "/document/raw-text", map[string]interface{}{
		"textContent": cleanContent,
		"metadata":    meta,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build upload request: %w", err)
	}

	uploadBody, _, err := c.doExecute(uploadReq)
	if err != nil {
		return nil, fmt.Errorf("document upload failed: %w", err)
	}

	var uploadResp RawUploadResponse
	if err := json.Unmarshal(uploadBody, &uploadResp); err != nil {
		return nil, fmt.Errorf("failed to parse upload response: %w", err)
	}

	if len(uploadResp.Documents) == 0 {
		return nil, fmt.Errorf("no document returned by AnythingLLM upload: %s", string(uploadBody))
	}

	doc := uploadResp.Documents[0]

	// 2. Update workspace embeddings
	embedPath := fmt.Sprintf("/workspace/%s/update-embeddings", url.PathEscape(targetWS))
	embedReq, err := c.newRequest(ctx, http.MethodPost, embedPath, map[string]interface{}{
		"adds": []string{doc.Location},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build embedding request: %w", err)
	}

	_, _, err = c.doExecute(embedReq)
	if err != nil {
		return nil, fmt.Errorf("failed to embed document into workspace '%s': %w", targetWS, err)
	}

	return &StoreResult{
		Success:   true,
		DocID:     doc.ID,
		Title:     cleanTitle,
		Location:  doc.Location,
		Workspace: targetWS,
		Tier:      tier,
	}, nil
}
