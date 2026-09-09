package etl

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeduplicator_Lifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "dedup_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	stateDir := filepath.Join(tempDir, "state")
	dedup, err := NewDeduplicator(stateDir)
	if err != nil {
		t.Fatalf("NewDeduplicator failed: %v", err)
	}
	defer dedup.Close()

	// 1. Create a dummy file
	testFile := filepath.Join(tempDir, "doc.md")
	if err := os.WriteFile(testFile, []byte("# Hello World"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	fi, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("failed to stat test file: %v", err)
	}
	mtime1 := float64(fi.ModTime().UnixNano()) / 1e9

	// 2. Initial check - ShouldProcess should be true
	needsProc, hash1, err := dedup.ShouldProcess(testFile, mtime1)
	if err != nil {
		t.Fatalf("ShouldProcess failed: %v", err)
	}
	if !needsProc || hash1 == "" {
		t.Errorf("expected needsProc=true with non-empty hash, got %v, %q", needsProc, hash1)
	}

	// 3. Mark processed
	err = dedup.MarkProcessed(testFile, mtime1, hash1, "loc-123", "ws-slug")
	if err != nil {
		t.Fatalf("MarkProcessed failed: %v", err)
	}

	// 4. Check again with same mtime - ShouldProcess should be false
	needsProc, _, err = dedup.ShouldProcess(testFile, mtime1)
	if err != nil {
		t.Fatalf("ShouldProcess failed: %v", err)
	}
	if needsProc {
		t.Error("expected needsProc=false for unchanged mtime, got true")
	}

	// 5. Update mtime without changing content - ShouldProcess should be false (content hash matches)
	mtime2 := mtime1 + 10.0
	needsProc, _, err = dedup.ShouldProcess(testFile, mtime2)
	if err != nil {
		t.Fatalf("ShouldProcess failed: %v", err)
	}
	if needsProc {
		t.Error("expected needsProc=false for changed mtime but identical content hash")
	}

	// 6. Update content and check - ShouldProcess should be true
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(testFile, []byte("# Hello World Updated"), 0644); err != nil {
		t.Fatalf("failed to update test file: %v", err)
	}
	fiUpdated, _ := os.Stat(testFile)
	mtime3 := float64(fiUpdated.ModTime().UnixNano()) / 1e9

	needsProc, hash3, err := dedup.ShouldProcess(testFile, mtime3)
	if err != nil {
		t.Fatalf("ShouldProcess failed: %v", err)
	}
	if !needsProc || hash3 == hash1 {
		t.Errorf("expected needsProc=true with new hash, got %v, hash3=%s", needsProc, hash3)
	}

	// Update ledger with new hash
	_ = dedup.MarkProcessed(testFile, mtime3, hash3, "loc-123-v2", "ws-slug")

	// 7. Process tombstones: activePaths contains testFile -> no tombstones
	tombstones, err := dedup.ProcessTombstones(map[string]bool{testFile: true})
	if err != nil {
		t.Fatalf("ProcessTombstones failed: %v", err)
	}
	if len(tombstones) != 0 {
		t.Errorf("expected 0 tombstones, got %d", len(tombstones))
	}

	// 8. Process tombstones: activePaths does NOT contain testFile -> tombstone generated
	tombstones, err = dedup.ProcessTombstones(map[string]bool{})
	if err != nil {
		t.Fatalf("ProcessTombstones failed: %v", err)
	}
	if len(tombstones) != 1 {
		t.Fatalf("expected 1 tombstone, got %d", len(tombstones))
	}
	if tombstones[0].FilePath != testFile || tombstones[0].DocLocation != "loc-123-v2" || tombstones[0].Workspace != "ws-slug" {
		t.Errorf("unexpected tombstone details: %+v", tombstones[0])
	}

	// 9. If file appears again after retraction -> ShouldProcess should be true
	needsProc, _, err = dedup.ShouldProcess(testFile, mtime3)
	if err != nil {
		t.Fatalf("ShouldProcess failed: %v", err)
	}
	if !needsProc {
		t.Error("expected needsProc=true for previously retracted file")
	}
}

func TestDeduplicator_Errors(t *testing.T) {
	// Missing file hash test
	h, err := HashFile("/path/to/nonexistent_file_12345.md")
	if err == nil || h != "" {
		t.Errorf("expected error on nonexistent file, got hash=%q, err=%v", h, err)
	}

	// Read-only directory or invalid DB path error handling
	tempFile, err := os.CreateTemp("", "invalid_db_dir_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())

	// Trying to create a directory where a regular file exists should fail
	_, err = NewDeduplicator(tempFile.Name())
	if err == nil {
		t.Error("expected error when creating state dir on existing file path")
	}
}

func TestDeduplicator_LegacyMigration(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "etl_ledger.sqlite")

	// Pre-create legacy table with conversation_id
	db, err := NewDeduplicator(tempDir)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	db.Close()

	// Drop and recreate with legacy schema
	rawDB, err := openRawDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open raw db: %v", err)
	}
	_, err = rawDB.db.Exec(`
		DROP TABLE processed_events;
		CREATE TABLE processed_events (
			conversation_id TEXT PRIMARY KEY,
			last_mtime REAL NOT NULL,
			content_hash TEXT NOT NULL,
			processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			status TEXT DEFAULT 'indexed'
		);
		INSERT INTO processed_events (conversation_id, last_mtime, content_hash)
		VALUES ('/path/doc.md', 123.456, 'hash123');
	`)
	rawDB.Close()
	if err != nil {
		t.Fatalf("failed to create legacy schema: %v", err)
	}

	// Reopen with NewDeduplicator which must trigger automatic migration
	migratedDedup, err := NewDeduplicator(tempDir)
	if err != nil {
		t.Fatalf("NewDeduplicator failed on legacy db: %v", err)
	}
	defer migratedDedup.Close()

	// Test tombstone processing without error
	tombstones, err := migratedDedup.ProcessTombstones(map[string]bool{})
	if err != nil {
		t.Fatalf("ProcessTombstones failed after migration: %v", err)
	}
	if len(tombstones) != 1 || tombstones[0].FilePath != "/path/doc.md" {
		t.Errorf("unexpected tombstones: %+v", tombstones)
	}
}

func openRawDB(dbPath string) (*Deduplicator, error) {
	return NewDeduplicator(filepath.Dir(dbPath))
}

