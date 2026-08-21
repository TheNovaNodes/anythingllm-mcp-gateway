"""Centralized configuration for memory-gateway.

All paths and timeouts are defined here. Values can be overridden via env vars
(prefix MG_) for deployment without modifying the code.
"""
import os

# ── AnythingLLM REST API (Official API only, Bearer) ──────────────
ALM_BASE = os.environ.get("MG_ALM_BASE", "http://127.0.0.1:3001/api/v1")

# ── Secrets (600) ──────────────────────────────────────────────────────
# System Bearer token for AnythingLLM vector-search.
TOKEN_FILE = os.environ.get("MG_TOKEN_FILE")
if not TOKEN_FILE and "MG_AUTH_TOKEN" in os.environ:
    # Legacy alias — remove in next major release
    TOKEN_FILE = os.environ.get("MG_AUTH_TOKEN")

# ── Operations Directory (Lexical layer) ─────────────────────────
_repo_root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OPS_DIR = os.environ.get("MG_OPS_DIR", os.path.join(_repo_root, "ops", "shared", "anythingllm-sync"))
# FTS5/BM25 index for .md corpus (read-only access).
LEXICAL_DB = os.environ.get("MG_LEXICAL_DB", os.path.join(OPS_DIR, "lexical.db"))
# Local workspace slugs map (faster and requires no /workspaces permissions).
MAP_FILE = os.environ.get("MG_MAP_FILE")
if not MAP_FILE:
    MAP_FILE = os.path.join(OPS_DIR, "workspace_map.json")

# ── Timeouts (sec) ─────────────────────────────────────────────────────
LIST_TIMEOUT = float(os.environ.get("MG_LIST_TIMEOUT", "15"))
SEARCH_TIMEOUT = float(os.environ.get("MG_SEARCH_TIMEOUT", "30"))

# ── Search ──────────────────────────────────────────────────────────────
DEFAULT_TOP_K = int(os.environ.get("MG_DEFAULT_TOP_K", "5"))
MAX_TOP_K = int(os.environ.get("MG_MAX_TOP_K", "25"))
# Vector layer cutoff threshold. Real scores distribution is bimodal:
# ~1.0 (exact match) or ~0.15 cluster (weak but relevant tail).
# 0.13 — slightly below tail, retains valid 0.15 matches while cutting noise.
VECTOR_SCORE_THRESHOLD = float(os.environ.get("MG_VECTOR_SCORE_THRESHOLD", "0.13"))
# Soft floor for lexical layer (BM25, higher = better). Cuts noise < 1.0.
LEXICAL_MIN_SCORE = float(os.environ.get("MG_LEXICAL_MIN_SCORE", "1.0"))
RRF_K = int(os.environ.get("MG_RRF_K", "60"))
QUERY_MAX_LEN = int(os.environ.get("MG_QUERY_MAX_LEN", "1000"))
# How many candidates to pull from each layer before fusion (recall > precision).
CANDIDATE_MULT = int(os.environ.get("MG_CANDIDATE_MULT", "3"))
# Max concurrent vector requests per workspace.
VECTOR_MAX_WORKERS = int(os.environ.get("MG_VECTOR_MAX_WORKERS", "6"))
# Max concurrent ALM calls per process (fan-out throttle, P4).
VECTOR_MAX_INFLIGHT = int(os.environ.get("MG_VECTOR_MAX_INFLIGHT", "4"))

# ── Fusion Strategy (P1: score-calibrated instead of pure RRF) ──────
# 'rrf'     — classic Reciprocal Rank Fusion (k=60), ignores absolute scores.
# 'weighted' — normalizes vector-cosine(0..1) and lexical-BM25(->0..1) to
#              a common scale + weighted sum α·vec+(1-α)·lex + combined threshold.
#              Removes noise when layers diverge (confirmed by eval NDCG@5 0.328->).
FUSION_MODE = os.environ.get("MG_FUSION_MODE", "weighted").lower()
# Weight of vector layer in weighted-fusion (1-α is lexical weight).
FUSION_VECTOR_WEIGHT = float(os.environ.get("MG_FUSION_VECTOR_WEIGHT", "0.6"))
# Combined threshold: result dropped if BOTH layers are weak
# (lexical-only noise with high BM25 for short tokens).
FUSION_MIN_COMBINED = float(os.environ.get("MG_FUSION_MIN_COMBINED", "0.05"))

# ── Context Assembly (Smart context merging) ────────────────────────
# If a relevant passage is found, the gateway expands it into a coherent block:
# pulls neighboring paragraphs of the same document (via full content from
# lexical.db / get_document), so the agent receives logically complete
# text instead of an isolated chunk cut off mid-sentence.
EXPAND_CONTEXT_DEFAULT = bool(int(os.environ.get("MG_EXPAND_CONTEXT_DEFAULT", "1")))
EXPAND_PARAGRAPHS = int(os.environ.get("MG_EXPAND_PARAGRAPHS", "1"))  # neighbors before/after
EXPAND_MAX_CHARS = int(os.environ.get("MG_EXPAND_MAX_CHARS", "4000"))  # hard limit
CONTEXT_ANCHOR_MIN = int(os.environ.get("MG_CONTEXT_ANCHOR_MIN", "30"))  # min anchor length

# ── Logs ───────────────────────────────────────────────────────────────
LOG_DIR = os.environ.get(
    "MG_LOG_DIR",
    os.path.join(os.path.dirname(os.path.dirname(__file__)), "logs"),
)
LOG_FILE = os.environ.get("MG_LOG_FILE", os.path.join(LOG_DIR, "memory-gateway.log"))
LOG_LEVEL = os.environ.get("MG_LOG_LEVEL", "INFO")

# ── MCP Transport ──────────────────────────────────────────────────────
# stdio — run as subprocess from agent config (recommended);
# streamable-http — for network deployments (systemd), port MG_PORT.
TRANSPORT = os.environ.get("MG_TRANSPORT", "stdio")
HOST = os.environ.get("MG_HOST", "127.0.0.1")
PORT = int(os.environ.get("MG_PORT", "8091"))
