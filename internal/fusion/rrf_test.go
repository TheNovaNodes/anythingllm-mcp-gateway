package fusion

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/alm"
	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/lexical"
)

func TestDedupKey(t *testing.T) {
	tests := []struct {
		docID    string
		title    string
		expected string
	}{
		{"protocols/agents/MANIFEST.md", "", "manifest.md"},
		{"/root/docs/README.md", "README", "readme.md"},
		{"", "Architecture.MD", "architecture.md"},
		{"docs\\win\\PATH.MD", "", "path.md"},
	}

	for _, tc := range tests {
		actual := DedupKey(tc.docID, tc.title)
		if actual != tc.expected {
			t.Errorf("DedupKey(%q, %q) = %q; expected %q", tc.docID, tc.title, actual, tc.expected)
		}
	}
}

func TestRRFMerge(t *testing.T) {
	vHits := []alm.VectorHit{
		{DocID: "docs/architecture.md", Title: "Architecture", Workspace: "ws1", Text: "Vector text", VectorScore: 0.95},
		{DocID: "docs/unique_vec.md", Title: "Unique Vec", Workspace: "ws1", Text: "Vec only", VectorScore: 0.80},
	}

	lHits := []lexical.LexicalHit{
		{DocID: "docs/architecture.md", Title: "Architecture", Text: "Lexical snippet", LexicalScore: 1.5},
		{DocID: "docs/unique_lex.md", Title: "Unique Lex", Text: "Lex only", LexicalScore: 1.2},
	}

	results := RRFMerge(vHits, lHits, 5, 60)

	if len(results) != 3 {
		t.Fatalf("expected 3 merged items, got %d", len(results))
	}

	// First item must be the hybrid match "docs/architecture.md"
	if results[0].Source != "hybrid" || results[0].DocID != "docs/architecture.md" {
		t.Errorf("expected top result to be hybrid architecture.md, got %+v", results[0])
	}
	if results[0].VectorScore != 0.95 || results[0].LexicalScore != 1.5 {
		t.Errorf("expected vector and lexical scores preserved: %+v", results[0])
	}

	// Verify other sources
	sources := map[string]bool{}
	for _, r := range results {
		sources[r.Source] = true
	}
	if !sources["hybrid"] || !sources["vector"] || !sources["lexical"] {
		t.Errorf("expected all 3 sources in results: %v", sources)
	}
}

func TestExpandContext(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_context.db")

	dbInit, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	dbInit.Exec(`
		CREATE VIRTUAL TABLE docs_fts USING fts5(path, title, content);
		INSERT INTO docs_fts(path, title, content) VALUES
			('protocols/test.md', 'Test', 'Paragraph 1\n\nFull expanded paragraph 2 with lots of words.\n\nParagraph 3');
	`)
	dbInit.Close()

	lexDB, _ := lexical.NewDB(dbPath, 0.0)
	defer lexDB.Close()

	items := []SearchResultItem{
		{DocID: "protocols/test.md", Text: "Paragraph 1"},
	}

	expanded := ExpandContext(context.Background(), lexDB, items, 1000)
	if !expanded[0].ContextExpanded {
		t.Error("expected ContextExpanded = true")
	}
	if len(expanded[0].Text) <= len("Paragraph 1") {
		t.Errorf("expected expanded text, got %q", expanded[0].Text)
	}

	// Unavailable DB test
	unexpanded := ExpandContext(context.Background(), nil, items, 1000)
	if len(unexpanded) != 1 {
		t.Error("expected 1 item on nil DB")
	}
}

func TestTrimToTokenBudget(t *testing.T) {
	items := []SearchResultItem{
		{Text: "This is item one with some text."},
		{Text: "This is item two with a very long paragraph that will exceed the remaining token budget easily."},
		{Text: "This is item three which will be completely dropped."},
	}

	trimmed, totalTokens := TrimToTokenBudget(items, 15) // small budget

	if len(trimmed) > 2 {
		t.Fatalf("expected at most 2 items after trimming, got %d", len(trimmed))
	}
	if totalTokens > 18 { // slight leeway for boundary
		t.Errorf("total tokens %d exceeded budget significantly", totalTokens)
	}
	if len(trimmed) == 2 && !trimmed[1].TrimmedToBudget {
		t.Error("expected second item to be marked as TrimmedToBudget")
	}
}

func TestRRFMerge_EdgeCases(t *testing.T) {
	// Both empty
	emptyRes := RRFMerge(nil, nil, 5, 60)
	if len(emptyRes) != 0 {
		t.Errorf("expected 0 results for empty inputs, got %d", len(emptyRes))
	}

	// Vector only
	vHits := []alm.VectorHit{
		{DocID: "doc-v1.md", Title: "V1", Text: "text", VectorScore: 0.8},
	}
	vOnly := RRFMerge(vHits, nil, 5, 60)
	if len(vOnly) != 1 || vOnly[0].Source != "vector" {
		t.Errorf("expected 1 vector-only item, got %+v", vOnly)
	}

	// Lexical only
	lHits := []lexical.LexicalHit{
		{DocID: "doc-l1.md", Title: "L1", Text: "text", LexicalScore: 1.2},
	}
	lOnly := RRFMerge(nil, lHits, 5, 60)
	if len(lOnly) != 1 || lOnly[0].Source != "lexical" {
		t.Errorf("expected 1 lexical-only item, got %+v", lOnly)
	}

	// Zero or negative top_k defaults to 5
	defTopK := RRFMerge(vHits, lHits, 0, 0)
	if len(defTopK) != 2 {
		t.Errorf("expected 2 items, got %d", len(defTopK))
	}
}

func TestTrimToTokenBudget_EdgeCases(t *testing.T) {
	// Zero or negative budget
	items := []SearchResultItem{
		{Text: "Some text here"},
	}
	out, tokens := TrimToTokenBudget(items, 0)
	if len(out) != 1 || tokens <= 0 {
		t.Errorf("expected full items when budget <= 0, got %d, %d", len(out), tokens)
	}

	// Empty items
	outEmpty, tokensEmpty := TrimToTokenBudget(nil, 100)
	if len(outEmpty) != 0 || tokensEmpty != 0 {
		t.Errorf("expected empty, got %d, %d", len(outEmpty), tokensEmpty)
	}
}
