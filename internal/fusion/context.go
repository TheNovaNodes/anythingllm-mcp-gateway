package fusion

import (
	"context"
	"strings"
	"unicode"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/lexical"
)

// ExpandContext attempts to expand matched passages with adjacent document paragraphs from the FTS database.
func ExpandContext(ctx context.Context, lexDB *lexical.DB, items []SearchResultItem, maxChars int) []SearchResultItem {
	if lexDB == nil || !lexDB.IsAvailable() {
		return items
	}

	if maxChars <= 0 {
		maxChars = 4000
	}

	for i := range items {
		if items[i].DocID == "" {
			continue
		}

		doc, err := lexDB.GetDocument(ctx, items[i].DocID, maxChars)
		if err == nil && doc != nil && doc.Found && doc.Content != "" {
			// If full content is reasonable length, adopt it as expanded context
			if len([]rune(doc.Content)) > len([]rune(items[i].Text)) {
				items[i].Text = doc.Content
				items[i].ContextExpanded = true
			}
		}
	}

	return items
}

// EstimateTokens calculates approximate token count for text (average 3.8 runes per token).
func EstimateTokens(text string) int {
	runes := len([]rune(text))
	if runes == 0 {
		return 0
	}
	tokens := int(float64(runes) / 3.8)
	if tokens < 1 {
		return 1
	}
	return tokens
}

// TrimToTokenBudget trims result items to stay within maximum token budget.
func TrimToTokenBudget(items []SearchResultItem, maxBudget int) ([]SearchResultItem, int) {
	if maxBudget <= 0 || len(items) == 0 {
		total := 0
		for _, it := range items {
			total += EstimateTokens(it.Text)
		}
		return items, total
	}

	var trimmed []SearchResultItem
	totalTokens := 0

	for _, item := range items {
		tokens := EstimateTokens(item.Text)
		if totalTokens+tokens <= maxBudget {
			trimmed = append(trimmed, item)
			totalTokens += tokens
			continue
		}

		// Partial trim on boundary
		remainingTokens := maxBudget - totalTokens
		if remainingTokens > 15 {
			allowedChars := int(float64(remainingTokens) * 3.8)
			runes := []rune(item.Text)
			if len(runes) > allowedChars {
				// Cut at last space or sentence boundary
				cutIdx := allowedChars
				for i := allowedChars; i > allowedChars-40 && i > 0; i-- {
					if unicode.IsSpace(runes[i]) || runes[i] == '.' || runes[i] == '\n' {
						cutIdx = i
						break
					}
				}
				item.Text = strings.TrimSpace(string(runes[:cutIdx])) + "..."
				item.TrimmedToBudget = true
				trimmed = append(trimmed, item)
				totalTokens += EstimateTokens(item.Text)
			}
		}
		break
	}

	return trimmed, totalTokens
}
