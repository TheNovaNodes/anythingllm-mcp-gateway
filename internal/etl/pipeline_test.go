package etl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/alm"
)

func TestPipeline_EndToEnd(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "etl_pipeline_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	projectsDir := filepath.Join(tempDir, "projects")
	stateDir := filepath.Join(tempDir, "state")

	// Create test repo hierarchy: projects/org1/repo1/README.md
	repoDir := filepath.Join(projectsDir, "org1", "repo1")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Org1 Repo1 Documentation"), 0644); err != nil {
		t.Fatalf("failed to write readme: %v", err)
	}

	var mu sync.Mutex
	createdWorkspaces := make(map[string]bool)
	uploadedDocs := make([]string, 0)
	embeddedDocs := make(map[string][]string)
	deletedEmbeddings := make(map[string][]string)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/workspaces":
			var wsList []alm.Workspace
			for slug := range createdWorkspaces {
				wsList = append(wsList, alm.Workspace{Slug: slug, Name: slug})
			}
			json.NewEncoder(w).Encode(alm.WorkspacesResponse{Workspaces: wsList})

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/workspace/new":
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			slug := req["name"]
			createdWorkspaces[slug] = true
			json.NewEncoder(w).Encode(alm.WorkspaceResponse{
				Workspace: alm.Workspace{ID: len(createdWorkspaces), Slug: slug, Name: slug},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/document/raw-text":
			var req map[string]interface{}
			json.NewDecoder(r.Body).Decode(&req)
			text := req["textContent"].(string)
			uploadedDocs = append(uploadedDocs, text)
			loc := fmt.Sprintf("custom-documents/doc-%d.json", len(uploadedDocs))
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"documents": []map[string]string{
					{"id": fmt.Sprintf("id-%d", len(uploadedDocs)), "location": loc},
				},
			})

		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/workspace/") && strings.HasSuffix(r.URL.Path, "/update-embeddings"):
			var req map[string][]string
			json.NewDecoder(r.Body).Decode(&req)
			slug := "org1-repo1" // target slug
			if adds, ok := req["adds"]; ok && len(adds) > 0 {
				embeddedDocs[slug] = append(embeddedDocs[slug], adds...)
			}
			if dels, ok := req["deletes"]; ok && len(dels) > 0 {
				deletedEmbeddings[slug] = append(deletedEmbeddings[slug], dels...)
			}
			w.Write([]byte(`{"workspace": {"slug": "org1-repo1"}}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg := PipelineConfig{
		ProjectsDir: projectsDir,
		StateDir:    stateDir,
		BaseURL:     ts.URL,
		APIKey:      "test-token",
		Timeout:     5 * time.Second,
	}
	pipeline := NewPipeline(cfg)

	ctx := context.Background()

	// 1. Initial run: should index the file
	if err := pipeline.Run(ctx); err != nil {
		t.Fatalf("pipeline.Run failed on first pass: %v", err)
	}

	mu.Lock()
	if len(uploadedDocs) != 1 {
		t.Errorf("expected 1 uploaded doc, got %d", len(uploadedDocs))
	}
	if len(embeddedDocs["org1-repo1"]) != 1 {
		t.Errorf("expected 1 embedded doc, got %d", len(embeddedDocs["org1-repo1"]))
	}
	mu.Unlock()

	// Verify lexical.db was created and populated
	lexIdx, err := NewLexicalIndexer(stateDir)
	if err != nil {
		t.Fatalf("failed to open generated lexical.db: %v", err)
	}
	hasLex, err := lexIdx.Has(readmePath)
	if err != nil || !hasLex {
		t.Errorf("expected lexical index to have readmePath, has=%v, err=%v", hasLex, err)
	}
	lexIdx.Close()

	// 2. Second run without file changes: should skip upload
	if err := pipeline.Run(ctx); err != nil {
		t.Fatalf("pipeline.Run failed on second pass: %v", err)
	}

	mu.Lock()
	if len(uploadedDocs) != 1 {
		t.Errorf("expected still 1 uploaded doc, got %d", len(uploadedDocs))
	}
	mu.Unlock()

	// 3. Remove README.md to trigger tombstone deletion
	if err := os.Remove(readmePath); err != nil {
		t.Fatalf("failed to remove readme: %v", err)
	}

	if err := pipeline.Run(ctx); err != nil {
		t.Fatalf("pipeline.Run failed on tombstone pass: %v", err)
	}

	mu.Lock()
	if len(deletedEmbeddings["org1-repo1"]) != 1 {
		t.Errorf("expected 1 deleted embedding for tombstone, got %d", len(deletedEmbeddings["org1-repo1"]))
	}
	mu.Unlock()

	// Verify lexical index deleted the tombstoned file
	lexIdx2, err := NewLexicalIndexer(stateDir)
	if err != nil {
		t.Fatalf("failed to open lexical.db: %v", err)
	}
	defer lexIdx2.Close()
	hasLex2, _ := lexIdx2.Has(readmePath)
	if hasLex2 {
		t.Errorf("expected lexical index to have deleted tombstoned file")
	}
}

func TestPipeline_SnapshotRepoFilter(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "etl_snapshot_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	projectsDir := filepath.Join(tempDir, "projects")
	stateDir := filepath.Join(tempDir, "state")

	// Create snapshot repo: projects/org1/agent-vault-f04/README.md
	f04Dir := filepath.Join(projectsDir, "org1", "agent-vault-f04")
	if err := os.MkdirAll(f04Dir, 0755); err != nil {
		t.Fatalf("failed to create f04 dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f04Dir, "README.md"), []byte("# Snapshot"), 0644); err != nil {
		t.Fatalf("failed to write f04 readme: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no HTTP requests should be made for snapshot repo, got %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := PipelineConfig{
		ProjectsDir: projectsDir,
		StateDir:    stateDir,
		BaseURL:     ts.URL,
		APIKey:      "dummy",
		Timeout:     5 * time.Second,
	}
	pipeline := NewPipeline(cfg)

	if err := pipeline.Run(context.Background()); err != nil {
		t.Fatalf("pipeline.Run failed: %v", err)
	}

	// Verify nothing was indexed into lexical.db
	lexIdx, err := NewLexicalIndexer(stateDir)
	if err != nil {
		t.Fatalf("failed to open lexical.db: %v", err)
	}
	defer lexIdx.Close()
	count, err := lexIdx.Count()
	if err != nil {
		t.Fatalf("failed to count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 indexed docs for snapshot repo, got %d", count)
	}
}

