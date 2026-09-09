package etl

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// TombstoneItem represents a deleted file that needs to be purged from AnythingLLM.
type TombstoneItem struct {
	FilePath    string
	DocLocation string
	Workspace   string
}

// Deduplicator tracks ingestion state in SQLite to guarantee idempotency and tombstone discovery.
type Deduplicator struct {
	dbPath string
	db     *sql.DB
}

// NewDeduplicator opens or creates the SQLite state ledger.
func NewDeduplicator(stateDir string) (*Deduplicator, error) {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state dir: %w", err)
	}

	dbPath := filepath.Join(stateDir, "etl_ledger.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open etl ledger database %s: %w", dbPath, err)
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	d := &Deduplicator{
		dbPath: dbPath,
		db:     db,
	}

	if err := d.initDB(); err != nil {
		db.Close()
		return nil, err
	}

	return d, nil
}

func (d *Deduplicator) initDB() error {
	schema := `
	CREATE TABLE IF NOT EXISTS processed_events (
		filepath TEXT PRIMARY KEY,
		last_mtime REAL NOT NULL,
		content_hash TEXT NOT NULL,
		doc_location TEXT,
		workspace TEXT,
		processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		status TEXT DEFAULT 'indexed'
	);
	`
	if _, err := d.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to initialize etl ledger schema: %w", err)
	}

	// Schema migration for legacy Python ledger
	rows, err := d.db.Query("PRAGMA table_info(processed_events)")
	if err != nil {
		return fmt.Errorf("failed to inspect processed_events columns: %w", err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
			cols[strings.ToLower(name)] = true
		}
	}

	if cols["conversation_id"] && !cols["filepath"] {
		if _, err := d.db.Exec("ALTER TABLE processed_events RENAME COLUMN conversation_id TO filepath"); err != nil {
			return fmt.Errorf("failed to rename conversation_id to filepath: %w", err)
		}
	}
	if !cols["doc_location"] {
		if _, err := d.db.Exec("ALTER TABLE processed_events ADD COLUMN doc_location TEXT"); err != nil {
			return fmt.Errorf("failed to add doc_location column: %w", err)
		}
	}
	if !cols["workspace"] {
		if _, err := d.db.Exec("ALTER TABLE processed_events ADD COLUMN workspace TEXT"); err != nil {
			return fmt.Errorf("failed to add workspace column: %w", err)
		}
	}

	if _, err := d.db.Exec("CREATE INDEX IF NOT EXISTS idx_processed_status ON processed_events(status)"); err != nil {
		return fmt.Errorf("failed to create status index: %w", err)
	}

	return nil
}

// Close closes the database connection pool.
func (d *Deduplicator) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// HashFile returns the SHA-256 hash of a file.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// ShouldProcess checks if the file has changed (O(1) mtime check followed by O(N) hash verification).
func (d *Deduplicator) ShouldProcess(filePath string, fileMtime float64) (bool, string, error) {
	var lastMtime float64
	var lastHash, status string
	err := d.db.QueryRow(
		"SELECT last_mtime, content_hash, status FROM processed_events WHERE filepath = ?",
		filePath,
	).Scan(&lastMtime, &lastHash, &status)

	if err == sql.ErrNoRows {
		// New file
		currentHash, err := HashFile(filePath)
		if err != nil {
			return false, "", err
		}
		return true, currentHash, nil
	} else if err != nil {
		return false, "", fmt.Errorf("ledger query failed: %w", err)
	}

	// If previously retracted, re-index
	if status == "retracted" {
		currentHash, err := HashFile(filePath)
		if err != nil {
			return false, "", err
		}
		return true, currentHash, nil
	}

	// O(1) fast path: mtime has not changed
	if fileMtime <= lastMtime {
		return false, lastHash, nil
	}

	// O(N) slow path: mtime changed, verify content hash
	currentHash, err := HashFile(filePath)
	if err != nil {
		return false, "", err
	}

	return (currentHash != lastHash), currentHash, nil
}

// MarkProcessed atomically updates the ledger on successful upload.
func (d *Deduplicator) MarkProcessed(filePath string, fileMtime float64, currentHash, docLocation, workspace string) error {
	query := `
	INSERT INTO processed_events (filepath, last_mtime, content_hash, doc_location, workspace, status, processed_at)
	VALUES (?, ?, ?, ?, ?, 'indexed', CURRENT_TIMESTAMP)
	ON CONFLICT(filepath) DO UPDATE SET
		last_mtime = excluded.last_mtime,
		content_hash = excluded.content_hash,
		doc_location = excluded.doc_location,
		workspace = excluded.workspace,
		status = 'indexed',
		processed_at = CURRENT_TIMESTAMP
	`
	_, err := d.db.Exec(query, filePath, fileMtime, currentHash, docLocation, workspace)
	if err != nil {
		return fmt.Errorf("failed to record processed file in ledger: %w", err)
	}
	return nil
}

// ProcessTombstones identifies files that were indexed but are no longer present on disk.
func (d *Deduplicator) ProcessTombstones(activePaths map[string]bool) ([]TombstoneItem, error) {
	rows, err := d.db.Query("SELECT filepath, doc_location, workspace FROM processed_events WHERE status = 'indexed'")
	if err != nil {
		return nil, fmt.Errorf("failed to query indexed records: %w", err)
	}
	defer rows.Close()

	var toRetract []TombstoneItem
	for rows.Next() {
		var path, loc, ws sql.NullString
		if err := rows.Scan(&path, &loc, &ws); err != nil {
			continue
		}
		if path.Valid && !activePaths[path.String] {
			toRetract = append(toRetract, TombstoneItem{
				FilePath:    path.String,
				DocLocation: loc.String,
				Workspace:   ws.String,
			})
		}
	}

	for _, item := range toRetract {
		_, _ = d.db.Exec(
			"UPDATE processed_events SET status = 'retracted', processed_at = CURRENT_TIMESTAMP WHERE filepath = ?",
			item.FilePath,
		)
	}

	return toRetract, nil
}
