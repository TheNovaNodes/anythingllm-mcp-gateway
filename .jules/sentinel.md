## 2026-08-31 - [Sentinel] - Security finding: Exposing Backend Details in API Responses
**Vulnerability:** The `store_memory` endpoint exposed `r.text` and `ws_r.text` from downstream backend services when backend errors occurred. The `search_memory` and `get_document` tools exposed the internal python exception type and details in their error responses (`{type(e).__name__}: {e}`).
**Learning:** These practices leak potentially highly sensitive internal backend implementation details, configuration, and state.
**Prevention:** Always log exceptions and backend responses locally, and return safe, generic error messages to the client.
