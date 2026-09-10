package etl

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// LexicalIndexer manages the SQLite FTS5 full-text index database (lexical.db).
type LexicalIndexer struct {
	dbPath string
	db     *sql.DB
}

// NewLexicalIndexer opens or creates the SQLite FTS5 lexical database.
func NewLexicalIndexer(stateDir string) (*LexicalIndexer, error) {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state dir for lexical db: %w", err)
	}

	dbPath := filepath.Join(stateDir, "lexical.db")
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open lexical database %s: %w", dbPath, err)
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	idx := &LexicalIndexer{
		dbPath: dbPath,
		db:     db,
	}

	if err := idx.init(); err != nil {
		db.Close()
		return nil, err
	}

	return idx, nil
}

func (idx *LexicalIndexer) init() error {
	var hasWorkspace bool
	rows, err := idx.db.Query("PRAGMA table_info(docs_fts)")
	if err == nil {
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dfltValue interface{}
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err == nil {
				if strings.EqualFold(name, "workspace") {
					hasWorkspace = true
				}
			}
		}
		rows.Close()
	}

	if err == nil && !hasWorkspace {
		var exists int
		_ = idx.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='docs_fts'").Scan(&exists)
		if exists > 0 {
			if _, err := idx.db.Exec("DROP TABLE IF EXISTS docs_fts"); err != nil {
				return fmt.Errorf("failed to drop legacy docs_fts for migration: %w", err)
			}
		}
	}

	schema := `
	CREATE VIRTUAL TABLE IF NOT EXISTS docs_fts USING fts5(path, title, workspace UNINDEXED, content);
	`
	_, err = idx.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to initialize docs_fts schema: %w", err)
	}
	return nil
}

// Close closes the lexical database connection pool.
func (idx *LexicalIndexer) Close() error {
	if idx.db != nil {
		return idx.db.Close()
	}
	return nil
}

// Has checks if a document path or its chunks are already indexed in docs_fts.
func (idx *LexicalIndexer) Has(path string) (bool, error) {
	var count int
	err := idx.db.QueryRow("SELECT count(*) FROM docs_fts WHERE path = ? OR path LIKE ? LIMIT 1", path, path+"#%").Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Index inserts or replaces a document in docs_fts within a single atomic transaction.
func (idx *LexicalIndexer) Index(path, title, workspace, content string) error {
	tx, err := idx.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM docs_fts WHERE path = ?", path); err != nil {
		return fmt.Errorf("failed to delete old entry from docs_fts: %w", err)
	}

	if _, err := tx.Exec("INSERT INTO docs_fts(path, title, workspace, content) VALUES (?, ?, ?, ?)", path, title, workspace, content); err != nil {
		return fmt.Errorf("failed to insert document into docs_fts: %w", err)
	}

	return tx.Commit()
}

// Delete removes a document and any associated chunks from docs_fts.
func (idx *LexicalIndexer) Delete(path string) error {
	_, err := idx.db.Exec("DELETE FROM docs_fts WHERE path = ? OR path LIKE ?", path, path+"#%")
	if err != nil {
		return fmt.Errorf("failed to delete %s from docs_fts: %w", path, err)
	}
	return nil
}

// Count returns total indexed documents.
func (idx *LexicalIndexer) Count() (int, error) {
	var count int
	err := idx.db.QueryRow("SELECT count(*) FROM docs_fts").Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
