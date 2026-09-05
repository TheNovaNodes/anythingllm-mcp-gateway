package lexical

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

var wordRegex = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// DB manages read-only queries to the SQLite FTS5 index.
type DB struct {
	dbPath   string
	db       *sql.DB
	minScore float64
}

// NewDB initializes a read-only SQLite connection to the FTS5 database.
func NewDB(dbPath string, minScore float64) (*DB, error) {
	if minScore < 0 {
		minScore = 0.0
	}

	d := &DB{
		dbPath:   dbPath,
		minScore: minScore,
	}

	if dbPath == "" || !fileExists(dbPath) {
		return d, nil // DB is optional; degraded mode if absent
	}

	dsn := fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database %s: %w", dbPath, err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	d.db = db
	return d, nil
}

// IsAvailable checks if the lexical database is open and accessible.
func (d *DB) IsAvailable() bool {
	return d.db != nil && fileExists(d.dbPath)
}

// Close terminates database connections.
func (d *DB) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// BuildSafeFTSQuery converts arbitrary user search query into safe FTS5 MATCH expression.
func BuildSafeFTSQuery(query string) string {
	matches := wordRegex.FindAllString(query, -1)
	if len(matches) == 0 {
		return ""
	}

	tokens := make([]string, 0, len(matches))
	for _, m := range matches {
		if utf8.RuneCountInString(m) >= 2 {
			// Escape quotes and wrap in double quotes to prevent FTS5 keyword collision (OR, AND, NOT)
			clean := strings.ReplaceAll(m, "\"", "")
			tokens = append(tokens, fmt.Sprintf("\"%s\"", clean))
		}
	}

	if len(tokens) == 0 {
		return ""
	}

	return strings.Join(tokens, " OR ")
}

// Search performs FTS5 full-text BM25 search against docs_fts.
func (d *DB) Search(ctx context.Context, query string, topK int) ([]LexicalHit, error) {
	if !d.IsAvailable() {
		return nil, nil
	}

	matchExpr := BuildSafeFTSQuery(query)
	if matchExpr == "" {
		return nil, nil
	}

	if topK <= 0 {
		topK = 5
	}

	querySQL := `
		SELECT path, title, bm25(docs_fts) AS rank, snippet(docs_fts, -1, '⟨b⟩', '⟨/b⟩', '…', 12)
		FROM docs_fts
		WHERE docs_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`

	rows, err := d.db.QueryContext(ctx, querySQL, matchExpr, topK)
	if err != nil {
		return nil, fmt.Errorf("lexical FTS5 query failed: %w", err)
	}
	defer rows.Close()

	var hits []LexicalHit
	for rows.Next() {
		var path, title, snip string
		var rank float64
		if err := rows.Scan(&path, &title, &rank, &snip); err != nil {
			continue
		}

		score := -rank // in SQLite FTS5 bm25, rank is negative, so -rank makes higher=better
		if score < d.minScore {
			continue
		}

		cleanSnip := strings.ReplaceAll(snip, "⟨b⟩", "")
		cleanSnip = strings.ReplaceAll(cleanSnip, "⟨/b⟩", "")
		cleanSnip = strings.TrimSpace(cleanSnip)

		hits = append(hits, LexicalHit{
			DocID:        path,
			Title:        title,
			Text:         cleanSnip,
			LexicalScore: score,
		})
	}

	return hits, nil
}

// GetDocument retrieves the raw content of a document by path or title.
func (d *DB) GetDocument(ctx context.Context, docID string, maxChars int) (*DocumentResult, error) {
	if !d.IsAvailable() {
		return &DocumentResult{DocID: docID, Found: false}, nil
	}

	if maxChars <= 0 {
		maxChars = 20000
	}

	cleanID := strings.TrimSpace(docID)
	if cleanID == "" {
		return &DocumentResult{Found: false}, nil
	}

	// 1. Exact match by path
	row := d.db.QueryRowContext(ctx, "SELECT path, title, content FROM docs_fts WHERE path = ? LIMIT 1", cleanID)
	var path, title, content string
	err := row.Scan(&path, &title, &content)
	if err != nil && err == sql.ErrNoRows {
		// 2. Fallback: match by basename suffix
		baseName := filepathBase(cleanID)
		row = d.db.QueryRowContext(ctx, "SELECT path, title, content FROM docs_fts WHERE path LIKE ? OR title = ? LIMIT 1", "%/"+baseName, cleanID)
		err = row.Scan(&path, &title, &content)
	}

	if err != nil {
		if err == sql.ErrNoRows {
			return &DocumentResult{DocID: docID, Found: false}, nil
		}
		return nil, fmt.Errorf("failed to fetch document: %w", err)
	}

	runes := []rune(content)
	if len(runes) > maxChars {
		content = string(runes[:maxChars]) + "..."
	}

	return &DocumentResult{
		DocID:   path,
		Title:   title,
		Content: content,
		Found:   true,
	}, nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Size() > 0
}

func filepathBase(path string) string {
	parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
	return parts[len(parts)-1]
}
