package fusion

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/alm"
	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/lexical"
)

// SearchResultItem represents a merged and scored document fragment.
type SearchResultItem struct {
	DocID           string  `json:"doc_id"`
	Title           string  `json:"title"`
	Source          string  `json:"source"`
	Workspace       string  `json:"workspace,omitempty"`
	Text            string  `json:"text"`
	Score           float64 `json:"score"`
	VectorScore     float64 `json:"vector_score,omitempty"`
	LexicalScore    float64 `json:"lexical_score,omitempty"`
	ContextExpanded bool    `json:"context_expanded,omitempty"`
	TrimmedToBudget bool    `json:"trimmed_to_budget,omitempty"`
}

// DedupKey returns a composite key (workspace:stem) for cross-layer document deduplication.
func DedupKey(workspace, docID, title string) string {
	target := docID
	if target == "" {
		target = title
	}
	base := filepath.Base(strings.ReplaceAll(target, "\\", "/"))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stemClean := strings.ToLower(strings.TrimSpace(stem))

	wsClean := strings.ToLower(strings.TrimSpace(workspace))
	if wsClean == "" {
		wsClean = strings.ToLower(lexical.DeriveWorkspaceFromPath(target))
	}

	if wsClean == "" {
		return stemClean
	}
	return wsClean + ":" + stemClean
}

// RRFMerge combines vector hits and lexical hits using Reciprocal Rank Fusion.
// If query is provided, performs workspace-prioritization boost for matching repositories.
func RRFMerge(vectorHits []alm.VectorHit, lexicalHits []lexical.LexicalHit, topK, rrfK int, query ...string) []SearchResultItem {
	if rrfK <= 0 {
		rrfK = 60
	}
	if topK <= 0 {
		topK = 5
	}

	type candidate struct {
		item    SearchResultItem
		hasVec  bool
		hasLex  bool
		rrfRank float64
	}

	merged := make(map[string]*candidate)

	// 1. Process Vector Hits
	for rank, vHit := range vectorHits {
		key := DedupKey(vHit.Workspace, vHit.DocID, vHit.Title)
		rrfContrib := 1.0 / float64(rrfK+rank+1)

		if c, exists := merged[key]; exists {
			c.rrfRank += rrfContrib
			c.hasVec = true
			c.item.VectorScore = vHit.VectorScore
			if c.item.Text == "" {
				c.item.Text = vHit.Text
			}
			if c.item.Workspace == "" {
				c.item.Workspace = vHit.Workspace
			}
		} else {
			merged[key] = &candidate{
				item: SearchResultItem{
					DocID:       vHit.DocID,
					Title:       vHit.Title,
					Workspace:   vHit.Workspace,
					Text:        vHit.Text,
					VectorScore: vHit.VectorScore,
				},
				hasVec:  true,
				rrfRank: rrfContrib,
			}
		}
	}

	// 2. Process Lexical Hits
	for rank, lHit := range lexicalHits {
		key := DedupKey(lHit.Workspace, lHit.DocID, lHit.Title)
		rrfContrib := 1.0 / float64(rrfK+rank+1)

		if c, exists := merged[key]; exists {
			c.rrfRank += rrfContrib
			c.hasLex = true
			c.item.LexicalScore = lHit.LexicalScore
			if c.item.Text == "" {
				c.item.Text = lHit.Text
			}
			if c.item.Workspace == "" {
				c.item.Workspace = lHit.Workspace
			}
		} else {
			merged[key] = &candidate{
				item: SearchResultItem{
					DocID:        lHit.DocID,
					Title:        lHit.Title,
					Workspace:    lHit.Workspace,
					Text:         lHit.Text,
					LexicalScore: lHit.LexicalScore,
				},
				hasLex:  true,
				rrfRank: rrfContrib,
			}
		}
	}

	// Prepare query tokens for workspace prioritization boost
	var queryTokens []string
	if len(query) > 0 && strings.TrimSpace(query[0]) != "" {
		words := strings.FieldsFunc(strings.ToLower(query[0]), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		for _, w := range words {
			if len([]rune(w)) >= 3 && w != "mcp" && w != "the" && w != "and" && w != "for" {
				queryTokens = append(queryTokens, w)
			}
		}
	}

	// 3. Assemble and calculate final scores with workspace boost
	results := make([]SearchResultItem, 0, len(merged))
	for _, c := range merged {
		boost := 1.0
		if len(queryTokens) > 0 && c.item.Workspace != "" {
			wsLower := strings.ToLower(c.item.Workspace)
			for _, tok := range queryTokens {
				if strings.Contains(wsLower, tok) {
					boost *= 1.35
				}
			}
			if boost > 2.5 {
				boost = 2.5
			}
		}

		c.item.Score = c.rrfRank * boost
		if c.hasVec && c.hasLex {
			c.item.Source = "hybrid"
		} else if c.hasVec {
			c.item.Source = "vector"
		} else {
			c.item.Source = "lexical"
		}
		results = append(results, c.item)
	}

	// 4. Sort descending by fused score
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}

	return results
}
