package lexical

// LexicalHit represents a single matched document from FTS5 full-text search.
type LexicalHit struct {
	DocID        string  `json:"doc_id"`
	Title        string  `json:"title"`
	Workspace    string  `json:"workspace,omitempty"`
	Text         string  `json:"text"`
	LexicalScore float64 `json:"lexical_score"`
}

// DocumentResult represents the full document payload returned by GetDocument.
type DocumentResult struct {
	DocID     string `json:"doc_id"`
	Title     string `json:"title"`
	Workspace string `json:"workspace,omitempty"`
	Content   string `json:"content"`
	Found     bool   `json:"found"`
}
