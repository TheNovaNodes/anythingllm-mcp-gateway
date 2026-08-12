"""memory-gateway - MCP Semantic Memory Gateway for LabDoctorM.

Single access point for agents to the lab's semantic memory.
Hybrid retrieval: vector (AnythingLLM /vector-search) + lexical (FTS5/BM25).
Raw data only (raw retrieval). No /chat or LLM middleware.
"""

__version__ = "0.1.0"
