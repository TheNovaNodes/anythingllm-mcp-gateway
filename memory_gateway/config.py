"""Централизованная конфигурация memory-gateway.

Все пути и таймауты — здесь. Значения можно переопределить через env
(префикс MG_) для деплоя, не трогая код.
"""
import os

# ── AnythingLLM REST API (только официальный API, Bearer) ──────────────
ALM_BASE = os.environ.get("MG_ALM_BASE", "http://127.0.0.1:3002/api/v1")

# ── Секреты (600) ──────────────────────────────────────────────────────
# Системный Bearer-токен AnythingLLM для vector-search.
TOKEN_FILE = os.environ.get(
    "MG_TOKEN_FILE",
    "/root/LabDoctorM/vault/anythingllm_token.txt",
)

# ── Операционная директория (лексический слой) ─────────────────────────
OPS_DIR = os.environ.get(
    "MG_OPS_DIR", "/root/LabDoctorM/.ops/shared/anythingllm-sync"
)
# FTS5/BM25 индекс по .md-корпусу лаборатории (read-only доступ).
LEXICAL_DB = os.environ.get("MG_LEXICAL_DB", os.path.join(OPS_DIR, "lexical.db"))
# Локальный список слагов workspace (быстрее и без прав на /workspaces).
MAP_FILE = os.environ.get(
    "MG_MAP_FILE",
    "/root/LabDoctorM/vault/anythingllm_workspaces.map",
)

# ── Таймауты (сек) ─────────────────────────────────────────────────────
LIST_TIMEOUT = float(os.environ.get("MG_LIST_TIMEOUT", "15"))
SEARCH_TIMEOUT = float(os.environ.get("MG_SEARCH_TIMEOUT", "30"))

# ── Поиск ──────────────────────────────────────────────────────────────
DEFAULT_TOP_K = int(os.environ.get("MG_DEFAULT_TOP_K", "5"))
MAX_TOP_K = int(os.environ.get("MG_MAX_TOP_K", "25"))
# Порог отсечения векторного слоя. Распределение реальных скоров бимодально:
# ~1.0 (точное совпадение) либо кластер ~0.15 (слабый, но релевантный хвост).
# 0.13 — чуть ниже хвоста, чтобы не срезать легитимные 0.15, но отсекать мусор.
VECTOR_SCORE_THRESHOLD = float(os.environ.get("MG_VECTOR_SCORE_THRESHOLD", "0.13"))
# Лёгкий пол для лексического слоя (BM25, выше = лучше). Отсекает шум < 1.0.
LEXICAL_MIN_SCORE = float(os.environ.get("MG_LEXICAL_MIN_SCORE", "1.0"))
RRF_K = int(os.environ.get("MG_RRF_K", "60"))
QUERY_MAX_LEN = int(os.environ.get("MG_QUERY_MAX_LEN", "1000"))
# Сколько кандидатов тянуть из каждого слоя перед слиянием (recall > precision).
CANDIDATE_MULT = int(os.environ.get("MG_CANDIDATE_MULT", "3"))
# Ограничение параллельных vector-запросов по workspace.
VECTOR_MAX_WORKERS = int(os.environ.get("MG_VECTOR_MAX_WORKERS", "6"))
# Ограничение конкурентных вызовов ALM на процесс (fan-out throttle, P4).
VECTOR_MAX_INFLIGHT = int(os.environ.get("MG_VECTOR_MAX_INFLIGHT", "4"))

# ── Fusion-стратегия (P1: score-calibrated вместо чистого RRF) ──────
# 'rrf'     — классический Reciprocal Rank Fusion (k=60), игнорирует абс. скоры.
# 'weighted' — нормализация vector-cosine(0..1) и lexical-BM25(->0..1) в
#              общую шкалу + взвешенная сумма α·vec+(1-α)·lex + совокупный порог.
#              Убирает шум, когда слои расходятся (подтверждено eval NDCG@5 0.328->).
FUSION_MODE = os.environ.get("MG_FUSION_MODE", "weighted").lower()
# Вес векторного слоя в weighted-fusion (1-α — вес lexical).
FUSION_VECTOR_WEIGHT = float(os.environ.get("MG_FUSION_VECTOR_WEIGHT", "0.6"))
# Совокупный порог: результат отбрасывается, если ОБА слоя слабы
# (lexical-only шум с высоким BM25 по коротким токенам).
FUSION_MIN_COMBINED = float(os.environ.get("MG_FUSION_MIN_COMBINED", "0.05"))

# ── Context Assembly (умная склейка контекста) ────────────────────────
# Если найден релевантный пассаж, шлюз расширяет его до связного блока:
# подтягивает соседние абзацы того же документа (через полный content из
# lexical.db / get_document), чтобы агент получал логически завершённый
# текст, а не изолированный чанк, оборванный на полуслове.
EXPAND_CONTEXT_DEFAULT = bool(int(os.environ.get("MG_EXPAND_CONTEXT_DEFAULT", "1")))
EXPAND_PARAGRAPHS = int(os.environ.get("MG_EXPAND_PARAGRAPHS", "1"))  # соседей до/после
EXPAND_MAX_CHARS = int(os.environ.get("MG_EXPAND_MAX_CHARS", "4000"))  # жёсткий потолок
CONTEXT_ANCHOR_MIN = int(os.environ.get("MG_CONTEXT_ANCHOR_MIN", "30"))  # мин. длина якоря

# ── Логи ───────────────────────────────────────────────────────────────
LOG_DIR = os.environ.get(
    "MG_LOG_DIR",
    os.path.join(os.path.dirname(os.path.dirname(__file__)), "logs"),
)
LOG_FILE = os.environ.get("MG_LOG_FILE", os.path.join(LOG_DIR, "memory-gateway.log"))
LOG_LEVEL = os.environ.get("MG_LOG_LEVEL", "INFO")

# ── MCP-транспорт ──────────────────────────────────────────────────────
# stdio — для запуска как subprocess из конфига агента (рекомендуется);
# streamable-http — для сетевого деплоя (systemd), порт MG_PORT.
TRANSPORT = os.environ.get("MG_TRANSPORT", "stdio")
HOST = os.environ.get("MG_HOST", "127.0.0.1")
PORT = int(os.environ.get("MG_PORT", "8091"))
