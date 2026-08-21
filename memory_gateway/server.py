"""MCP server memory-gateway — a single entry point to the laboratory's semantic memory.

Official MCP SDK (FastMCP). Tools:
  - search_memory(query, top_k, workspace): hybrid search vector+lexical (RRF).
  - get_document(doc_id, max_chars): full raw document text.
  - gateway_health(): layer states (diagnostics).

Raw data only. No /chat or LLM middleware.
Transport: MG_TRANSPORT=stdio (default) | streamable-http (network deployment).
"""
__version__ = "0.2.0"
import os
import time
from typing import Any, Dict, Optional

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
                  workspace: Optional[str] = None,
                  expand_context: bool = config.EXPAND_CONTEXT_DEFAULT,
                  max_token_budget: Optional[int] = None,
                  tier: Optional[str] = None) -> Dict[str, Any]:
    """Hybrid semantic search in laboratory memory (vector + lexical, RRF).

    Args:
        query: natural language or keyword search query.
        top_k: number of results (1..MAX_TOP_K).
        workspace: optional — restrict vector layer to a single workspace slug.
        expand_context: if True (default), each found passage is expanded
            into a coherent block — adjacent paragraphs from the same
            document are fetched (Context Assembly).
        max_token_budget: optional — maximum output token budget (Adaptive Token Budgeting).
        tier: optional — hierarchical memory level ('episodic', 'semantic', 'procedural').

    Returns:
        Pure JSON: {query, count, results[], degraded, layers, total_estimated_tokens}.
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
    except Exception as e:  # noqa: BLE001 — tool must not crash the server
        log.exception("search_memory failed")
        return {"query": query, "count": 0, "results": [], "degraded": True,
                "error": f"{type(e).__name__}: {e}"}
    out["latency_ms"] = round((time.time() - t0) * 1000.0, 1)
    log.info("search_memory q=%r top_k=%s count=%s degraded=%s %sms",
             (query or "")[:80], top_k, out.get("count"), out.get("degraded"),
             out["latency_ms"])
    return out


@mcp.tool(name="store_memory")
def store_memory(content: str, title: Optional[str] = None,
                 workspace: Optional[str] = "dmagybot",
                 metadata: Optional[Dict[str, Any]] = None,
                 tier: Optional[str] = "semantic") -> Dict[str, Any]:
    """Store new facts, dialogs, and knowledge in semantic memory (Issue #7).

    Args:
        content: text of the knowledge, fact, or note to save.
        title: title/filename of the knowledge (e.g., 'user_preferences.txt').
        workspace: target workspace in AnythingLLM (default 'dmagybot').
        metadata: additional metadata as a dictionary.
        tier: hierarchical memory level ('episodic', 'semantic', 'procedural').

    Returns:
        Pure JSON: {success, doc_id, title, location, workspace, tier}.
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
    except Exception as e:
        log.exception("store_memory failed")
        return {"success": False, "error": f"{type(e).__name__}: {e}"}



@mcp.tool(name="get_document")
def get_document(doc_id: str, max_chars: int = 20000) -> Dict[str, Any]:
    """Get the full raw text of a document by doc_id (from search_memory results[].doc_id).

    Args:
        doc_id: path/identifier of the document (e.g., projects/lab-memory/CHANGELOG.md).
        max_chars: max text characters (context overflow protection).

    Returns:
        Pure JSON: {doc_id, found, source, title, chars, truncated, content}.
    """
    try:
        return search.get_document(doc_id, max_chars)
    except Exception as e:  # noqa: BLE001
        log.exception("get_document failed")
        return {"doc_id": doc_id, "found": False, "error": f"{type(e).__name__}: {e}"}


@mcp.tool(name="gateway_health")
def gateway_health() -> Dict[str, Any]:
    """Gateway diagnostics: token availability, lexical.db, workspace count."""
    health: Dict[str, Any] = {"ok": True}
    # token
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
    # HONEST probe: reachable+vector_count lie if the search path fails.
    # Trigger a real hybrid search and check if the vector layer returned hits.
    try:
        probe = search.hybrid_search("test semantic memory", top_k=3,
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

    # ── Human-readable summary (for alerts/cron) ───────────────────────
    problems: list[str] = []
    if not health.get("token", {}).get("present"):
        problems.append("missing AnythingLLM Bearer token")
    if not health.get("lexical_db", {}).get("exists"):
        problems.append("lexical index (lexical.db) is missing")
    vl = health.get("vector_layer", {})
    if not vl.get("reachable"):
        problems.append("vector layer unreachable")
    vs = health.get("vector_search", {})
    if vl.get("reachable") and not vs.get("functional", True):
        problems.append(
            "vector search is broken (index exists, but search returns 0 hits) "
            "— semantic memory has degraded to lexical"
        )
    ws = health.get("workspaces", {})
    if "error" in ws:
        problems.append("error listing workspaces")
    if problems:
        health["message"] = (
            "⚠️ Semantic memory degradation: " + ", ".join(problems) + "."
        )
    else:
        health["message"] = (
            f"✅ Semantic memory operating normally: "
            f"{ws.get('count')} knowledge spaces, "
            f"{vl.get('vector_count')} vectors in index, "
            f"lexical layer connected. Gateway v{health['version']}."
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
