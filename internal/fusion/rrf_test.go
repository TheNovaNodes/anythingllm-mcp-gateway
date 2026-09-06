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
		workspace string
		docID     string
		title     string
		expected  string
	}{
		{"ws1", "protocols/agents/MANIFEST.md", "", "ws1:manifest"},
		{"ws1", "/root/docs/README.md", "README", "ws1:readme"},
		{"", "Architecture.MD", "", "architecture"},
		{"", "/root/projects/TheNovaNodes/mcp-router/README.md", "", "thenovanodes-mcp-router:readme"},
		{"docs\\win\\PATH.MD", "", "", "docs\\win\\path.md:"},
	}

	for _, tc := range tests {
		actual := DedupKey(tc.workspace, tc.docID, tc.title)
		if actual != tc.expected {
			t.Errorf("DedupKey(%q, %q, %q) = %q; expected %q", tc.workspace, tc.docID, tc.title, actual, tc.expected)
		}
	}
}

func TestRRFMerge(t *testing.T) {
	vHits := []alm.VectorHit{
		{DocID: "docs/architecture.md", Title: "Architecture", Workspace: "ws1", Text: "Vector text", VectorScore: 0.95},
		{DocID: "docs/unique_vec.md", Title: "Unique Vec", Workspace: "ws1", Text: "Vec only", VectorScore: 0.80},
	}

	lHits := []lexical.LexicalHit{
		{DocID: "docs/architecture.md", Title: "Architecture", Workspace: "ws1", Text: "Lexical snippet", LexicalScore: 1.5},
		{DocID: "docs/unique_lex.md", Title: "Unique Lex", Workspace: "ws1", Text: "Lex only", LexicalScore: 1.2},
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

func TestRRFMerge_NoCrossWorkspaceCollision(t *testing.T) {
	// Two separate workspaces with identical filenames
	vHits := []alm.VectorHit{
		{DocID: "readme.txt", Title: "README.md", Workspace: "thenovanodes-mcp-router", Text: "Router docs", VectorScore: 0.9},
		{DocID: "readme.txt", Title: "README.md", Workspace: "thenovanodes-nextcloud-mcp-control", Text: "Nextcloud docs", VectorScore: 0.8},
	}

	results := RRFMerge(vHits, nil, 5, 60)
	if len(results) != 2 {
		t.Fatalf("expected 2 distinct items from 2 workspaces without collision, got %d", len(results))
	}
	if results[0].Workspace == results[1].Workspace {
		t.Errorf("expected distinct workspaces, got %s and %s", results[0].Workspace, results[1].Workspace)
	}
}

func TestRRFMerge_WorkspacePrioritizationBoost(t *testing.T) {
	vHits := []alm.VectorHit{
		{DocID: "readme.txt", Title: "README.md", Workspace: "thenovanodes-nextcloud-mcp-control", Text: "General docs", VectorScore: 0.70},
		{DocID: "readme.txt", Title: "README.md", Workspace: "thenovanodes-mcp-router", Text: "Router docs", VectorScore: 0.69},
	}

	// Without query boost, nextcloud is top-1
	unboosted := RRFMerge(vHits, nil, 5, 60)
	if unboosted[0].Workspace != "thenovanodes-nextcloud-mcp-control" {
		t.Errorf("expected unboosted top to be nextcloud, got %s", unboosted[0].Workspace)
	}

	// With query containing "router", mcp-router gets boosted to top-1
	boosted := RRFMerge(vHits, nil, 5, 60, "mcp router architecture")
	if boosted[0].Workspace != "thenovanodes-mcp-router" {
		t.Errorf("expected boosted top to be mcp-router, got %s", boosted[0].Workspace)
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

func TestFilterByOrg(t *testing.T) {
	vHits := []alm.VectorHit{
		{DocID: "doc1.txt", Title: "Doc1", Workspace: "thenovanodes-agent-vault"},
		{DocID: "doc2.txt", Title: "Doc2", Workspace: "thedoctormes-hue-doctorm-unify-protocol"},
	}

	// 1. Filter by allowed org "thenovanodes"
	filtered := FilterVectorHitsByOrg(vHits, []string{"thenovanodes"})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 hit for thenovanodes, got %d", len(filtered))
	}
	if filtered[0].Workspace != "thenovanodes-agent-vault" {
		t.Errorf("unexpected workspace in filtered: %s", filtered[0].Workspace)
	}

	// 2. Filter by allowed org "thedoctormes-hue"
	filteredDoc := FilterVectorHitsByOrg(vHits, []string{"thedoctormes-hue"})
	if len(filteredDoc) != 1 || filteredDoc[0].Workspace != "thedoctormes-hue-doctorm-unify-protocol" {
		t.Errorf("expected 1 doctormes hit, got %+v", filteredDoc)
	}

	// 3. Filter with empty allowed orgs allows all
	all := FilterVectorHitsByOrg(vHits, nil)
	if len(all) != 2 {
		t.Errorf("expected all 2 hits when allowedOrgs is empty, got %d", len(all))
	}
}

func TestRRFMerge_ExactLexicalBoost(t *testing.T) {
	vHits := []alm.VectorHit{
		{DocID: "unrelated.txt", Title: "BTC REFUTED", Workspace: "ws1", Text: "Crypto table", VectorScore: 0.95},
	}
	lHits := []lexical.LexicalHit{
		{DocID: "readme.txt", Title: "ChaCha20Poly1305 README", Workspace: "ws1", Text: "ChaCha20Poly1305 details", LexicalScore: 8.5},
	}

	results := RRFMerge(vHits, lHits, 5, 60, "ChaCha20Poly1305")
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// High lexical score + exact title match boost should rank ChaCha20Poly1305 at top-1
	if results[0].DocID != "readme.txt" {
		t.Errorf("expected exact lexical boost to elevate readme.txt to top-1, got top-1: %s (%s)", results[0].DocID, results[0].Title)
	}
}

func TestRRFMerge_PureVectorSimilarityCutoff(t *testing.T) {
	// 1. Out-of-domain query: low vector score, 0 lexical matches -> must be dropped (#45)
	vNoise := []alm.VectorHit{
		{DocID: "noise1.txt", Title: "Noise 1", Workspace: "ws1", Text: "Some random text", VectorScore: 0.35},
		{DocID: "noise2.txt", Title: "Noise 2", Workspace: "ws1", Text: "Another text", VectorScore: 0.42},
	}
	resNoise := RRFMerge(vNoise, nil, 5, 60, "квантовая хромодинамика")
	if len(resNoise) != 0 {
		t.Fatalf("expected 0 results for out-of-domain pure-vector noise, got %d", len(resNoise))
	}

	// 2. In-domain semantic query: high vector score (>= 0.55), 0 lexical matches -> must be kept
	vValid := []alm.VectorHit{
		{DocID: "arch.txt", Title: "Architecture Overview", Workspace: "ws1", Text: "System design", VectorScore: 0.68},
	}
	resValid := RRFMerge(vValid, nil, 5, 60, "system architecture design")
	if len(resValid) != 1 {
		t.Fatalf("expected 1 result for strong semantic vector hit, got %d", len(resValid))
	}
	if resValid[0].DocID != "arch.txt" {
		t.Errorf("expected arch.txt, got %s", resValid[0].DocID)
	}

	// 3. Technical keyword query: low vector score (0.30) but strong lexical hit (8.0) -> must be kept (hybrid)
	lStrong := []lexical.LexicalHit{
		{DocID: "noise1.txt", Title: "Noise 1", Workspace: "ws1", Text: "Some random text", LexicalScore: 8.0},
	}
	resHybrid := RRFMerge(vNoise, lStrong, 5, 60, "technical keyword")
	if len(resHybrid) != 1 {
		t.Fatalf("expected 1 hybrid result preserved despite low vector score, got %d", len(resHybrid))
	}
	if resHybrid[0].Source != "hybrid" {
		t.Errorf("expected source to be hybrid, got %s", resHybrid[0].Source)
	}
}

