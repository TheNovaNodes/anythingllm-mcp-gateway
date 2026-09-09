package etl

import (
	"testing"
)

func TestLexicalIndexer_Lifecycle(t *testing.T) {
	tmpDir := t.TempDir()

	indexer, err := NewLexicalIndexer(tmpDir)
	if err != nil {
		t.Fatalf("NewLexicalIndexer failed: %v", err)
	}
	defer indexer.Close()

	// 1. Initially empty
	has, err := indexer.Has("repo/doc1.md")
	if err != nil {
		t.Fatalf("Has failed: %v", err)
	}
	if has {
		t.Errorf("expected doc1.md not to be indexed")
	}

	count, err := indexer.Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}

	// 2. Index a document
	err = indexer.Index("repo/doc1.md", "Doc 1", "ws-1", "This is the content of document 1 describing architecture.")
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	has, err = indexer.Has("repo/doc1.md")
	if err != nil {
		t.Fatalf("Has failed: %v", err)
	}
	if !has {
		t.Errorf("expected doc1.md to be indexed")
	}

	count, _ = indexer.Count()
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	// 3. Update existing document (upsert idempotency)
	err = indexer.Index("repo/doc1.md", "Doc 1 Updated", "ws-1", "Updated content for document 1.")
	if err != nil {
		t.Fatalf("Index update failed: %v", err)
	}

	count, _ = indexer.Count()
	if count != 1 {
		t.Errorf("expected count 1 after update, got %d", count)
	}

	// 4. Index a second document
	err = indexer.Index("repo/doc2.md", "Doc 2", "ws-2", "Another file with details.")
	if err != nil {
		t.Fatalf("Index second doc failed: %v", err)
	}

	count, _ = indexer.Count()
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}

	// 5. Delete a document
	err = indexer.Delete("repo/doc1.md")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	has, _ = indexer.Has("repo/doc1.md")
	if has {
		t.Errorf("expected doc1.md to be deleted")
	}

	count, _ = indexer.Count()
	if count != 1 {
		t.Errorf("expected count 1 after delete, got %d", count)
	}
}

func TestLexicalIndexer_Reopen(t *testing.T) {
	tmpDir := t.TempDir()

	indexer, err := NewLexicalIndexer(tmpDir)
	if err != nil {
		t.Fatalf("NewLexicalIndexer failed: %v", err)
	}

	if err := indexer.Index("path/a.md", "A", "ws-a", "Content A"); err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	indexer.Close()

	// Reopen same directory
	indexer2, err := NewLexicalIndexer(tmpDir)
	if err != nil {
		t.Fatalf("Reopening NewLexicalIndexer failed: %v", err)
	}
	defer indexer2.Close()

	has, err := indexer2.Has("path/a.md")
	if err != nil || !has {
		t.Errorf("expected path/a.md to persist across reopens, has=%v, err=%v", has, err)
	}
}

func TestLexicalIndexer_MigrationFromLegacySchema(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a database with legacy schema (without workspace column)
	legacyIndexer, err := NewLexicalIndexer(tmpDir)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}
	// Force recreate legacy table without workspace
	if _, err := legacyIndexer.db.Exec("DROP TABLE IF EXISTS docs_fts; CREATE VIRTUAL TABLE docs_fts USING fts5(path, title, content);"); err != nil {
		t.Fatalf("failed to create legacy table: %v", err)
	}
	legacyIndexer.Close()

	// Now open with NewLexicalIndexer, which should detect missing workspace and migrate
	migratedIndexer, err := NewLexicalIndexer(tmpDir)
	if err != nil {
		t.Fatalf("NewLexicalIndexer failed on migration: %v", err)
	}
	defer migratedIndexer.Close()

	if err := migratedIndexer.Index("path/migrated.md", "Migrated", "ws-migrated", "Migrated content"); err != nil {
		t.Fatalf("Index failed after migration: %v", err)
	}

	has, err := migratedIndexer.Has("path/migrated.md")
	if err != nil || !has {
		t.Errorf("expected path/migrated.md to be indexed after migration")
	}
}
