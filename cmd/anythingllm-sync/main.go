package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/etl"
)

func main() {
	var (
		projectsDir string
		stateDir    string
		baseURL     string
		apiKey      string
		timeout     time.Duration
		interval    time.Duration
		once        bool
	)

	defaultProjects := os.Getenv("PROJECTS_DIR")
	if defaultProjects == "" {
		defaultProjects = "/root/projects"
	}

	defaultState := os.Getenv("STATE_DIR")
	if defaultState == "" {
		defaultState = "/root/projects/TheNovaNodes/ops/shared/anythingllm-sync"
	}

	defaultBaseURL := os.Getenv("ANYTHINGLLM_BASE_URL")
	if defaultBaseURL == "" {
		defaultBaseURL = os.Getenv("ALM_BASE")
	}
	if defaultBaseURL == "" {
		defaultBaseURL = "http://127.0.0.1:3002/api/v1"
	}

	defaultAPIKey := os.Getenv("ANYTHINGLLM_API_KEY")
	if defaultAPIKey == "" {
		defaultAPIKey = os.Getenv("MG_API_KEY")
	}

	flag.StringVar(&projectsDir, "projects", defaultProjects, "Path to projects directory to scan")
	flag.StringVar(&stateDir, "state-dir", defaultState, "Path to directory storing SQLite state ledger")
	flag.StringVar(&baseURL, "alm-base", defaultBaseURL, "AnythingLLM API base URL")
	flag.StringVar(&apiKey, "api-key", defaultAPIKey, "AnythingLLM Bearer API Key")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "HTTP request timeout per operation")
	flag.DurationVar(&interval, "interval", 0, "Daemon loop interval (0 or omitted runs once)")
	flag.BoolVar(&once, "once", false, "Force single execution run and exit")
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.Printf("Starting AnythingLLM Sync Daemon (Projects: %s, State: %s)", projectsDir, stateDir)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := etl.PipelineConfig{
		ProjectsDir: projectsDir,
		StateDir:    stateDir,
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Timeout:     timeout,
	}

	pipeline := etl.NewPipeline(cfg)

	// Execute first synchronization run
	if err := pipeline.Run(ctx); err != nil {
		log.Printf("ERROR: Pipeline run encountered an error: %v", err)
	}

	if once || interval <= 0 {
		log.Println("Single run completed. Exiting.")
		return
	}

	log.Printf("Entering continuous daemon mode (interval: %v)...", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Received shutdown signal. Sync daemon stopped cleanly.")
			return
		case <-ticker.C:
			log.Println("Triggering scheduled synchronization cycle...")
			if err := pipeline.Run(ctx); err != nil {
				log.Printf("ERROR: Pipeline run encountered an error: %v", err)
			}
		}
	}
}
