package alm

// VectorHit represents a single search result from AnythingLLM vector search.
type VectorHit struct {
	DocID       string  `json:"doc_id"`
	Title       string  `json:"title"`
	Workspace   string  `json:"workspace"`
	Text        string  `json:"text"`
	VectorScore float64 `json:"vector_score"`
	Tier        string  `json:"tier,omitempty"`
}

// RawVectorResult represents the item inside AnythingLLM vector-search response.
type RawVectorResult struct {
	ID       string                 `json:"id"`
	Text     string                 `json:"text"`
	Score    float64                `json:"score"`
	Distance float64                `json:"distance,omitempty"`
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


// WorkspacesEnvelope represents /workspaces response.
type WorkspacesEnvelope struct {
	Workspaces []Workspace `json:"workspaces"`
}

// WorkspacesResponse is an alias for WorkspacesEnvelope.
type WorkspacesResponse = WorkspacesEnvelope

// Workspace represents an AnythingLLM workspace object.
type Workspace struct {
	ID                  int                    `json:"id"`
	Name                string                 `json:"name"`
	Slug                string                 `json:"slug"`
	VectorTag           *string                `json:"vectorTag,omitempty"`
	CreatedAt           string                 `json:"createdAt,omitempty"`
	OpenAITemp          *float64               `json:"openAiTemp,omitempty"`
	OpenAIHistory       *int                   `json:"openAiHistory,omitempty"`
	LastUpdatedAt       string                 `json:"lastUpdatedAt,omitempty"`
	OpenAIPrompt        string                 `json:"openAiPrompt,omitempty"`
	SimilarityThreshold float64                `json:"similarityThreshold,omitempty"`
	TopN                int                    `json:"topN,omitempty"`
	ChatMode            string                 `json:"chatMode,omitempty"`
	VectorCount         int                    `json:"vectorCount,omitempty"`
	VectorsCount        int                    `json:"vectorsCount,omitempty"`
	Additional          map[string]interface{} `json:"-"`
}

// WorkspaceResponse represents the response envelope for a single workspace.
type WorkspaceResponse struct {
	Workspace Workspace `json:"workspace"`
	Message   string    `json:"message,omitempty"`
	Error     string    `json:"error,omitempty"`
}

