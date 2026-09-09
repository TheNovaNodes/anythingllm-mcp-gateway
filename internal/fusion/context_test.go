package fusion

import (
	"context"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"empty string", "", 0},
		{"small string", "hello", 1},
		{"average sentence", "This is an average sentence with some words.", 11},
		{"long string", strings.Repeat("a", 380), 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EstimateTokens(tt.text); got != tt.expected {
				t.Errorf("EstimateTokens() = %d, expected %d", got, tt.expected)
			}
		})
	}
}

func TestTrimToTokenBudget_EdgeCases(t *testing.T) {
	// 1. Zero or negative budget
	items := []SearchResultItem{{Text: "Some text"}}
	trimmed, total := TrimToTokenBudget(items, 0)
	if len(trimmed) != 1 {
		t.Errorf("expected 1 item for zero budget bypass, got %d", len(trimmed))
	}
	if total <= 0 {
		t.Errorf("expected total tokens > 0, got %d", total)
	}

	// 2. Empty items
	trimmedEmpty, totalEmpty := TrimToTokenBudget(nil, 100)
	if len(trimmedEmpty) != 0 || totalEmpty != 0 {
		t.Errorf("expected empty results for empty items, got len %d, total %d", len(trimmedEmpty), totalEmpty)
	}

	// 3. Sentence boundary cutoff fallback
	longText := strings.Repeat("a", 200) + " " + strings.Repeat("b", 100)
	boundaryItems := []SearchResultItem{{Text: longText}}
	
	// Budget ~40 tokens (approx 152 chars) -> Should trigger partial boundary trim on space
	budget := 40
	trimmedBoundary, _ := TrimToTokenBudget(boundaryItems, budget)
	
	if len(trimmedBoundary) != 1 {
		t.Fatalf("expected 1 item, got %d", len(trimmedBoundary))
	}
	if !trimmedBoundary[0].TrimmedToBudget {
		t.Errorf("expected item to be marked as TrimmedToBudget")
	}
	if !strings.HasSuffix(trimmedBoundary[0].Text, "...") {
		t.Errorf("expected item text to be suffixed with '...' due to truncation, got: %s", trimmedBoundary[0].Text)
	}
}

func TestExpandContext_UnavailableLexDB(t *testing.T) {
	items := []SearchResultItem{{DocID: "doc1", Text: "short text"}}
	
	// Lexical DB is nil
	res1 := ExpandContext(context.Background(), nil, items, 1000)
	if len(res1) != 1 || res1[0].ContextExpanded {
		t.Errorf("expected no expansion for nil DB, got %+v", res1)
	}

	// Also tests maxChars fallback to 4000 if <= 0
	res2 := ExpandContext(context.Background(), nil, items, -100)
	if len(res2) != 1 || res2[0].ContextExpanded {
		t.Errorf("expected no expansion for nil DB, got %+v", res2)
	}
}
