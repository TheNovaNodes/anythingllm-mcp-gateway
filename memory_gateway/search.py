"""Гибридный retrieval: vector (AnythingLLM) + lexical (FTS5/BM25) + RRF.

Контракт: только сырые данные. Никаких /chat, никаких LLM-синтезов.
Оба слоя выполняются параллельно; результаты объединяются Reciprocal Rank
Fusion (RRF) с дедупликацией по канонической цели документа.
"""
import concurrent.futures
import json
import os
import re
import sqlite3
import threading
import time
import urllib.parse
from typing import Any

import requests

from . import config
from .logger import get_logger

log = get_logger()

# P4: throttle concurrent ALM calls (fan-out protection).
# One semaphore per process: caps simultaneous /vector-search to AnythingLLM.
_ALM_SEM = threading.Semaphore(max(1, int(config.VECTOR_MAX_INFLIGHT)))
# Lightweight ALM latency telemetry (last/p50/p95 ms), thread-safe.
_ALM_LATENCY = []
_ALM_LAT_LOCK = threading.Lock()
_ALM_LATENCY_MAX = 64

# Маркеры подсветки FTS5-сниппета (〈b〉…〈/b〉). Убираем при извлечении якоря.
SNIP_OPEN = "\u27e8b\u27e9"
SNIP_CLOSE = "\u27e8/b\u27e9"

# AnythingLLM (utils/TextSplitter/index.js:145) заворачивает каждый чанк в
# <document_metadata>...</document_metadata> и добавляет e5-префикс
# passage:/query: при эмбеддинге. Это серверная обёртка — во входе sync.py
# её нет, поэтому вырезаем на стороне выдачи шлюза, чтобы агент получал
# чистый текст без служебного XML-подобного мусора.
_DOC_META_RE = re.compile(r"<document_metadata>.*?</document_metadata>", re.DOTALL | re.IGNORECASE)
_CHUNK_PREFIX_RE = re.compile(r"^\s*(passage|query|search_document|search_query)\s*:\s*", re.IGNORECASE)


def _clean_text(text: str) -> str:
    """Снимает серверные обёртки AnythingLLM: metadata-блок + e5-префикс."""
    if not text:
        return text
    t = _DOC_META_RE.sub("", text)
    t = _CHUNK_PREFIX_RE.sub("", t)
    t = t.replace("\u2026", " ")  # FTS5 snippet-разделитель
    return re.sub(r"[ \t]+\n", "\n", t).strip()

# ── Секрет: кэшируем токен в памяти, читаем один раз ────────────────────
_token_cache: str | None = None
_token_file_mtime: float = 0.0
_token_lock = threading.Lock()


def invalidate_token_cache() -> None:
    """Сбрасывает кэш Bearer-токена в памяти (Issue #16)."""
    global _token_cache, _token_file_mtime
    with _token_lock:
        _token_cache = None
        _token_file_mtime = 0.0
        log.info("Bearer token cache invalidated")


def load_token(force_reload: bool = False) -> str:
    """Читает Bearer-токен из переменной окружения или TOKEN_FILE (600). Кэширует в памяти."""
    global _token_cache, _token_file_mtime
    with _token_lock:
        token_env = (
            getattr(config, "TOKEN_RAW", None)
            or os.environ.get("ANYTHINGLLM_API_KEY")
            or os.environ.get("MG_API_KEY")
            or os.environ.get("MG_AUTH_TOKEN")
        )
        path = config.TOKEN_FILE or (token_env if token_env and os.path.exists(token_env) else None)

        # Check if token file mtime changed
        if path and os.path.exists(path):
            try:
                mtime = os.path.getmtime(path)
                if mtime > _token_file_mtime:
                    _token_cache = None
            except OSError:
                pass

        if _token_cache and not force_reload:
            return _token_cache

        # 1. Direct env variable token
        if token_env and not (os.path.isabs(token_env) and os.path.exists(token_env)):
            _token_cache = token_env.strip()
            return _token_cache

        # 2. File path token
        if not path:
            raise RuntimeError("Neither API Key nor MG_TOKEN_FILE environment variable is configured")
        if not os.path.exists(path):
            raise RuntimeError(f"token file not found: {path}")
        # Мягкая проверка прав: секрет не должен быть мир-читаемым.
        try:
            mode = os.stat(path).st_mode & 0o777
            if mode & 0o077:
                log.warning("token file %s has loose perms %o (expect 600)", path, mode)
        except OSError:
            pass
        with open(path, "r", encoding="utf-8") as f:
            tok = f.read().strip()
        if not tok:
            raise RuntimeError(f"token file empty: {path}")
        try:
            _token_file_mtime = os.path.getmtime(path)
        except OSError:
            _token_file_mtime = 0.0
        _token_cache = tok
        return tok



# ── Список слагов workspace ─────────────────────────────────────────────
def workspace_slugs() -> list[str]:
    """Локальный список слагов из MAP_FILE (быстро, без /workspaces).
    Fallback: официальный GET /workspaces."""
    slugs: list[str] = []
    if os.path.exists(config.MAP_FILE):
        try:
            with open(config.MAP_FILE, "r", encoding="utf-8") as f:
                content = f.read().strip()
                if content.startswith("{") or content.startswith("["):
                    data = json.loads(content)
                    if isinstance(data, dict):
                        slugs = list(data.keys())
                    elif isinstance(data, list):
                        slugs = [item["slug"] if isinstance(item, dict) and "slug" in item else str(item) for item in data]
                else:
                    for line in content.splitlines():
                        line = line.strip()
                        if not line or line.startswith("#"):
                            continue
                        parts = line.split()
                        slugs.append(parts[1] if len(parts) >= 2 else parts[0])
        except Exception as e:
            log.warning("map read failed: %s", e)
    if slugs:
        return sorted(set(slugs))
    # fallback: официальный API
    try:
        tok = load_token()
        r = requests.get(
            f"{config.ALM_BASE}/workspaces",
            headers={"Authorization": f"Bearer {tok}"},
            timeout=config.LIST_TIMEOUT,
        )
        r.raise_for_status()
        return sorted({w["slug"] for w in r.json().get("workspaces", [])})
    except Exception as e:  # noqa: BLE001
        log.error("workspace list failed (map+api): %s", e)
        return []


# ── D1: workspace-aware routing ─────────────────────────────────────────
_WORKSPACE_MAP: list[dict[str, Any]] | None = None

def _load_workspace_map() -> list[dict[str, Any]]:
    """Load workspace_map.json once, cached in module memory."""
    global _WORKSPACE_MAP
    if _WORKSPACE_MAP is not None:
        return _WORKSPACE_MAP
    try:
        _map_path = os.environ.get(
            "MG_MAP_FILE",
            os.path.join(config.OPS_DIR, "workspace_map.json"),
        )
        with open(_map_path, "r", encoding="utf-8") as f:
            _WORKSPACE_MAP = json.load(f)
    except Exception as e:
        log.warning("D1: failed to load workspace_map.json: %s", e)
        _WORKSPACE_MAP = []
    return _WORKSPACE_MAP


def workspace_slugs_for_query(query: str) -> list[str]:
    """D1: Return workspace slugs whose topics match the query.

    For each workspace in workspace_map.json, checks if any of its "topics"
    keywords appear as case-insensitive substrings in the query.
    If no match found, falls back to all slugs.
    """
    if not query:
        return workspace_slugs()
    q_lower = query.lower()
    wmap = _load_workspace_map()
    if not wmap:
        return workspace_slugs()
    # workspace_map.json format: dict {slug: {"topics": [...], "source": ...}}.
    # Legacy/defensive: also accept list of {"slug":..., "topics":...} dicts.
    if isinstance(wmap, dict):
        entries = [
            {"slug": slug, "topics": (meta or {}).get("topics", [])
                if isinstance(meta, dict) else []}
            for slug, meta in wmap.items()
        ]
    else:
        entries = [ws for ws in wmap if isinstance(ws, dict)]
    matched = []
    for ws in entries:
        slug = ws.get("slug")
        if not slug:
            continue
        topics = ws.get("topics", []) or []
        if any(str(topic).lower() in q_lower for topic in topics):
            matched.append(slug)
    if matched:
        return sorted(set(matched))
    # fallback: all slugs
    return workspace_slugs()



# ── Vector-слой (AnythingLLM /vector-search) ────────────────────────────
def _vector_search_one(slug: str, query: str, top_k: int, threshold: float, retry_auth: bool = True) -> list[dict[str, Any]]:
    tok = load_token()
    try:
        t0 = time.monotonic()
        with _ALM_SEM:
            r = requests.post(
                f"{config.ALM_BASE}/workspace/{urllib.parse.quote(slug, safe='')}/vector-search",
                headers={"Authorization": f"Bearer {tok}", "Content-Type": "application/json"},
                json={"query": query, "topN": top_k, "scoreThreshold": threshold},
                timeout=config.SEARCH_TIMEOUT,
            )
            dt = (time.monotonic() - t0) * 1000.0
            with _ALM_LAT_LOCK:
                _ALM_LATENCY.append(dt)
                if len(_ALM_LATENCY) > _ALM_LATENCY_MAX:
                    del _ALM_LATENCY[: len(_ALM_LATENCY) - _ALM_LATENCY_MAX]

        # Issue #16: Invalidate token and retry once on 401/403 (token expiration)
        if r.status_code in (401, 403) and retry_auth:
            log.warning(
                "vector-search %s received HTTP %s (token expired/invalid). Invalidating token cache and retrying...",
                slug,
                r.status_code,
            )
            invalidate_token_cache()
            return _vector_search_one(slug, query, top_k, threshold, retry_auth=False)

        if r.status_code != 200:
            log.warning("vector-search %s -> HTTP %s", slug, r.status_code)
            return []
        out = []
        for res in r.json().get("results", []):
            if not isinstance(res, dict):
                log.warning("vector-search %s: unexpected result type %s",
                            slug, type(res).__name__)
                continue
            meta = res.get("metadata", {}) or {}
            title = meta.get("title") or meta.get("docSource") or "?"
            out.append({
                "source": "vector",
                "workspace": slug,
                "title": title,
                "doc_id": _doc_id_from_meta(meta, title),
                "text": _clean_text((res.get("text") or "")[:2000]),
                "vector_score": float(res.get("score", 0.0)),
            })
        return out
    except requests.Timeout:
        log.warning("vector-search %s timeout", slug)
        return []
    except Exception as e:  # noqa: BLE001
        log.warning("vector-search %s error: %s", slug, e)
        return []


def _doc_id_from_meta(meta: dict[str, Any], title: str) -> str:
    """Каноническая идентификация документа для дедупликации/get_document.
    Приоритет: relative path (если есть) -> title/basename."""
    for k in ("docpath", "path", "url", "chunkSource"):
        v = meta.get(k)
        if v:
            return str(v).replace("file://", "").lstrip("/")
    return title


# ── Кэш vector-ответов (снижение нагрузки на ALM, ускорение ответов) ───────
_VECTOR_CACHE: "dict[tuple, tuple]" = {}
_VECTOR_CACHE_LOCK = threading.Lock()
_VECTOR_CACHE_TTL = float(os.environ.get("MG_VECTOR_CACHE_TTL", "120"))
_VECTOR_CACHE_MAX = int(os.environ.get("MG_VECTOR_CACHE_MAX", "256"))

# D2: sync-state invalidation — следим за mtime реального файла-продюсера.
# Файл incremental_report.json перезаписывается каждые 5 мин (alm-sync-incremental.service).
# При изменении mtime — полный сброс кэша (garbage window ≤ mtime-latency).
_SYNC_STATE_FILE = os.environ.get(
    "MG_SYNC_STATE_FILE",
    os.path.join(config.OPS_DIR, "incremental_report.json"),
)
_SYNC_STATE_MTIME: float = 0.0
_SYNC_STATE_CHECK_INTERVAL = 5.0  # проверяем mtime не чаще раза в 5 сек
_SYNC_STATE_LAST_CHECK: float = 0.0


def _check_sync_state() -> None:
    """D2: сброс кэша при обновлении индекса ALM.
    Проверяет mtime incremental_report.json (пишется каждые 5 мин инкременталом).
    Вызывается перед каждым поиском, но не чаще _SYNC_STATE_CHECK_INTERVAL.
    """
    global _SYNC_STATE_MTIME, _SYNC_STATE_LAST_CHECK
    now = time.monotonic()
    if now - _SYNC_STATE_LAST_CHECK < _SYNC_STATE_CHECK_INTERVAL:
        return
    _SYNC_STATE_LAST_CHECK = now
    try:
        mtime = os.path.getmtime(_SYNC_STATE_FILE)
    except OSError:
        return  # файл не найден — кэш не трогаем
    if _SYNC_STATE_MTIME == 0.0:
        # первый вызов — запоминаем mtime без сброса
        _SYNC_STATE_MTIME = mtime
        return
    if mtime > _SYNC_STATE_MTIME:
        _SYNC_STATE_MTIME = mtime
        with _VECTOR_CACHE_LOCK:
            n = len(_VECTOR_CACHE)
            _VECTOR_CACHE.clear()
        if n:
            log.info("D2: sync state changed, cleared %d cached entries", n)


def _vector_cache_get(query: str, top_k: int, threshold: float, workspace) -> "list | None":
    _check_sync_state()
    key = (query, top_k, threshold, workspace)
    with _VECTOR_CACHE_LOCK:
        entry = _VECTOR_CACHE.get(key)
        if entry and (time.monotonic() - entry[1]) < _VECTOR_CACHE_TTL:
            return entry[0]
    return None


def _vector_cache_put(query: str, top_k: int, threshold: float, workspace, results: list) -> None:
    key = (query, top_k, threshold, workspace)
    with _VECTOR_CACHE_LOCK:
        _VECTOR_CACHE[key] = (results, time.monotonic())
        if len(_VECTOR_CACHE) > _VECTOR_CACHE_MAX:
            try:
                _VECTOR_CACHE.pop(next(iter(_VECTOR_CACHE)))
            except StopIteration:
                pass


def vector_search(query: str, top_k: int, threshold: float,
                  workspace: str | None = None) -> list[dict[str, Any]]:
    """Векторный поиск по одному или всем workspace (параллельно).
    Результат кэшируется на MG_VECTOR_CACHE_TTL сек (см. _vector_cache_*)."""
    cached = _vector_cache_get(query, top_k, threshold, workspace)
    if cached is not None:
        return cached
    slugs = [workspace] if workspace else workspace_slugs_for_query(query)
    if not slugs:
        return []
    hits: list[dict[str, Any]] = []
    workers = max(1, min(len(slugs), config.VECTOR_MAX_WORKERS))
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as ex:
        futs = [ex.submit(_vector_search_one, s, query, top_k, threshold) for s in slugs]
        for fut in concurrent.futures.as_completed(futs):
            try:
                hits.extend(fut.result())
            except Exception as e:  # noqa: BLE001
                log.warning("vector future error: %s", e)
    hits.sort(key=lambda h: h["vector_score"], reverse=True)
    out = hits[:top_k]
    _vector_cache_put(query, top_k, threshold, workspace, out)
    return out


# ── Lexical-слой (FTS5 / BM25 по lexical.db, read-only) ──────────────────
def _lexical_connect() -> sqlite3.Connection:
    """Read-only соединение к lexical.db (защита от записи/повреждения)."""
    uri = f"file:{config.LEXICAL_DB}?mode=ro"
    return sqlite3.connect(uri, uri=True, timeout=config.SEARCH_TIMEOUT)


def lexical_search(query: str, top_k: int) -> list[dict[str, Any]]:
    """FTS5/BM25 полнотекстовый поиск. OR по токенам (recall), BM25 ранжирует."""
    if not os.path.exists(config.LEXICAL_DB):
        log.warning("lexical.db missing: %s", config.LEXICAL_DB)
        return []
    tokens = [t for t in re.split(r"\W+", query) if len(t) >= 2]
    if not tokens:
        return []
    match = " OR ".join(tokens)
    try:
        conn = _lexical_connect()
        try:
            sql = (
                "SELECT path, title, bm25(docs_fts) AS rank, "
                f"snippet(docs_fts, 0, '{SNIP_OPEN}', '{SNIP_CLOSE}', '\u2026', 12) "
                "FROM docs_fts WHERE docs_fts MATCH ? ORDER BY rank LIMIT ?"
            )
            rows = conn.execute(sql, (match, top_k)).fetchall()
        finally:
            conn.close()
    except sqlite3.Error as e:
        log.error("lexical query failed: %s", e)
        return []
    out = []
    for path, title, rank, snip in rows:
        score = round(-float(rank), 4)
        if score < config.LEXICAL_MIN_SCORE:
            continue
        clean = (snip or "").replace(SNIP_OPEN, "").replace(SNIP_CLOSE, "").strip()
        out.append({
            "source": "lexical",
            "workspace": None,
            "title": title,
            "doc_id": path,               # rel path — каноничный id для get_document
            "text": clean,
            "lexical_score": score,       # bm25 negative -> higher=better
        })
    return out


# ── Слияние: Reciprocal Rank Fusion + дедуп ─────────────────────────────
def _dedup_key(item: dict[str, Any]) -> str:
    """Ключ дедупликации: базовое имя документа, регистронезависимо.
    Сводит vector(title=CHANGELOG.md) и lexical(path=.../CHANGELOG.md)."""
    did = item.get("doc_id") or item.get("title") or ""
    return os.path.basename(str(did)).lower()


def _minmax(vals: list[float]) -> dict[float, float]:
    """Min-max нормализация списка в 0..1."""
    if not vals:
        return {}
    lo, hi = min(vals), max(vals)
    if hi - lo < 1e-9:
        return {v: 1.0 for v in vals}
    return {v: (v - lo) / (hi - lo) for v in vals}


import math


def _calculate_temporal_decay(doc_id: str) -> float:

    """Issue #8: Temporal Decay Scaling.
    Calculates recency factor exp(-0.005 * delta_days) based on doc mtime.
    Returns float multiplier in range 0.6..1.0.
    """
    if not doc_id or ".." in doc_id or doc_id.startswith("/"):
        return 1.0
    try:
        if os.path.exists(doc_id):
            mtime = os.path.getmtime(doc_id)
            delta_days = max(0.0, (time.time() - mtime) / 86400.0)
            return max(0.6, math.exp(-0.005 * delta_days))
    except Exception:
        pass
    return 1.0


def _extract_related_docs(text: str) -> list[str]:
    """Issue #11: Cross-Document Dependency Graphing.
    Extracts markdown file links [label](path.md) and Python import paths.
    """
    if not text:
        return []
    related = []
    # Markdown links
    for match in re.findall(r"\[.*?\]\(([\w\.\/\-]+\.(?:md|py|json|txt|sql))\)", text):
        if match not in related:
            related.append(match)
    # Python imports
    for match in re.findall(r"^(?:from|import)\s+([\w\.]+)", text, re.MULTILINE):
        rel_path = match.replace(".", "/") + ".py"
        if rel_path not in related:
            related.append(rel_path)
    return related[:5]


def _fuse_weighted(vector_hits, lexical_hits, top_k):
    """Score-calibrated fusion (P1) + Temporal Decay Scaling (Issue #8).

    Нормализует vector-cosine и lexical-BM25 в общую 0..1 шкалу,
    применяет временное затухание (Temporal Decay) для отдачи предпочтения свежим файлам.
    """
    alpha = config.FUSION_VECTOR_WEIGHT
    v_scores = [float(h.get("vector_score") or 0.0) for h in vector_hits]
    l_scores = [float(h.get("lexical_score") or 0.0) for h in lexical_hits]
    v_norm = _minmax(v_scores)
    l_norm = _minmax(l_scores)

    fused: dict[str, dict[str, Any]] = {}
    for hits, norms, weight in (
        (vector_hits, [v_norm.get(s, 0.0) for s in v_scores], alpha),
        (lexical_hits, [l_norm.get(s, 0.0) for s in l_scores], 1.0 - alpha),
    ):
        for h, sc in zip(hits, norms):
            key = _dedup_key(h)
            if not key:
                continue
            entry = fused.get(key)
            if entry is None:
                doc_id = h.get("doc_id")
                entry = {
                    "doc_id": doc_id,
                    "title": h.get("title"),
                    "workspace": h.get("workspace"),
                    "text": h.get("text", ""),
                    "sources": [],
                    "vector_score": None,
                    "lexical_score": None,
                    "fused_score": 0.0,
                    "temporal_decay": _calculate_temporal_decay(str(doc_id or "")),
                }
                fused[key] = entry
            entry["fused_score"] += weight * sc
            src = h.get("source")
            if src and src not in entry["sources"]:
                entry["sources"].append(src)
            if h.get("vector_score") is not None:
                entry["vector_score"] = h["vector_score"]
            if h.get("lexical_score") is not None:
                entry["lexical_score"] = h["lexical_score"]
            if len(h.get("text", "")) > len(entry["text"]):
                entry["text"] = h["text"]
            if h.get("workspace") and not entry["workspace"]:
                entry["workspace"] = h["workspace"]
            if "/" in str(h.get("doc_id", "")) and "/" not in str(entry["doc_id"] or ""):
                entry["doc_id"] = h["doc_id"]

    for entry in fused.values():
        entry["fused_score"] *= entry.get("temporal_decay", 1.0)
        entry["related_doc_ids"] = _extract_related_docs(entry.get("text", ""))

    ordered = [e for e in fused.values() if e["fused_score"] >= config.FUSION_MIN_COMBINED]
    ordered.sort(key=lambda e: e["fused_score"], reverse=True)
    for e in ordered:
        e["rrf_score"] = round(e.pop("fused_score"), 6)
    return ordered[:top_k]



def rrf_merge(vector_hits: list[dict[str, Any]],
              lexical_hits: list[dict[str, Any]],
              top_k: int) -> list[dict[str, Any]]:
    """RRF: score = sum(1/(rank+K)) по спискам. Дедуп по имени документа.
    При дубле сохраняем самый информативный вариант (с непустым text/workspace)."""
    k = config.RRF_K
    k = config.RRF_K
    merged: dict[str, dict[str, Any]] = {}
    for hits in (vector_hits, lexical_hits):
        for rank, item in enumerate(hits, start=1):
            key = _dedup_key(item)
            if not key:
                continue
            entry = merged.get(key)
            if entry is None:
                entry = {
                    "doc_id": item.get("doc_id"),
                    "title": item.get("title"),
                    "workspace": item.get("workspace"),
                    "text": item.get("text", ""),
                    "sources": [],
                    "vector_score": None,
                    "lexical_score": None,
                    "rrf_score": 0.0,
                }
                merged[key] = entry
            entry["rrf_score"] += 1.0 / (rank + k)
            src = item.get("source")
            if src and src not in entry["sources"]:
                entry["sources"].append(src)
            if item.get("vector_score") is not None:
                entry["vector_score"] = item["vector_score"]
            if item.get("lexical_score") is not None:
                entry["lexical_score"] = item["lexical_score"]
            # предпочесть более полный контекст и реальный doc_id-путь
            if len(item.get("text", "")) > len(entry["text"]):
                entry["text"] = item["text"]
            if item.get("workspace") and not entry["workspace"]:
                entry["workspace"] = item["workspace"]
            if "/" in str(item.get("doc_id", "")) and "/" not in str(entry["doc_id"] or ""):
                entry["doc_id"] = item["doc_id"]
    ordered = sorted(merged.values(), key=lambda e: e["rrf_score"], reverse=True)
    for e in ordered:
        e["rrf_score"] = round(e["rrf_score"], 6)
    return ordered[:top_k]


# ── Публичный гибридный поиск ───────────────────────────────────────────
def hybrid_search(query: str, top_k: int,
                  workspace: str | None = None,
                  expand_context: bool = config.EXPAND_CONTEXT_DEFAULT,
                  fusion: str | None = None,
                  max_token_budget: int | None = None,
                  tier: str | None = None) -> dict[str, Any]:
    """Гибрид: vector + lexical ПАРАЛЛЕЛЬНО, слияние. Возвращает чистый JSON.

    degraded=True, если один из слоёв недоступен (второй всё равно вернёт данные).
    fusion: 'weighted' (default, score-calibrated) | 'rrf' (classic).
    """
    query = (query or "").strip()[: config.QUERY_MAX_LEN]
    if not query:
        return {"query": query, "count": 0, "results": [], "degraded": False,
                "layers": {"vector": 0, "lexical": 0}, "error": "empty query"}

    top_k = max(1, min(int(top_k or config.DEFAULT_TOP_K), config.MAX_TOP_K))
    cand = top_k * config.CANDIDATE_MULT

    vector_hits: list[dict[str, Any]] = []
    lexical_hits: list[dict[str, Any]] = []
    vec_ok = lex_ok = True

    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as ex:
        f_vec = ex.submit(vector_search, query, cand, config.VECTOR_SCORE_THRESHOLD, workspace)
        f_lex = ex.submit(lexical_search, query, cand)
        try:
            vector_hits = f_vec.result()
        except Exception as e:  # noqa: BLE001
            vec_ok = False
            log.error("vector layer failed: %s", e)
        try:
            lexical_hits = f_lex.result()
        except Exception as e:  # noqa: BLE001
            lex_ok = False
            log.error("lexical layer failed: %s", e)

    mode = (fusion or config.FUSION_MODE).lower()
    if mode == "weighted":
        results = _fuse_weighted(vector_hits, lexical_hits, top_k)
    else:
        results = rrf_merge(vector_hits, lexical_hits, top_k)
    if expand_context:
        for r in results:
            _expand_result(r)

    # Adaptive Token Budgeting (P3): trim context results cleanly if token budget specified
    total_tokens = 0
    if max_token_budget and max_token_budget > 0:
        trimmed_results = []
        for r in results:
            text = r.get("text", "")
            est_tokens = max(1, len(text) // 4)
            if total_tokens + est_tokens <= max_token_budget:
                trimmed_results.append(r)
                total_tokens += est_tokens
            else:
                # Partial sentence boundary trimming
                remaining_tokens = max_token_budget - total_tokens
                allowed_chars = remaining_tokens * 4
                if allowed_chars > 50:
                    sub_text = text[:allowed_chars].rsplit(" ", 1)[0] + "..."
                    r["text"] = sub_text
                    r["trimmed_to_budget"] = True
                    trimmed_results.append(r)
                    total_tokens += remaining_tokens
                break
        results = trimmed_results

    return {
        "query": query,
        "count": len(results),
        "results": results,
        "degraded": not (vec_ok and lex_ok),
        "layers": {"vector": len(vector_hits), "lexical": len(lexical_hits)},
        "total_estimated_tokens": total_tokens
    }


def store_memory(content: str, title: str | None = None, workspace: str | None = "dmagybot",
                 metadata: dict[str, Any] | None = None, tier: str | None = "semantic",
                 retry_auth: bool = True) -> dict[str, Any]:
    """Инструмент активной записи памяти (Issue #7). Загружает сырой текст в AnythingLLM и добавляет эмбеддинги."""
    if not content or not content.strip():
        return {"success": False, "error": "empty content"}

    tok = load_token()
    clean_title = (title or f"memory_{int(time.time())}.txt").strip()
    meta = metadata or {}
    meta.update({
        "title": clean_title,
        "tier": tier or "semantic",
        "stored_at": time.strftime("%Y-%m-%dT%H:%M:%SZ")
    })

    try:
        r = requests.post(
            f"{config.ALM_BASE}/document/raw-text",
            headers={"Authorization": f"Bearer {tok}", "Content-Type": "application/json"},
            json={"textContent": content, "metadata": meta},
            timeout=config.SEARCH_TIMEOUT
        )
        if r.status_code in (401, 403) and retry_auth:
            log.warning("store_memory received HTTP %s (token expired). Invalidating token cache and retrying...", r.status_code)
            invalidate_token_cache()
            return store_memory(content, title, workspace, metadata, tier, retry_auth=False)
        if not r.ok:
            log.error("Upload HTTP %s: %s", r.status_code, r.text)
            return {"success": False, "error": "An internal error occurred"}

        doc_data = r.json()
        docs = doc_data.get("documents", [])
        if not docs:
            return {"success": False, "error": "No document returned by AnythingLLM upload"}

        location = docs[0].get("location")
        doc_id = docs[0].get("id")

        target_ws = workspace or "dmagybot"
        ws_r = requests.post(
            f"{config.ALM_BASE}/workspace/{urllib.parse.quote(target_ws, safe='')}/update-embeddings",
            headers={"Authorization": f"Bearer {tok}", "Content-Type": "application/json"},
            json={"adds": [location]},
            timeout=config.SEARCH_TIMEOUT
        )
        if not ws_r.ok:
            log.error("Workspace embedding HTTP %s: %s", ws_r.status_code, ws_r.text)
            return {"success": False, "error": "An internal error occurred"}

        log.info("store_memory success title=%r workspace=%s doc_id=%s", clean_title, target_ws, doc_id)
        return {
            "success": True,
            "doc_id": doc_id,
            "title": clean_title,
            "location": location,
            "workspace": target_ws,
            "tier": tier
        }
    except Exception as e:
        log.exception("store_memory failed")
        return {"success": False, "error": str(e)}



# ── get_document: полный сырой текст документа ──────────────────────────
def alm_latency_stats() -> dict[str, Any]:
    """ALM call latency telemetry (P4). Read-only, thread-safe.
    Returns last/p50/p95 in ms and sample count; None if no samples.
    """
    with _ALM_LAT_LOCK:
        s = list(_ALM_LATENCY)
    if not s:
        return {"count": 0, "last_ms": None, "p50_ms": None,
                "p95_ms": None,
                "inflight_limit": max(1, int(config.VECTOR_MAX_INFLIGHT))}
    s.sort()
    n = len(s)
    def pct(p):
        return s[min(n - 1, int(p * n))]
    return {"count": n,
            "last_ms": round(s[-1], 1),
            "p50_ms": round(pct(0.50), 1),
            "p95_ms": round(pct(0.95), 1),
            "inflight_limit": max(1, int(config.VECTOR_MAX_INFLIGHT))}

def get_document(doc_id: str, max_chars: int = 20000) -> dict[str, Any]:
    """Полный сырой текст документа по doc_id.

    Стратегия (raw retrieval, без LLM):
    1) lexical.db (полный content) по точному path, затем по basename;
    2) fallback — метаданные официального API AnythingLLM (/document/:name).
    """
    doc_id = (doc_id or "").strip()
    if not doc_id:
        return {"doc_id": doc_id, "found": False, "error": "empty doc_id"}

    # 1) полный текст из lexical.db
    if os.path.exists(config.LEXICAL_DB):
        try:
            conn = _lexical_connect()
            try:
                row = conn.execute(
                    "SELECT path, title, content FROM docs_fts WHERE path = ? LIMIT 1",
                    (doc_id,),
                ).fetchone()
                if row is None:
                    base = os.path.basename(doc_id)
                    row = conn.execute(
                        "SELECT path, title, content FROM docs_fts "
                        "WHERE path LIKE ? LIMIT 1",
                        (f"%/{base}",),
                    ).fetchone()
            finally:
                conn.close()
            if row is not None:
                path, title, content = row
                content = content or ""
                return {
                    "doc_id": path,
                    "found": True,
                    "source": "lexical",
                    "title": title,
                    "chars": len(content),
                    "truncated": len(content) > max_chars,
                    "content": content[:max_chars],
                }
        except sqlite3.Error as e:
            log.error("get_document lexical error: %s", e)

    # 2) fallback: метаданные официального API (без /chat)
    try:
        tok = load_token()
        r = requests.get(
            f"{config.ALM_BASE}/document/{urllib.parse.quote(doc_id, safe='')}",
            headers={"Authorization": f"Bearer {tok}"},
            timeout=config.LIST_TIMEOUT,
        )
        if r.status_code == 200:
            d = r.json().get("document", {})
            return {
                "doc_id": doc_id,
                "found": True,
                "source": "anythingllm-metadata",
                "title": d.get("title"),
                "chars": 0,
                "truncated": False,
                "content": "",
                "metadata": d,
                "note": "AnythingLLM API returns metadata only; full text не найден в lexical.db",
            }
    except Exception as e:  # noqa: BLE001
        log.warning("get_document api fallback failed: %s", e)

    return {"doc_id": doc_id, "found": False, "error": "document not found"}


# ── Context Assembly: расширение пассажа до связного блока ──────────────
def _strip_anchor(text: str) -> str:
    """Убирает FTS5-маркеры и схлопывает пробелы для надёжного поиска якоря."""
    return re.sub(r"\s+", " ", (text or "").replace(SNIP_OPEN, "").replace(SNIP_CLOSE, "").replace("\u2026", " ")).strip()


def _norm_map(text: str) -> tuple:
    """Нормализует текст (lower + удаление кавычек + схлопывание whitespace в
    один пробел) для поиска якоря и возвращает (norm_text, offsets), где
    offsets[i] — позиция i-го символа norm_text в ИСХОДНОМ text. Позволяет
    искать seed без кавычек/переводов строк, но получить корректную позицию
    в оригинале для последующего разбиения на абзацы.
    """
    norm_chars: list[str] = []
    offsets: list[int] = []
    prev_ws = False
    for i, ch in enumerate(text):
        if ch in '"“”':
            continue
        if ch.isspace():
            if not prev_ws:
                norm_chars.append(" ")
                offsets.append(i)
                prev_ws = True
            continue
        norm_chars.append(ch.lower())
        offsets.append(i)
        prev_ws = False
    return "".join(norm_chars), offsets


def _lexical_fts_phrase(phrase: str) -> list[tuple]:
    """[(path, content)] по точной фразе FTS5 — точное попадание в нужный документ."""
    if not os.path.exists(config.LEXICAL_DB) or len(phrase) < 8:
        return []
    try:
        conn = _lexical_connect()
        try:
            rows = conn.execute(
                "SELECT path, content FROM docs_fts WHERE docs_fts MATCH ? LIMIT 5",
                (f'"{phrase}"',),
            ).fetchall()
        finally:
            conn.close()
        return [(p, c or "") for p, c in rows]
    except sqlite3.Error as e:
        log.error("lexical fts phrase failed: %s", e)
        return []


def _lexical_fts_tokens(q: str) -> list[tuple]:
    """[(path, content)] по токенам FTS5 (AND, не обязательно смежные).
    Не зависит от пути документа — находит нужный док по содержимому.
    """
    if not q or not os.path.exists(config.LEXICAL_DB):
        return []
    try:
        conn = _lexical_connect()
        try:
            rows = conn.execute(
                "SELECT path, content FROM docs_fts WHERE docs_fts MATCH ? LIMIT 25",
                (q,),
            ).fetchall()
        finally:
            conn.close()
        return [(p, c or "") for p, c in rows]
    except sqlite3.Error as e:
        log.error("lexical fts tokens failed: %s", e)
        return []


def _lexical_candidates(base: str) -> list[tuple]:
    """Все документы, чей path оканчивается на /base (неоднозначные имена: SKILL.md)."""
    if not base or not os.path.exists(config.LEXICAL_DB):
        return []
    try:
        conn = _lexical_connect()
        try:
            rows = conn.execute(
                "SELECT path, content FROM docs_fts WHERE path LIKE ? LIMIT 20",
                (f"%/{base}",),
            ).fetchall()
        finally:
            conn.close()
        return [(p, c or "") for p, c in rows]
    except sqlite3.Error as e:
        log.error("lexical candidates failed: %s", e)
        return []


def _strip_uuid_prefix(base: str) -> str:
    return re.sub(
        r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}-", "", base
    )


def _expand_result(result: dict[str, Any]) -> None:
    """Мутирует result: расширяет result['text'] соседними абзацами того же
    документа. Неоднозначность имён (SKILL.md, README.md) снимается через
    FTS5-фразу якоря — выбирается документ, в котором якорь реально есть.

    При неудаче оставляет text как есть, context_expanded=False.
    """
    did = result.get("doc_id")
    original = _clean_text(result.get("text", "") or "")
    result["context_expanded"] = False
    if not did or not original.strip():
        return
    anchor = _strip_anchor(original)
    seed = re.sub(r'["“”]', "", anchor[:60]).lower()
    base = _strip_uuid_prefix(os.path.basename(did))

    # 1) точный документ по фразе якоря (FTS5, смежные токены)
    phrase = " ".join(re.sub(r'["“”]', "", w) for w in anchor.split()[:8])
    candidates = _lexical_fts_phrase(phrase)
    # 2) фолбэк: FTS5 по ключевым токенам якоря (AND) — не зависит от пути.
    #    Пунктуацию (в т.ч. ':' которая ломает MATCH как column-filter) режем.
    if not candidates:
        raw = re.sub(r'["“”]', "", anchor).lower()
        toks = [re.sub(r"[^\w]", "", w) for w in raw.split()]
        toks = [t for t in toks if len(t) > 2][:6]
        if toks:
            candidates = _lexical_fts_tokens(" ".join(toks))
    # 3) фолбэк: все доки с таким basename
    if not candidates and base:
        candidates = _lexical_candidates(base)
    if not candidates:
        return
    # выбираем документ, содержащий якорь (снимает неоднозначность).
    # Сверяем через нормализацию (whitespace + кавычки), чтобы якорь
    # находился даже при разнице в переводах строк.
    content = None
    for _path, _c in candidates:
        _n, _o = _norm_map(_c)
        if _n.find(seed[:30]) >= 0:
            content = _c
            break
    if content is None:
        content = candidates[0][1]  # фолбэк на первый
    if not content:
        return

    norm, offsets = _norm_map(content)
    idx = norm.find(seed)
    if idx < 0 and len(seed) > 30:
        idx = norm.find(seed[:30])
    if idx < 0 and len(seed) > 20:
        idx = norm.find(seed[:20])
    if idx < 0:
        return
    idx = offsets[idx]
    # разбиваем на абзацы и локализуем matched
    paras = re.split(r"\n\s*\n", content)
    pos = 0
    matched = 0
    for i, p in enumerate(paras):
        if idx < pos + len(p):
            matched = i
            break
        pos += len(p) + 2  # съедаем разделитель \n\n
    else:
        matched = len(paras) - 1
    # растим окно симметрично от matched до EXPAND_MAX_CHARS
    window = [paras[matched]]
    step = 1
    while len("\n\n".join(window)) < config.EXPAND_MAX_CHARS and (
        matched - step >= 0 or matched + step < len(paras)
    ):
        if matched - step >= 0:
            window.insert(0, paras[matched - step])
        if matched + step < len(paras):
            window.append(paras[matched + step])
        step += 1
    expanded = "\n\n".join(window)
    if len(expanded) > config.EXPAND_MAX_CHARS:
        expanded = expanded[: config.EXPAND_MAX_CHARS]
    result["text"] = expanded
    result["context_expanded"] = True
    result["expanded_chars"] = len(expanded)
    result["original_chars"] = len(original)
