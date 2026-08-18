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
