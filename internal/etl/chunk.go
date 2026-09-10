package etl

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/fusion"
)

const (
	// DefaultMaxChunkTokens defines the target maximum token budget for a single chunk (512 tokens).
	DefaultMaxChunkTokens = 512
	// DefaultChunkOverlapTokens defines the sliding window overlap between consecutive chunks (64 tokens).
	DefaultChunkOverlapTokens = 64
)

// Chunk represents a segmented passage of a parent document.
type Chunk struct {
	Index      int    `json:"index"`
	Total      int    `json:"total"`
	ChunkID    string `json:"chunk_id"`
	ParentPath string `json:"parent_path"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Hash       string `json:"hash"`
}

// ChunkDocument splits a document into semantically coherent passages.
// If the document is within maxTokens, it returns a single chunk without fragmentation.
func ChunkDocument(filePath, title, content string, maxTokens, overlapTokens int) []Chunk {
	cleanContent := strings.TrimSpace(content)
	if cleanContent == "" {
		return nil
	}

	if maxTokens <= 0 {
		maxTokens = DefaultMaxChunkTokens
	}
	if overlapTokens < 0 {
		overlapTokens = 0
	}
	if overlapTokens >= maxTokens {
		overlapTokens = maxTokens / 4
	}

	totalTokens := fusion.EstimateTokens(cleanContent)
	if totalTokens <= maxTokens {
		// Single chunk fast path
		chunkHash := computeChunkHash(filePath, 0, cleanContent)
		return []Chunk{
			{
				Index:      0,
				Total:      1,
				ChunkID:    filePath,
				ParentPath: filePath,
				Title:      title,
				Content:    cleanContent,
				Hash:       chunkHash,
			},
		}
	}

	// 1. Break into structural blocks: paragraphs, headers
	blocks := splitIntoBlocks(cleanContent)
	if len(blocks) == 0 {
		blocks = []string{cleanContent}
	}

	// 2. Aggregate blocks into chunks respecting maxTokens and overlapTokens
	type rawChunk struct {
		text string
	}
	var rawChunks []rawChunk

	var currentBlocks []string
	currentTokens := 0

	for _, block := range blocks {
		bTokens := fusion.EstimateTokens(block)
		if bTokens > maxTokens {
			// Subdivide large single block by sentence boundaries
			subBlocks := splitBlockBySentences(block, maxTokens)
			for _, sub := range subBlocks {
				subTok := fusion.EstimateTokens(sub)
				if currentTokens+subTok > maxTokens && len(currentBlocks) > 0 {
					rawChunks = append(rawChunks, rawChunk{text: strings.TrimSpace(strings.Join(currentBlocks, "\n\n"))})
					currentBlocks = getOverlapBlocks(currentBlocks, overlapTokens)
					currentTokens = 0
					for _, cb := range currentBlocks {
						currentTokens += fusion.EstimateTokens(cb)
					}
				}
				currentBlocks = append(currentBlocks, sub)
				currentTokens += subTok
			}
			continue
		}

		if currentTokens+bTokens > maxTokens && len(currentBlocks) > 0 {
			rawChunks = append(rawChunks, rawChunk{text: strings.TrimSpace(strings.Join(currentBlocks, "\n\n"))})
			currentBlocks = getOverlapBlocks(currentBlocks, overlapTokens)
			currentTokens = 0
			for _, cb := range currentBlocks {
				currentTokens += fusion.EstimateTokens(cb)
			}
		}

		currentBlocks = append(currentBlocks, block)
		currentTokens += bTokens
	}

	if len(currentBlocks) > 0 {
		rawChunks = append(rawChunks, rawChunk{text: strings.TrimSpace(strings.Join(currentBlocks, "\n\n"))})
	}

	totalChunks := len(rawChunks)
	chunks := make([]Chunk, totalChunks)
	for i, rc := range rawChunks {
		chunkID := fmt.Sprintf("%s#chunk-%d", filePath, i)
		chunkTitle := title
		if totalChunks > 1 {
			chunkTitle = fmt.Sprintf("%s (Part %d/%d)", title, i+1, totalChunks)
		}
		chunkHash := computeChunkHash(filePath, i, rc.text)
		chunks[i] = Chunk{
			Index:      i,
			Total:      totalChunks,
			ChunkID:    chunkID,
			ParentPath: filePath,
			Title:      chunkTitle,
			Content:    rc.text,
			Hash:       chunkHash,
		}
	}

	return chunks
}

func splitIntoBlocks(content string) []string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	rawParas := strings.Split(normalized, "\n\n")

	var blocks []string
	for _, p := range rawParas {
		pClean := strings.TrimSpace(p)
		if pClean == "" {
			continue
		}

		// Check if paragraph contains multiple markdown headers
		lines := strings.Split(pClean, "\n")
		var currentHeaderBlock []string

		for _, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			if strings.HasPrefix(trimmedLine, "#") && len(currentHeaderBlock) > 0 {
				blocks = append(blocks, strings.Join(currentHeaderBlock, "\n"))
				currentHeaderBlock = nil
			}
			currentHeaderBlock = append(currentHeaderBlock, line)
		}
		if len(currentHeaderBlock) > 0 {
			blocks = append(blocks, strings.Join(currentHeaderBlock, "\n"))
		}
	}

	return blocks
}

func splitBlockBySentences(block string, maxTokens int) []string {
	runes := []rune(block)
	n := len(runes)
	if n == 0 {
		return nil
	}

	var sentences []string
	start := 0

	for i := 0; i < n; i++ {
		r := runes[i]
		if (r == '.' || r == '!' || r == '?' || r == '\n') && i+1 < n && unicode.IsSpace(runes[i+1]) {
			sentence := strings.TrimSpace(string(runes[start : i+1]))
			if sentence != "" {
				sentences = append(sentences, sentence)
			}
			start = i + 1
		}
	}

	if start < n {
		tail := strings.TrimSpace(string(runes[start:]))
		if tail != "" {
			sentences = append(sentences, tail)
		}
	}

	if len(sentences) <= 1 {
		// Hard cut if no sentence delimiters found
		maxChars := int(float64(maxTokens) * 3.8)
		if maxChars <= 0 {
			maxChars = 1000
		}
		var parts []string
		for idx := 0; idx < n; idx += maxChars {
			end := idx + maxChars
			if end > n {
				end = n
			}
			parts = append(parts, string(runes[idx:end]))
		}
		return parts
	}

	return sentences
}

func getOverlapBlocks(blocks []string, overlapTokens int) []string {
	if overlapTokens <= 0 || len(blocks) == 0 {
		return nil
	}

	var overlap []string
	accumTokens := 0

	for i := len(blocks) - 1; i >= 0; i-- {
		bTok := fusion.EstimateTokens(blocks[i])
		overlap = append([]string{blocks[i]}, overlap...)
		accumTokens += bTok
		if accumTokens >= overlapTokens {
			break
		}
	}

	return overlap
}

func computeChunkHash(parentPath string, index int, text string) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%s:%d:%s", parentPath, index, text)))
	return hex.EncodeToString(h.Sum(nil))
}
