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
	Tier            string  `json:"tier,omitempty"`
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

// DefaultMinPureVectorSimilarity defines minimum cosine similarity required for pure-vector hits (issue #45).
const DefaultMinPureVectorSimilarity = 0.55

// RRFOptions defines tunable weights and thresholds for hybrid search fusion.
type RRFOptions struct {
	TopK             int     `json:"top_k"`
	RRFK             int     `json:"rrf_k"`
	MinPureVectorSim float64 `json:"min_pure_vector_sim"`
	VectorWeight     float64 `json:"vector_weight"` // default: 1.0
	LexicalWeight    float64 `json:"lexical_weight"` // default: 1.0
	SynergyBonus     float64 `json:"synergy_bonus"`  // multiplier boost for hybrid hits (default: 0.25)
	Query            string  `json:"query"`
}

// DefaultRRFOptions returns production-calibrated RRF options.
func DefaultRRFOptions() RRFOptions {
	return RRFOptions{
		TopK:             5,
		RRFK:             60,
		MinPureVectorSim: DefaultMinPureVectorSimilarity,
		VectorWeight:     1.0,
		LexicalWeight:    1.0,
		SynergyBonus:     0.25,
	}
}

// RRFMerge combines vector hits and lexical hits using Reciprocal Rank Fusion.
// If query is provided, performs workspace-prioritization boost for matching repositories.
func RRFMerge(vectorHits []alm.VectorHit, lexicalHits []lexical.LexicalHit, topK, rrfK int, query ...string) []SearchResultItem {
	return RRFMergeWithCutoff(vectorHits, lexicalHits, topK, rrfK, DefaultMinPureVectorSimilarity, query...)
}

// RRFMergeWithCutoff combines vector hits and lexical hits with an explicit pure-vector similarity cutoff.
func RRFMergeWithCutoff(vectorHits []alm.VectorHit, lexicalHits []lexical.LexicalHit, topK, rrfK int, minPureVectorSim float64, query ...string) []SearchResultItem {
	q := ""
	if len(query) > 0 {
		q = query[0]
	}
	opts := RRFOptions{
		TopK:             topK,
		RRFK:             rrfK,
		MinPureVectorSim: minPureVectorSim,
		VectorWeight:     1.0,
		LexicalWeight:    1.0,
		SynergyBonus:     0.25,
		Query:            q,
	}
	return RRFMergeWithOptions(vectorHits, lexicalHits, opts)
}

// RRFMergeWithOptions combines vector hits and lexical hits with full control over weights, synergy bonus, and thresholds.
func RRFMergeWithOptions(vectorHits []alm.VectorHit, lexicalHits []lexical.LexicalHit, opts RRFOptions) []SearchResultItem {
	rrfK := opts.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}
	topK := opts.TopK
	if topK <= 0 {
		topK = 5
	}
	vecW := opts.VectorWeight
	if vecW <= 0 {
		vecW = 1.0
	}
	lexW := opts.LexicalWeight
	if lexW <= 0 {
		lexW = 1.0
	}
	synergy := opts.SynergyBonus
	if synergy < 0 {
		synergy = 0.0
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
		rrfContrib := vecW / float64(rrfK+rank+1)

		if c, exists := merged[key]; exists {
			c.rrfRank += rrfContrib
			c.hasVec = true
			c.item.VectorScore = vHit.VectorScore
			if c.item.Tier == "" && vHit.Tier != "" {
				c.item.Tier = vHit.Tier
			}
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
					Tier:        vHit.Tier,
				},
				hasVec:  true,
				rrfRank: rrfContrib,
			}
		}
	}

	// 2. Process Lexical Hits
	for rank, lHit := range lexicalHits {
		key := DedupKey(lHit.Workspace, lHit.DocID, lHit.Title)
		rrfContrib := lexW / float64(rrfK+rank+1)

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
	cleanQuery := strings.TrimSpace(opts.Query)
	if cleanQuery != "" {
		words := strings.FieldsFunc(strings.ToLower(cleanQuery), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		for _, w := range words {
			if len([]rune(w)) >= 3 && w != "mcp" && w != "the" && w != "and" && w != "for" {
				queryTokens = append(queryTokens, w)
			}
		}
	}

	// 3. Assemble and calculate final scores with synergy, workspace boost and exact match bonus
	rawQuery := strings.ToLower(cleanQuery)

	results := make([]SearchResultItem, 0, len(merged))
	for _, c := range merged {
		// Pure vector threshold cutoff (#45): discard pure-vector candidates below similarity threshold
		if c.hasVec && !c.hasLex && opts.MinPureVectorSim > 0 && c.item.VectorScore < opts.MinPureVectorSim {
			continue
		}

		// Synergy multiplier: reward items corroborated by both vector & lexical modalities
		if c.hasVec && c.hasLex && synergy > 0 {
			c.rrfRank *= (1.0 + synergy)
		}

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

		// Exact lexical boost (#37)
		if c.hasLex && c.item.LexicalScore >= 5.0 {
			c.rrfRank += 0.02
		}
		if rawQuery != "" && len(rawQuery) >= 3 {
			titleLower := strings.ToLower(c.item.Title)
			docLower := strings.ToLower(c.item.DocID)
			if strings.Contains(titleLower, rawQuery) || strings.Contains(docLower, rawQuery) {
				c.rrfRank += 0.03
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

// DeriveOrgFromWorkspace extracts organization slug from workspace name or doc path.
func DeriveOrgFromWorkspace(workspace, docID string) string {
	clean := strings.ToLower(strings.TrimSpace(workspace))
	if clean == "" {
		clean = strings.ToLower(lexical.DeriveWorkspaceFromPath(docID))
	}
	if clean == "" {
		return ""
	}
	parts := strings.Split(clean, "-")
	if len(parts) >= 2 && (parts[0] == "thedoctormes" || parts[0] == "doctormes") {
		return "thedoctormes-hue"
	}
	if len(parts) >= 1 && (parts[0] == "thenovanodes" || parts[0] == "nova") {
		return "thenovanodes"
	}
	return parts[0]
}

// FilterVectorHitsByOrg filters out vector hits not belonging to allowed orgs.
func FilterVectorHitsByOrg(hits []alm.VectorHit, allowedOrgs []string) []alm.VectorHit {
	if len(allowedOrgs) == 0 {
		return hits
	}
	filtered := make([]alm.VectorHit, 0, len(hits))
	for _, h := range hits {
		org := DeriveOrgFromWorkspace(h.Workspace, h.DocID)
		if isOrgAllowed(org, allowedOrgs) {
			filtered = append(filtered, h)
		}
	}
	return filtered
}

// FilterLexicalHitsByOrg filters out lexical hits not belonging to allowed orgs.
func FilterLexicalHitsByOrg(hits []lexical.LexicalHit, allowedOrgs []string) []lexical.LexicalHit {
	if len(allowedOrgs) == 0 {
		return hits
	}
	filtered := make([]lexical.LexicalHit, 0, len(hits))
	for _, h := range hits {
		org := DeriveOrgFromWorkspace(h.Workspace, h.DocID)
		if isOrgAllowed(org, allowedOrgs) {
			filtered = append(filtered, h)
		}
	}
	return filtered
}

func isOrgAllowed(org string, allowedOrgs []string) bool {
	if org == "" {
		return true
	}
	for _, a := range allowedOrgs {
		aClean := strings.ToLower(strings.TrimSpace(a))
		if aClean == "" || aClean == "*" {
			return true
		}
		if strings.EqualFold(org, aClean) || strings.Contains(org, aClean) || strings.Contains(aClean, org) {
			return true
		}
	}
	return false
}
