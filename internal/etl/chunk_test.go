package etl

import (
	"strings"
	"testing"
)

func TestChunkDocument_EmptyAndSmall(t *testing.T) {
	// Empty
	if chunks := ChunkDocument("test.md", "test.md", "", 512, 64); chunks != nil {
		t.Fatalf("expected nil for empty content, got %d chunks", len(chunks))
	}

	// Small (within 512 tokens)
	smallText := "This is a brief readme document about the project."
	chunks := ChunkDocument("test.md", "test.md", smallText, 512, 64)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].ChunkID != "test.md" {
		t.Errorf("expected chunk ID 'test.md', got %s", chunks[0].ChunkID)
	}
	if chunks[0].Title != "test.md" {
		t.Errorf("expected title 'test.md', got %s", chunks[0].Title)
	}
	if chunks[0].Content != smallText {
		t.Errorf("expected content %q, got %q", smallText, chunks[0].Content)
	}
}

func TestChunkDocument_LargeMarkdown(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		sb.WriteString("## Section ")
		sb.WriteString(string(rune('0' + i)))
		sb.WriteString("\n\n")
		sb.WriteString(strings.Repeat("Кайрос и Хронос представляют две фундаментальные концепции времени в античной философии. ", 20))
		sb.WriteString("\n\n")
	}

	content := sb.String()
	// Using small maxTokens (100) to force multiple chunks
	chunks := ChunkDocument("/path/to/essay.md", "essay.md", content, 100, 20)
	if len(chunks) <= 1 {
		t.Fatalf("expected > 1 chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		if c.Index != i {
			t.Errorf("chunk %d has wrong index %d", i, c.Index)
		}
		if c.Total != len(chunks) {
			t.Errorf("chunk %d has wrong total %d, expected %d", i, c.Total, len(chunks))
		}
		if !strings.HasPrefix(c.ChunkID, "/path/to/essay.md#chunk-") {
			t.Errorf("chunk %d has wrong ID: %s", i, c.ChunkID)
		}
		if c.ParentPath != "/path/to/essay.md" {
			t.Errorf("chunk %d has wrong ParentPath: %s", i, c.ParentPath)
		}
		if !strings.Contains(c.Title, "(Part ") {
			t.Errorf("chunk %d title missing Part info: %s", i, c.Title)
		}
		if len(c.Content) == 0 {
			t.Errorf("chunk %d content is empty", i)
		}
		if len(c.Hash) == 0 {
			t.Errorf("chunk %d hash is empty", i)
		}
	}
}

func TestChunkDocument_PunctuationAndSentences(t *testing.T) {
	longPara := strings.Repeat("Sentence one. Sentence two! Sentence three? ", 50)
	chunks := ChunkDocument("long.md", "long.md", longPara, 60, 15)
	if len(chunks) <= 1 {
		t.Fatalf("expected > 1 chunks, got %d", len(chunks))
	}

	for _, c := range chunks {
		if len(c.Content) == 0 {
			t.Errorf("chunk content should not be empty")
		}
	}
}
