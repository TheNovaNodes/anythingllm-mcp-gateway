package alm

// VectorHit represents a single search result from AnythingLLM vector search.
type VectorHit struct {
	DocID       string  `json:"doc_id"`
	Title       string  `json:"title"`
	Workspace   string  `json:"workspace"`
	Text        string  `json:"text"`
	VectorScore float64 `json:"vector_score"`
}

// RawVectorResult represents the item inside AnythingLLM vector-search response.
type RawVectorResult struct {
	ID       string                 `json:"id"`
	Text     string                 `json:"text"`
	Score    float64                `json:"score"`
	Metadata map[string]interface{} `json:"metadata"`
}

// VectorSearchResponse represents the top-level envelope of /vector-search.
type VectorSearchResponse struct {
	Results []RawVectorResult `json:"results"`
}

// RawUploadDocument represents the document descriptor returned by /document/raw-text.
type RawUploadDocument struct {
	ID       string `json:"id"`
	Location string `json:"location"`
	Title    string `json:"title,omitempty"`
}

// RawUploadResponse represents the response envelope of /document/raw-text.
type RawUploadResponse struct {
	Success   bool                `json:"success"`
	Documents []RawUploadDocument `json:"documents"`
	Error     string              `json:"error,omitempty"`
}

// StoreResult represents the formatted output of store_memory.
type StoreResult struct {
	Success   bool   `json:"success"`
	DocID     string `json:"doc_id,omitempty"`
	Title     string `json:"title"`
	Location  string `json:"location,omitempty"`
	Workspace string `json:"workspace"`
	Tier      string `json:"tier,omitempty"`
	Error     string `json:"error,omitempty"`
}

// WorkspacesEnvelope represents /workspaces response.
type WorkspacesEnvelope struct {
	Workspaces []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"workspaces"`
}
