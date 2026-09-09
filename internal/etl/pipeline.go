package etl

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/alm"
)

// PipelineConfig configures the ETL sync pipeline.
type PipelineConfig struct {
	ProjectsDir string
	StateDir    string
	BaseURL     string
	APIKey      string
	Timeout     time.Duration
}

// Pipeline coordinates filesystem scanning, deduplication, and AnythingLLM vector sync.
type Pipeline struct {
	cfg       PipelineConfig
	almClient *alm.Client
}

// NewPipeline creates a new ETL sync pipeline.
func NewPipeline(cfg PipelineConfig) *Pipeline {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	almClient := alm.NewClient(alm.ClientConfig{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Timeout: cfg.Timeout,
	})
	return &Pipeline{
		cfg:       cfg,
		almClient: almClient,
	}
}

// Run executes a complete ETL synchronization cycle with tombstone cleanup.
func (p *Pipeline) Run(ctx context.Context) error {
	log.Printf("Starting AnythingLLM ETL Sync Pipeline (ProjectsDir: %s)", p.cfg.ProjectsDir)

	deduper, err := NewDeduplicator(p.cfg.StateDir)
	if err != nil {
		return fmt.Errorf("failed to initialize deduplicator: %w", err)
	}
	defer deduper.Close()

	indexer, err := NewLexicalIndexer(p.cfg.StateDir)
	if err != nil {
		return fmt.Errorf("failed to initialize lexical indexer: %w", err)
	}
	defer indexer.Close()

	if _, err := os.Stat(p.cfg.ProjectsDir); os.IsNotExist(err) {
		return fmt.Errorf("projects directory %s does not exist", p.cfg.ProjectsDir)
	}

	activeFiles := make(map[string]bool)
	accounts, err := os.ReadDir(p.cfg.ProjectsDir)
	if err != nil {
		return fmt.Errorf("failed to read projects directory: %w", err)
	}

	syncedCount := 0
	lexicalBackfilledCount := 0
	skippedCount := 0

	for _, acc := range accounts {
		if !acc.IsDir() || strings.HasPrefix(acc.Name(), ".") {
			continue
		}
		accountPath := filepath.Join(p.cfg.ProjectsDir, acc.Name())
		repos, err := os.ReadDir(accountPath)
		if err != nil {
			continue
		}

		for _, repo := range repos {
			if !repo.IsDir() || strings.HasPrefix(repo.Name(), ".") {
				continue
			}
			if IsBlacklisted(repo.Name()) || IsSnapshotRepo(repo.Name()) {
				continue
			}
			repoPath := filepath.Join(accountPath, repo.Name())
			slug := SlugFromRepo(acc.Name(), repo.Name())

			var mdFiles []string
			_ = filepath.WalkDir(repoPath, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				rel, err := filepath.Rel(repoPath, path)
				if err != nil {
					return nil
				}
				if rel == "." {
					return nil
				}
				if d.IsDir() {
					if IsBlacklisted(rel) {
						return filepath.SkipDir
					}
					return nil
				}
				if !IsBlacklisted(rel) && strings.EqualFold(filepath.Ext(path), ".md") {
					mdFiles = append(mdFiles, path)
				}
				return nil
			})

			if len(mdFiles) == 0 {
				continue
			}

			// Ensure workspace exists
			wsSlug, err := p.ensureWorkspace(ctx, slug)
			if err != nil {
				log.Printf("ERROR: Could not ensure workspace '%s': %v. Skipping repo %s.", slug, err, repo.Name())
				continue
			}

			for _, path := range mdFiles {
				activeFiles[path] = true
				fi, err := os.Stat(path)
				if err != nil {
					continue
				}

				mtime := float64(fi.ModTime().UnixNano()) / 1e9
				needsProc, currentHash, err := deduper.ShouldProcess(path, mtime)
				if err != nil {
					log.Printf("ERROR: Error checking %s in ledger: %v", path, err)
					continue
				}

				hasLex, err := indexer.Has(path)
				if err != nil {
					hasLex = false
				}

				if !needsProc && hasLex {
					skippedCount++
					continue
				}

				contentBytes, err := os.ReadFile(path)
				if err != nil {
					log.Printf("ERROR: Failed to read %s: %v", path, err)
					continue
				}
				contentStr := string(contentBytes)
				title := filepath.Base(path)

				// 1. Maintain SQLite FTS5 lexical index
				if !hasLex || needsProc {
					if err := indexer.Index(path, title, wsSlug, contentStr); err != nil {
						log.Printf("ERROR: Failed to index %s into lexical.db: %v", path, err)
					} else if !needsProc {
						lexicalBackfilledCount++
					}
				}

				// 2. Vector indexing via AnythingLLM API
				if needsProc {
					log.Printf("Syncing %s -> Workspace: %s", path, wsSlug)
					location, err := p.uploadDocument(ctx, wsSlug, path, contentStr)
					if err != nil {
						log.Printf("ERROR: Failed to upload %s: %v", path, err)
						continue
					}

					if err := deduper.MarkProcessed(path, mtime, currentHash, location, wsSlug); err != nil {
						log.Printf("WARNING: Failed to record %s in ledger: %v", path, err)
					}
					syncedCount++
				}
			}
		}
	}

	// Tombstone purge: remove deleted files from AnythingLLM and lexical index
	tombstones, err := deduper.ProcessTombstones(activeFiles)
	if err != nil {
		log.Printf("WARNING: Tombstone query failed: %v", err)
	} else {
		for _, t := range tombstones {
			if err := indexer.Delete(t.FilePath); err != nil {
				log.Printf("WARNING: Failed to delete %s from lexical index: %v", t.FilePath, err)
			}
			if t.DocLocation != "" && t.Workspace != "" {
				log.Printf("Purging tombstone: %s (Location: %s) from Workspace: %s", t.FilePath, t.DocLocation, t.Workspace)
				if err := p.almClient.UpdateEmbeddings(ctx, t.Workspace, nil, []string{t.DocLocation}); err != nil {
					log.Printf("WARNING: Failed to purge embedding for %s: %v", t.FilePath, err)
				}
			}
		}
	}

	log.Printf("ETL Sync Pipeline completed: %d synced, %d lexical backfilled, %d up-to-date, %d tombstones purged.", syncedCount, lexicalBackfilledCount, skippedCount, len(tombstones))
	return nil
}

func (p *Pipeline) ensureWorkspace(ctx context.Context, targetSlug string) (string, error) {
	workspaces, err := p.almClient.ListWorkspaces(ctx)
	if err == nil {
		for _, w := range workspaces {
			if strings.EqualFold(w.Slug, targetSlug) || strings.EqualFold(w.Name, targetSlug) {
				return w.Slug, nil
			}
		}
	}

	// Create workspace if missing
	ws, err := p.almClient.CreateWorkspace(ctx, targetSlug)
	if err != nil {
		return "", err
	}
	return ws.Slug, nil
}

func (p *Pipeline) uploadDocument(ctx context.Context, slug, filePath, content string) (string, error) {
	metaHeader := fmt.Sprintf("<document_metadata>\nsource_path: %s\ngenerated_at: %s\n</document_metadata>\n\n", filePath, time.Now().UTC().Format(time.RFC3339))
	fullText := metaHeader + content

	location, err := p.almClient.UploadRawText(ctx, fullText, map[string]interface{}{
		"title": filepath.Base(filePath),
	})
	if err != nil {
		return "", err
	}

	if err := p.almClient.UpdateEmbeddings(ctx, slug, []string{location}, nil); err != nil {
		return location, err
	}

	return location, nil
}
