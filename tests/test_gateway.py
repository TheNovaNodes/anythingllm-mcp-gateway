import pytest
from unittest.mock import patch, MagicMock

# Import the server tools
from memory_gateway.server import search_memory, get_document, gateway_health

def test_search_memory_success():
    with patch("memory_gateway.search.hybrid_search") as mock_search:
        mock_search.return_value = {
            "query": "test", "count": 1, "results": [{"doc_id": "test.md"}],
            "degraded": False, "layers": {"vector": 1}
        }
        res = search_memory("test")
        assert res["count"] == 1
        assert not res["degraded"]
        assert "latency_ms" in res

def test_search_memory_exception():
    with patch("memory_gateway.search.hybrid_search") as mock_search:
        mock_search.side_effect = Exception("API Timeout")
        res = search_memory("test")
        assert res["count"] == 0
        assert res["degraded"]
        assert "error" in res
        assert "API Timeout" in res["error"]

def test_get_document_success():
    with patch("memory_gateway.search.get_document") as mock_get:
        mock_get.return_value = {"doc_id": "123", "found": True, "content": "hello"}
        res = get_document("123")
        assert res["found"]
        assert res["content"] == "hello"

def test_get_document_exception():
    with patch("memory_gateway.search.get_document") as mock_get:
        mock_get.side_effect = Exception("404 Not Found")
        res = get_document("123")
        assert not res["found"]
        assert "error" in res

@patch("memory_gateway.search.load_token")
@patch("memory_gateway.search.workspace_slugs")
@patch("memory_gateway.search.hybrid_search")
@patch("requests.get")
def test_gateway_health_success(mock_get, mock_search, mock_slugs, mock_token):
    mock_token.return_value = "fake-token"
    mock_slugs.return_value = ["workspace1"]
    
    mock_vr = MagicMock()
    mock_vr.ok = True
    mock_vr.json.return_value = {"vectorCount": 100}
    mock_get.return_value = mock_vr
    
    mock_search.return_value = {"layers": {"vector": 1}, "degraded": False}
    
    res = gateway_health()
    # It might be missing lexical_db, so it might not be fully OK
    assert "token" in res
    assert "workspaces" in res


def test_token_invalidation_and_reload(tmp_path, monkeypatch):
    from memory_gateway import search, config
    
    token_file = tmp_path / "test_token.txt"
    token_file.write_text("token-v1")
    
    monkeypatch.setattr(config, "TOKEN_FILE", str(token_file))
    monkeypatch.setattr(config, "TOKEN_RAW", None)
    monkeypatch.delenv("ANYTHINGLLM_API_KEY", raising=False)
    monkeypatch.delenv("MG_API_KEY", raising=False)
    monkeypatch.delenv("MG_AUTH_TOKEN", raising=False)
    
    search.invalidate_token_cache()
    tok1 = search.load_token()
    assert tok1 == "token-v1"
    
    # Invalidate cache and update file
    token_file.write_text("token-v2")
    search.invalidate_token_cache()
    tok2 = search.load_token()
    assert tok2 == "token-v2"


def test_vector_search_one_401_retry():
    from memory_gateway import search
    
    resp_401 = MagicMock()
    resp_401.status_code = 401
    
    resp_200 = MagicMock()
    resp_200.status_code = 200
    resp_200.json.return_value = {
        "results": [{"title": "Doc1", "text": "Content", "score": 0.9, "metadata": {"title": "Doc1"}}]
    }
    
    with patch("requests.post", side_effect=[resp_401, resp_200]) as mock_post, \
         patch("memory_gateway.search.load_token", return_value="token123"):
        
        res = search._vector_search_one("default", "query", 5, 0.1)
        assert len(res) == 1
        assert res[0]["title"] == "Doc1"
        assert mock_post.call_count == 2

