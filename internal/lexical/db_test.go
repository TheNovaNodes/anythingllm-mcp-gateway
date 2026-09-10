package lexical

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestBuildSafeFTSQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"a", ""},
		{"hello world", "\"hello\" OR \"hello\"* OR \"world\" OR \"world\"*"},
		{"to be or not to be", "\"to\" OR \"be\" OR \"or\" OR \"not\""},
		{"go & python: fast!", "\"go\" OR \"python\" OR \"python\"* OR \"fast\" OR \"fast\"*"},
		{"ChaCha20-Poly1305", "\"ChaCha20-Poly1305\" OR \"ChaCha20-Poly1305\"* OR \"ChaCha20\" OR \"ChaCha20\"* OR \"Cha\" OR \"Cha20\" OR \"Cha20\"* OR \"Poly1305\" OR \"Poly1305\"*"},
		{"ChaCha20Poly1305", "\"ChaCha20Poly1305\" OR \"ChaCha20Poly1305\"* OR \"Cha\" OR \"Cha20Poly1305\" OR \"Cha20Poly1305\"*"},
		{"маршрутизация", "\"маршрутизация\" OR \"маршрутизация\"* OR \"маршрутиза\"*"},
		{"deployments", "\"deployments\" OR \"deployments\"* OR \"deploy\"*"},
	}

	for _, tc := range tests {
		actual := BuildSafeFTSQuery(tc.input)
		if actual != tc.expected {
			t.Errorf("BuildSafeFTSQuery(%q) = %q; expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestDB_FTS5SearchAndGetDocument(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_lexical.db")

	dbInit, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite test db: %v", err)
	}

	_, err = dbInit.Exec(`
		CREATE VIRTUAL TABLE docs_fts USING fts5(path, title, workspace, content);
		INSERT INTO docs_fts(path, title, workspace, content) VALUES
			('protocols/agents/MANIFEST.md', 'Manifest', 'thenovanodes-ecosystem-docs', 'The NovaNodes Collective foundation directives and rules.'),
			('docs/architecture.md', 'Architecture', 'thenovanodes-anythingllm-mcp-gateway', 'AnythingLLM MCP gateway architecture and vector search details.');
	`)
	if err != nil {
		t.Fatalf("failed to populate FTS5 table: %v", err)
	}
	dbInit.Close()

	lexDB, err := NewDB(dbPath, 0.0)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer lexDB.Close()

	if !lexDB.IsAvailable() {
		t.Fatal("expected DB to be available")
	}

	ctx := context.Background()

	// 1. Search FTS5 global
	hits, err := lexDB.Search(ctx, "collective foundation", "", 5)
	if err != nil {
		t.Fatalf("Search failed with error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].DocID != "protocols/agents/MANIFEST.md" {
		t.Errorf("unexpected docID: %s", hits[0].DocID)
	}
	if hits[0].Workspace != "thenovanodes-ecosystem-docs" {
		t.Errorf("unexpected workspace: %s", hits[0].Workspace)
	}

	// 2. Search scoped to matching workspace
	hitsScoped, err := lexDB.Search(ctx, "architecture", "thenovanodes-anythingllm-mcp-gateway", 5)
	if err != nil {
		t.Fatalf("Scoped search failed: %v", err)
	}
	if len(hitsScoped) != 1 || hitsScoped[0].DocID != "docs/architecture.md" {
		t.Errorf("expected 1 scoped hit, got %+v", hitsScoped)
	}

	// 3. Search scoped to non-matching workspace returns 0
	hitsEmpty, err := lexDB.Search(ctx, "architecture", "different-workspace", 5)
	if err != nil {
		t.Fatalf("Search with wrong workspace failed: %v", err)
	}
	if len(hitsEmpty) != 0 {
		t.Errorf("expected 0 hits for wrong workspace, got %d", len(hitsEmpty))
	}

	// 4. GetDocument exact path
	doc1, err := lexDB.GetDocument(ctx, "protocols/agents/MANIFEST.md", "", 100)
	if err != nil || !doc1.Found {
		t.Fatalf("GetDocument exact failed: %v, doc: %+v", err, doc1)
	}
	if doc1.Title != "Manifest" {
		t.Errorf("expected Title 'Manifest', got %q", doc1.Title)
	}

	// 5. GetDocument scoped by workspace
	doc2, err := lexDB.GetDocument(ctx, "architecture.md", "thenovanodes-anythingllm-mcp-gateway", 100)
	if err != nil || !doc2.Found {
		t.Fatalf("GetDocument scoped failed: %v, doc: %+v", err, doc2)
	}

	// 6. GetDocument missing
	docMissing, err := lexDB.GetDocument(ctx, "nonexistent.md", "", 100)
	if err != nil || docMissing.Found {
		t.Fatalf("expected found=false for missing document, got %+v", docMissing)
	}
}

func TestDB_Unavailable(t *testing.T) {
	lexDB, err := NewDB("/path/to/nonexistent.db", 1.0)
	if err != nil {
		t.Fatalf("NewDB on missing path returned error: %v", err)
	}
	defer lexDB.Close()

	if lexDB.IsAvailable() {
		t.Error("expected IsAvailable() to be false")
	}

	hits, err := lexDB.Search(context.Background(), "test", "", 5)
	if err != nil || hits != nil {
		t.Errorf("expected nil, nil on unavailable DB, got hits: %v, err: %v", hits, err)
	}

	doc, err := lexDB.GetDocument(context.Background(), "test.md", "", 100)
	if err != nil || doc.Found {
		t.Errorf("expected found=false, got doc: %+v, err: %v", doc, err)
	}
}

func TestDB_ChunkAssembly(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_chunk_assembly.db")

	dbInit, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	_, err = dbInit.Exec(`
		CREATE VIRTUAL TABLE docs_fts USING fts5(path, title, workspace, content);
		INSERT INTO docs_fts(path, title, workspace, content) VALUES
			('/projects/org/repo/essay.md#chunk-0', 'essay.md (Part 1/2)', 'org-repo', 'Part 1 intro to Kairos.'),
			('/projects/org/repo/essay.md#chunk-1', 'essay.md (Part 2/2)', 'org-repo', 'Part 2 conclusion to Chronos.');
	`)
	if err != nil {
		t.Fatalf("failed to populate test table: %v", err)
	}
	dbInit.Close()

	lexDB, err := NewDB(dbPath, 0.0)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer lexDB.Close()

	ctx := context.Background()

	// 1. Fetch exact chunk
	chunkDoc, err := lexDB.GetDocument(ctx, "/projects/org/repo/essay.md#chunk-0", "", 500)
	if err != nil || !chunkDoc.Found {
		t.Fatalf("failed to fetch chunk: %v", err)
	}
	if chunkDoc.Content != "Part 1 intro to Kairos." {
		t.Errorf("unexpected chunk content: %q", chunkDoc.Content)
	}

	// 2. Fetch parent doc (re-assembly)
	parentDoc, err := lexDB.GetDocument(ctx, "/projects/org/repo/essay.md", "", 500)
	if err != nil || !parentDoc.Found {
		t.Fatalf("failed to re-assemble parent doc: %v", err)
	}
	if parentDoc.Title != "essay.md" {
		t.Errorf("expected title 'essay.md', got %q", parentDoc.Title)
	}
	expectedCombined := "Part 1 intro to Kairos.\n\nPart 2 conclusion to Chronos."
	if parentDoc.Content != expectedCombined {
		t.Errorf("expected combined content %q, got %q", expectedCombined, parentDoc.Content)
	}

	// 3. Fetch parent doc scoped by workspace
	scopedParent, err := lexDB.GetDocument(ctx, "essay.md", "org-repo", 500)
	if err != nil || !scopedParent.Found {
		t.Fatalf("failed to re-assemble scoped parent doc: %v", err)
	}
	if scopedParent.Content != expectedCombined {
		t.Errorf("expected scoped combined content %q, got %q", expectedCombined, scopedParent.Content)
	}
}
