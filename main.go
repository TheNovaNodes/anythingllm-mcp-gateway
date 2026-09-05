package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/alm"
	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/lexical"
	"github.com/TheNovaNodes/anythingllm-mcp-gateway/internal/server"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

func main() {
	almBase := getEnv("MG_ALM_BASE", getEnv("ANYTHINGLLM_BASE_URL", "http://127.0.0.1:3002/api/v1"))
	apiKey := getEnv("MG_API_KEY", getEnv("ANYTHINGLLM_API_KEY", ""))
	tokenFile := os.Getenv("MG_TOKEN_FILE")
	defaultWS := getEnv("MG_WORKSPACE", getEnv("MG_DEFAULT_WORKSPACE", "default"))
	mapFile := getEnv("MG_MAP_FILE", "")
	maxInflight := int64(getEnvInt("MG_VECTOR_MAX_INFLIGHT", 4))
	timeoutSec := getEnvInt("MG_SEARCH_TIMEOUT", 10)

	lexicalDBPath := getEnv("MG_LEXICAL_DB", "")
	lexicalMinScore := getEnvFloat("MG_LEXICAL_MIN_SCORE", 0.0)

	almClient := alm.NewClient(alm.ClientConfig{
		BaseURL:     almBase,
		APIKey:      apiKey,
		TokenFile:   tokenFile,
		DefaultWS:   defaultWS,
		MapFile:     mapFile,
		MaxInflight: maxInflight,
		Timeout:     time.Duration(timeoutSec) * time.Second,
	})

	lexDB, err := lexical.NewDB(lexicalDBPath, lexicalMinScore)
	if err != nil {
		log.Printf("WARNING: Failed to open lexical FTS5 database (%s): %v. Operating in degraded vector-only mode.", lexicalDBPath, err)
	} else if lexDB.IsAvailable() {
		log.Printf("Lexical FTS5 database connected: %s", lexicalDBPath)
		defer lexDB.Close()
	}

	cfg := server.Config{
		DefaultTopK:    getEnvInt("MG_DEFAULT_TOP_K", 5),
		MaxTopK:        getEnvInt("MG_MAX_TOP_K", 25),
		VectorScoreThr: getEnvFloat("MG_VECTOR_SCORE_THRESHOLD", 0.13),
		RRFK:           getEnvInt("MG_RRF_K", 60),
	}

	srv := server.NewServer(almClient, lexDB, cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("Starting anythingllm-gateway MCP server (ALM: %s, defaultWS: %s)", almClient.BaseURL(), defaultWS)

	if err := mcpserver.ServeStdio(srv.MCPServer()); err != nil {
		if ctx.Err() != nil {
			log.Println("MCP Server shut down cleanly.")
			return
		}
		fmt.Fprintf(os.Stderr, "FATAL: MCP Server terminated with error: %v\n", err)
		os.Exit(1)
	}
}
