package lexical

// LexicalHit represents a single matched document from FTS5 full-text search.
type LexicalHit struct {
	DocID        string  `json:"doc_id"`
	Title        string  `json:"title"`
	Text         string  `json:"text"`
	LexicalScore float64 `json:"lexical_score"`
}

// DocumentResult represents the full document payload returned by GetDocument.
type DocumentResult struct {
	DocID   string `json:"doc_id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Found   bool   `json:"found"`
}
