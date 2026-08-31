"""MCP-сервер memory-gateway — единое окно к семантической памяти лаборатории.

Официальный MCP SDK (FastMCP). Инструменты:
  - search_memory(query, top_k, workspace): гибридный поиск vector+lexical (RRF).
  - get_document(doc_id, max_chars): полный сырой текст документа.
  - gateway_health(): состояние слоёв (диагностика).

Только сырые данные. Никаких /chat и LLM-прослоек.
Транспорт: MG_TRANSPORT=stdio (по умолчанию) | streamable-http (сетевой деплой).
"""
__version__ = "0.2.0"
import os
import time
from typing import Any

try:
    from mcp.server.fastmcp import FastMCP
except ImportError:
    try:
        from fastmcp import FastMCP
    except ImportError:
        from mcp.server.mcpserver import MCPServer as FastMCP

from . import config, search
from .logger import get_logger

log = get_logger()

try:
    mcp = FastMCP("anythingllm-mcp-gateway", host=config.HOST, port=config.PORT)
except TypeError:
    mcp = FastMCP("anythingllm-mcp-gateway")



@mcp.tool(name="search_memory")
def search_memory(query: str, top_k: int = config.DEFAULT_TOP_K,
                  workspace: str | None = None,
                  expand_context: bool = config.EXPAND_CONTEXT_DEFAULT,
                  max_token_budget: int | None = None,
                  tier: str | None = None) -> dict[str, Any]:
    """Гибридный семантический поиск по памяти лаборатории (vector + lexical, RRF).

    Args:
        query: поисковый запрос на естественном языке или по ключевым словам.
        top_k: число результатов (1..MAX_TOP_K).
        workspace: опционально — ограничить векторный слой одним слагом workspace.
        expand_context: если True (по умолчанию), каждый найденный пассаж
            расширяется до связного блока — подтягиваются соседние абзацы того
            же документа (Context Assembly).
        max_token_budget: опционально — максимальный бюджет токенов вывода (Adaptive Token Budgeting).
        tier: опционально — иерархический уровень памяти ('episodic', 'semantic', 'procedural').

    Returns:
        Чистый JSON: {query, count, results[], degraded, layers, total_estimated_tokens}.
    """
    t0 = time.time()
    try:
        out = search.hybrid_search(
            query=query,
            top_k=top_k,
            workspace=workspace,
            expand_context=expand_context,
            max_token_budget=max_token_budget,
            tier=tier
        )
    except Exception:
        log.exception("search_memory failed")
        return {"query": query, "count": 0, "results": [], "degraded": True,
                "error": "An internal error occurred"}
    out["latency_ms"] = round((time.time() - t0) * 1000.0, 1)
    log.info("search_memory q=%r top_k=%s count=%s degraded=%s %sms",
             (query or "")[:80], top_k, out.get("count"), out.get("degraded"),
             out["latency_ms"])
    return out


@mcp.tool(name="store_memory")
def store_memory(content: str, title: str | None = None,
                 workspace: str | None = "dmagybot",
                 metadata: dict[str, Any] | None = None,
                 tier: str | None = "semantic") -> dict[str, Any]:
    """Сохранение новых фактов, диалогов и знаний в семантическую память (Issue #7).

    Args:
        content: текст знания, факта или заметки для сохранения.
        title: заголовок/имя файла знания (например, 'user_preferences.txt').
        workspace: целевой воркспейс в AnythingLLM (по умолчанию 'dmagybot').
        metadata: дополнительные метаданные в формате словаря.
        tier: иерархический уровень памяти ('episodic', 'semantic', 'procedural').

    Returns:
        Чистый JSON: {success, doc_id, title, location, workspace, tier}.
    """
    t0 = time.time()
    try:
        res = search.store_memory(
            content=content,
            title=title,
            workspace=workspace,
            metadata=metadata,
            tier=tier
        )
        res["latency_ms"] = round((time.time() - t0) * 1000.0, 1)
        return res
    except Exception:
        log.exception("store_memory failed")
        return {"success": False, "error": "An internal error occurred"}



@mcp.tool(name="get_document")
def get_document(doc_id: str, max_chars: int = 20000) -> dict[str, Any]:
    """Полный сырой текст документа по doc_id (из search_memory results[].doc_id).

    Args:
        doc_id: путь/идентификатор документа (напр. projects/lab-memory/CHANGELOG.md).
        max_chars: максимум символов текста (защита от переполнения контекста).

    Returns:
        Чистый JSON: {doc_id, found, source, title, chars, truncated, content}.
    """
    try:
        return search.get_document(doc_id, max_chars)
    except Exception:
        log.exception("get_document failed")
        return {"doc_id": doc_id, "found": False, "error": "An internal error occurred"}


@mcp.tool(name="gateway_health")
def gateway_health() -> dict[str, Any]:
    """Диагностика шлюза: доступность токена, lexical.db, число workspace."""
    health: dict[str, Any] = {"ok": True}
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
