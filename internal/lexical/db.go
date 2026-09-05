package lexical

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

var wordRegex = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// DB manages read-only queries to the SQLite FTS5 index.
type DB struct {
	dbPath          string
	db              *sql.DB
	minScore        float64
	hasWorkspaceCol bool
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

	// Detect if docs_fts has workspace column
	rows, err := db.Query("PRAGMA table_info(docs_fts)")
	if err == nil {
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dfltValue interface{}
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err == nil {
				if strings.EqualFold(name, "workspace") {
					d.hasWorkspaceCol = true
				}
			}
		}
		rows.Close()
	}

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

var compoundRegex = regexp.MustCompile(`[\p{L}\p{N}_\-\./]+`)

// BuildSafeFTSQuery converts arbitrary user search query into safe FTS5 MATCH expression.
// It splits compound words on hyphens, underscores, dots, and camelCase boundaries.
func BuildSafeFTSQuery(query string) string {
	rawTokens := compoundRegex.FindAllString(query, -1)
	if len(rawTokens) == 0 {
		return ""
	}

	seen := make(map[string]bool)
	tokens := make([]string, 0, len(rawTokens)*2)

	addToken := func(t string) {
		tClean := strings.TrimSpace(strings.ReplaceAll(t, "\"", ""))
		tClean = strings.Trim(tClean, "-_./")
		if utf8.RuneCountInString(tClean) >= 2 && !seen[strings.ToLower(tClean)] {
			seen[strings.ToLower(tClean)] = true
			tokens = append(tokens, fmt.Sprintf("\"%s\"", tClean))
		}
	}

	for _, m := range rawTokens {
		addToken(m)

		// Split on underscores, hyphens, slashes, or dots
		if strings.ContainsAny(m, "_-./") {
			parts := strings.FieldsFunc(m, func(r rune) bool {
				return r == '_' || r == '-' || r == '.' || r == '/'
			})
			for _, p := range parts {
				addToken(p)
				for _, sp := range splitCamelCase(p) {
					addToken(sp)
				}
			}
		} else {
			for _, sp := range splitCamelCase(m) {
				addToken(sp)
			}
		}
	}

	if len(tokens) == 0 {
		return ""
	}

	return strings.Join(tokens, " OR ")
}

// splitCamelCase splits camelCase/PascalCase words into constituent words.
// E.g. ChaCha20Poly1305 -> [ChaCha20, Poly1305]
func splitCamelCase(s string) []string {
	var words []string
	var current strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			// Only split if previous character was lowercase
			if unicode.IsLower(prev) {
				if current.Len() > 0 {
					words = append(words, current.String())
					current.Reset()
				}
			} else if i+1 < len(runes) && unicode.IsLower(runes[i+1]) && unicode.IsUpper(prev) {
				if current.Len() > 0 {
					words = append(words, current.String())
					current.Reset()
				}
			}
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	if len(words) <= 1 {
		return nil
	}
	return words
}

// Search performs FTS5 full-text BM25 search against docs_fts.
func (d *DB) Search(ctx context.Context, query, workspace string, topK int) ([]LexicalHit, error) {
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

	cleanWS := strings.ToLower(strings.TrimSpace(workspace))

	var rows *sql.Rows
	var err error

	if cleanWS != "" && d.hasWorkspaceCol {
		querySQL := `
			SELECT path, title, workspace, bm25(docs_fts, 5.0, 10.0, 0.0, 1.0) AS rank, snippet(docs_fts, -1, '⟨b⟩', '⟨/b⟩', '…', 12)
			FROM docs_fts
			WHERE docs_fts MATCH ? AND lower(workspace) = ?
			ORDER BY rank
			LIMIT ?
		`
		rows, err = d.db.QueryContext(ctx, querySQL, matchExpr, cleanWS, topK)
	} else if d.hasWorkspaceCol {
		querySQL := `
			SELECT path, title, workspace, bm25(docs_fts, 5.0, 10.0, 0.0, 1.0) AS rank, snippet(docs_fts, -1, '⟨b⟩', '⟨/b⟩', '…', 12)
			FROM docs_fts
			WHERE docs_fts MATCH ?
			ORDER BY rank
			LIMIT ?
		`
		rows, err = d.db.QueryContext(ctx, querySQL, matchExpr, topK)
	} else {
		querySQL := `
			SELECT path, title, '' AS workspace, bm25(docs_fts, 5.0, 10.0, 1.0) AS rank, snippet(docs_fts, -1, '⟨b⟩', '⟨/b⟩', '…', 12)
			FROM docs_fts
			WHERE docs_fts MATCH ?
			ORDER BY rank
			LIMIT ?
		`
		rows, err = d.db.QueryContext(ctx, querySQL, matchExpr, topK)
	}

	if err != nil {
		return nil, fmt.Errorf("lexical FTS5 query failed: %w", err)
	}
	defer rows.Close()

	var hits []LexicalHit
	for rows.Next() {
		var path, title, ws, snip string
		var rank float64
		if err := rows.Scan(&path, &title, &ws, &rank, &snip); err != nil {
			continue
		}

		if ws == "" {
			ws = DeriveWorkspaceFromPath(path)
		}

		if cleanWS != "" && !d.hasWorkspaceCol && !strings.EqualFold(ws, cleanWS) {
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
			Workspace:    ws,
			Text:         cleanSnip,
			LexicalScore: score,
		})
	}

	return hits, nil
}

// GetDocument retrieves the raw content of a document by path or within a specific workspace.
func (d *DB) GetDocument(ctx context.Context, docID, workspace string, maxChars int) (*DocumentResult, error) {
	if !d.IsAvailable() {
		return &DocumentResult{DocID: docID, Workspace: workspace, Found: false}, nil
	}

	if maxChars <= 0 {
		maxChars = 20000
	}

	cleanID := strings.TrimSpace(docID)
	if cleanID == "" {
		return &DocumentResult{Found: false}, nil
	}

	cleanWS := strings.ToLower(strings.TrimSpace(workspace))
	var path, title, ws, content string
	var err error

	// 1. Exact match by filesystem/relative path
	if d.hasWorkspaceCol {
		err = d.db.QueryRowContext(ctx, "SELECT path, title, workspace, content FROM docs_fts WHERE path = ? LIMIT 1", cleanID).Scan(&path, &title, &ws, &content)
	} else {
		err = d.db.QueryRowContext(ctx, "SELECT path, title, '' AS workspace, content FROM docs_fts WHERE path = ? LIMIT 1", cleanID).Scan(&path, &title, &ws, &content)
	}

	// 2. Scoped match within workspace
	if (err != nil || path == "") && cleanWS != "" {
		baseName := filepathBase(cleanID)
		baseStem := strings.TrimSuffix(baseName, filepath.Ext(baseName))

		if d.hasWorkspaceCol {
			err = d.db.QueryRowContext(ctx, `
				SELECT path, title, workspace, content FROM docs_fts 
				WHERE lower(workspace) = ? AND (path LIKE ? OR title LIKE ? OR title = ?) LIMIT 1
			`, cleanWS, "%/"+baseName, "%"+baseStem+"%", baseName).Scan(&path, &title, &ws, &content)
		} else {
			err = d.db.QueryRowContext(ctx, `
				SELECT path, title, '' AS workspace, content FROM docs_fts 
				WHERE path LIKE ? AND (path LIKE ? OR title = ?) LIMIT 1
			`, "%/"+cleanWS+"/%", "%/"+baseName, baseName).Scan(&path, &title, &ws, &content)
		}
	}

	// 3. Fallback when workspace is not specified
	if (err != nil || path == "") && cleanWS == "" {
		baseName := filepathBase(cleanID)
		if d.hasWorkspaceCol {
			err = d.db.QueryRowContext(ctx, "SELECT path, title, workspace, content FROM docs_fts WHERE path LIKE ? OR title = ? LIMIT 1", "%/"+baseName, cleanID).Scan(&path, &title, &ws, &content)
		} else {
			err = d.db.QueryRowContext(ctx, "SELECT path, title, '' AS workspace, content FROM docs_fts WHERE path LIKE ? OR title = ? LIMIT 1", "%/"+baseName, cleanID).Scan(&path, &title, &ws, &content)
		}
	}

	if err != nil {
		if err == sql.ErrNoRows {
			return &DocumentResult{DocID: docID, Workspace: workspace, Found: false}, nil
		}
		return nil, fmt.Errorf("failed to fetch document: %w", err)
	}

	if ws == "" {
		ws = DeriveWorkspaceFromPath(path)
	}

	runes := []rune(content)
	if len(runes) > maxChars {
		content = string(runes[:maxChars]) + "..."
	}

	return &DocumentResult{
		DocID:     path,
		Title:     title,
		Workspace: ws,
		Content:   content,
		Found:     true,
	}, nil
}

// DeriveWorkspaceFromPath extracts canonical workspace slug from host filesystem path.
func DeriveWorkspaceFromPath(path string) string {
	norm := strings.ReplaceAll(path, "\\", "/")
	parts := strings.Split(norm, "/")
	for i, part := range parts {
		if part == "projects" && i+2 < len(parts) {
			account := parts[i+1]
			repo := parts[i+2]
			return SlugFromRepo(account, repo)
		}
	}
	return ""
}

// SlugFromRepo converts organization and repo names to a normalized workspace slug.
func SlugFromRepo(account, repo string) string {
	accClean := strings.ToLower(strings.TrimSpace(account))
	repoClean := strings.ToLower(strings.TrimSpace(repo))
	combined := accClean + "-" + repoClean
	combined = strings.ReplaceAll(combined, "_", "-")
	combined = strings.ReplaceAll(combined, " ", "-")
	for strings.Contains(combined, "--") {
		combined = strings.ReplaceAll(combined, "--", "-")
	}
	return strings.Trim(combined, "-")
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
