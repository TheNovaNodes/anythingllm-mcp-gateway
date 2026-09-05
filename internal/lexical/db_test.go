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
		{"hello world", "\"hello\" OR \"world\""},
		{"to be or not to be", "\"to\" OR \"be\" OR \"or\" OR \"not\" OR \"to\" OR \"be\""},
		{"go & python: fast!", "\"go\" OR \"python\" OR \"fast\""},
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
		CREATE VIRTUAL TABLE docs_fts USING fts5(path, title, content);
		INSERT INTO docs_fts(path, title, content) VALUES
			('protocols/agents/MANIFEST.md', 'Manifest', 'The NovaNodes Collective foundation directives and rules.'),
			('docs/architecture.md', 'Architecture', 'AnythingLLM MCP gateway architecture and vector search details.');
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

	// 1. Search FTS5
	hits, err := lexDB.Search(ctx, "collective foundation", 5)
	if err != nil {
		t.Fatalf("Search failed with error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].DocID != "protocols/agents/MANIFEST.md" {
		t.Errorf("unexpected docID: %s", hits[0].DocID)
	}

	// 2. Search with FTS5 keyword ("or not")
	hitsKeyword, err := lexDB.Search(ctx, "vector or architecture", 5)
	if err != nil {
		t.Fatalf("Search with keywords failed: %v", err)
	}
	if len(hitsKeyword) != 1 || hitsKeyword[0].DocID != "docs/architecture.md" {
		t.Errorf("unexpected hitsKeyword: %+v", hitsKeyword)
	}

	// 3. GetDocument exact path
	doc1, err := lexDB.GetDocument(ctx, "protocols/agents/MANIFEST.md", 100)
	if err != nil || !doc1.Found {
		t.Fatalf("GetDocument exact failed: %v, doc: %+v", err, doc1)
	}
	if doc1.Title != "Manifest" {
		t.Errorf("expected Title 'Manifest', got %q", doc1.Title)
	}

	// 4. GetDocument suffix fallback
	doc2, err := lexDB.GetDocument(ctx, "MANIFEST.md", 100)
	if err != nil || !doc2.Found {
		t.Fatalf("GetDocument suffix fallback failed: %v, doc: %+v", err, doc2)
	}

	// 5. GetDocument missing
	docMissing, err := lexDB.GetDocument(ctx, "nonexistent.md", 100)
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

	hits, err := lexDB.Search(context.Background(), "test", 5)
	if err != nil || hits != nil {
		t.Errorf("expected nil, nil on unavailable DB, got hits: %v, err: %v", hits, err)
	}

	doc, err := lexDB.GetDocument(context.Background(), "test.md", 100)
	if err != nil || doc.Found {
		t.Errorf("expected found=false, got doc: %+v, err: %v", doc, err)
	}
}
