"""memory-gateway — MCP Semantic Memory Gateway for LabDoctorM.

Единое окно доступа агентов OpenClaw к семантической памяти лаборатории.
Гибридный retrieval: vector (AnythingLLM /vector-search) + lexical (FTS5/BM25).
Только сырые данные (raw retrieval). Никаких /chat и LLM-прослоек.
"""

__version__ = "0.1.0"
