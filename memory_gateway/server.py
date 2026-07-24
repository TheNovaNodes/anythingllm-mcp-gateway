"""MCP-сервер memory-gateway — единое окно к семантической памяти лаборатории.

Официальный MCP SDK (FastMCP). Инструменты:
  - search_memory(query, top_k, workspace): гибридный поиск vector+lexical (RRF).
  - get_document(doc_id, max_chars): полный сырой текст документа.
  - gateway_health(): состояние слоёв (диагностика).

Только сырые данные. Никаких /chat и LLM-прослоек.
Транспорт: MG_TRANSPORT=stdio (по умолчанию) | streamable-http (сетевой деплой).
"""
import os
import time
from typing import Any, Dict, Optional

from mcp.server.fastmcp import FastMCP

from . import config, search
from .logger import get_logger

log = get_logger()

mcp = FastMCP("memory-gateway", host=config.HOST, port=config.PORT)


@mcp.tool(name="search_memory")
def search_memory(query: str, top_k: int = config.DEFAULT_TOP_K,
                  workspace: Optional[str] = None,
                  expand_context: bool = config.EXPAND_CONTEXT_DEFAULT) -> Dict[str, Any]:
    """Гибридный семантический поиск по памяти лаборатории (vector + lexical, RRF).

    Args:
        query: поисковый запрос на естественном языке или по ключевым словам.
        top_k: число результатов (1..MAX_TOP_K).
        workspace: опционально — ограничить векторный слой одним слагом workspace.
        expand_context: если True (по умолчанию), каждый найденный пассаж
            расширяется до связного блока — подтягиваются соседние абзацы того
            же документа (Context Assembly), чтобы не отдавать изолированный
            чанк, оборванный на полуслове.

    Returns:
        Чистый JSON: {query, count, results[], degraded, layers}. Без LLM-синтеза.
        Каждый результат: {doc_id, title, workspace, text, sources[],
                           vector_score, lexical_score, rrf_score,
                           context_expanded, expanded_chars, original_chars}.
    """
    t0 = time.time()
    try:
        out = search.hybrid_search(query, top_k, workspace, expand_context)
    except Exception as e:  # noqa: BLE001 — инструмент не должен ронять сервер
        log.exception("search_memory failed")
        return {"query": query, "count": 0, "results": [], "degraded": True,
                "error": f"{type(e).__name__}: {e}"}
    out["latency_ms"] = round((time.time() - t0) * 1000.0, 1)
    log.info("search_memory q=%r top_k=%s count=%s degraded=%s %sms",
             (query or "")[:80], top_k, out.get("count"), out.get("degraded"),
             out["latency_ms"])
    return out


@mcp.tool(name="get_document")
def get_document(doc_id: str, max_chars: int = 20000) -> Dict[str, Any]:
    """Полный сырой текст документа по doc_id (из search_memory results[].doc_id).

    Args:
        doc_id: путь/идентификатор документа (напр. projects/lab-memory/CHANGELOG.md).
        max_chars: максимум символов текста (защита от переполнения контекста).

    Returns:
        Чистый JSON: {doc_id, found, source, title, chars, truncated, content}.
    """
    try:
        return search.get_document(doc_id, max_chars)
    except Exception as e:  # noqa: BLE001
        log.exception("get_document failed")
        return {"doc_id": doc_id, "found": False, "error": f"{type(e).__name__}: {e}"}


@mcp.tool(name="gateway_health")
def gateway_health() -> Dict[str, Any]:
    """Диагностика шлюза: доступность токена, lexical.db, число workspace."""
    health: Dict[str, Any] = {"ok": True}
    # токен
    try:
        tok = search.load_token()
        health["token"] = {"present": bool(tok), "len": len(tok)}
    except Exception as e:  # noqa: BLE001
        health["ok"] = False
        health["token"] = {"present": False, "error": str(e)}
    # lexical.db
    health["lexical_db"] = {
        "path": config.LEXICAL_DB,
        "exists": os.path.exists(config.LEXICAL_DB),
    }
    # workspaces
    try:
        slugs = search.workspace_slugs()
        health["workspaces"] = {"count": len(slugs)}
    except Exception as e:  # noqa: BLE001
        health["ok"] = False
        health["workspaces"] = {"count": 0, "error": str(e)}
    # vector layer — live probe (AnythingLLM /api/v1/system/vector-count)
    try:
        import requests as _requests
        tok = search.load_token()
        vr = _requests.get(
            f"{config.ALM_BASE}/system/vector-count",
            headers={"Authorization": f"Bearer {tok}"},
            timeout=config.SEARCH_TIMEOUT,
        )
        if vr.ok:
            health["vector_layer"] = {
                "reachable": True,
                "vector_count": vr.json().get("vectorCount"),
            }
        else:
            health["ok"] = False
            health["vector_layer"] = {"reachable": False, "status": vr.status_code}
    except Exception as e:  # noqa: BLE001
        health["ok"] = False
        health["vector_layer"] = {"reachable": False, "error": str(e)}
    # HONEST probe: reachable+vector_count врут, если поисковый путь падает.
    # Дёргаем реальный гибридный поиск и смотрим, вернул ли векторный слой хиты.
    try:
        probe = search.hybrid_search("тест семантической памяти", top_k=3,
                                     expand_context=False)
        vec_hits = int(probe.get("layers", {}).get("vector", 0))
        vl_ok = health.get("vector_layer", {}).get("reachable", False)
        health["vector_search"] = {
            "functional": vec_hits > 0,
            "vector_hits": vec_hits,
            "degraded": bool(probe.get("degraded")),
        }
        if vl_ok and vec_hits == 0:
            health["ok"] = False
            health["vector_layer"]["functional"] = False
    except Exception as e:  # noqa: BLE001
        health["ok"] = False
        health["vector_search"] = {"functional": False, "error": str(e)}
    # P4: ALM call latency telemetry (throttle + health visibility)
    try:
        health["latency"] = search.alm_latency_stats()
    except Exception as e:  # noqa: BLE001
        health["latency"] = {"error": str(e)}
    health["alm_base"] = config.ALM_BASE
    health["version"] = __import__("memory_gateway").__version__

    # ── человекочитаемый итог (для алертов/крона) ───────────────────────
    problems: list[str] = []
    if not health.get("token", {}).get("present"):
        problems.append("нет Bearer-токена AnythingLLM")
    if not health.get("lexical_db", {}).get("exists"):
        problems.append("лексический индекс (lexical.db) отсутствует")
    vl = health.get("vector_layer", {})
    if not vl.get("reachable"):
        problems.append("векторный слой недоступен")
    vs = health.get("vector_search", {})
    if vl.get("reachable") and not vs.get("functional", True):
        problems.append(
            "векторный поиск не работает (индекс есть, но поиск отдаёт 0 хитов) "
            "— семантическая память деградировала до лексической"
        )
    ws = health.get("workspaces", {})
    if "error" in ws:
        problems.append("ошибка перечисления workspace")
    if problems:
        health["message"] = (
            "⚠️ Деградация семантической памяти: " + ", ".join(problems) + "."
        )
    else:
        health["message"] = (
            f"✅ Семантическая память работает штатно: "
            f"{ws.get('count')} пространств знаний, "
            f"{vl.get('vector_count')} векторов в индексе, "
            f"лексический слой подключён. Шлюз v{health['version']}."
        )
    return health


def main() -> None:
    log.info("memory-gateway starting transport=%s", config.TRANSPORT)
    if config.TRANSPORT == "stdio":
        mcp.run(transport="stdio")
    else:
        mcp.run(transport="streamable-http")


if __name__ == "__main__":
    main()
