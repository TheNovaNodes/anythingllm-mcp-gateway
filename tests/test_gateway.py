import unittest
from unittest.mock import patch, MagicMock
import os
import tempfile

from memory_gateway import search, server, config


class TestMemoryGateway(unittest.TestCase):

    def setUp(self):
        self.temp_dir = tempfile.TemporaryDirectory()
        self.token_file = os.path.join(self.temp_dir.name, "token.txt")
        with open(self.token_file, "w", encoding="utf-8") as f:
            f.write("0RYFPN9-S6648E5-QX9SG9F-0C8DXPD")
        os.chmod(self.token_file, 0o600)
        config.TOKEN_FILE = self.token_file
        search._token_cache = None

    def tearDown(self):
        self.temp_dir.cleanup()
        search._token_cache = None

    def test_load_token(self):
        tok = search.load_token()
        self.assertEqual(tok, "0RYFPN9-S6648E5-QX9SG9F-0C8DXPD")

    @patch("memory_gateway.search.requests.post")
    def test_store_memory_success(self, mock_post):
        # Mock document upload
        resp_raw = MagicMock()
        resp_raw.ok = True
        resp_raw.json.return_value = {
            "documents": [{"id": "doc-123", "location": "custom-documents/test.json"}]
        }
        
        # Mock workspace embedding update
        resp_ws = MagicMock()
        resp_ws.ok = True
        resp_ws.json.return_value = {"workspace": {"id": 1, "slug": "dmagybot"}}

        mock_post.side_effect = [resp_raw, resp_ws]

        res = search.store_memory(
            content="User prefers Python 3.12 and dark mode",
            title="user_prefs.txt",
            workspace="dmagybot",
            tier="episodic"
        )
        self.assertTrue(res["success"])
        self.assertEqual(res["doc_id"], "doc-123")
        self.assertEqual(res["workspace"], "dmagybot")

    @patch("memory_gateway.search.vector_search")
    @patch("memory_gateway.search.lexical_search")
    def test_hybrid_search_with_token_budget(self, mock_lexical, mock_vector):
        mock_vector.return_value = [
            {"source": "vector", "workspace": "dmagybot", "title": "Doc 1", "doc_id": "doc1", "text": "A" * 400, "vector_score": 0.9},
            {"source": "vector", "workspace": "dmagybot", "title": "Doc 2", "doc_id": "doc2", "text": "B" * 400, "vector_score": 0.8}
        ]
        mock_lexical.return_value = []

        res = search.hybrid_search("python", top_k=2, expand_context=False, max_token_budget=150)
        self.assertLessEqual(res["total_estimated_tokens"], 150)
        self.assertTrue(res["count"] > 0)

    def test_temporal_decay_calculation(self):
        # Existing file should have decay factor <= 1.0
        decay = search._calculate_temporal_decay(self.token_file)
        self.assertGreaterEqual(decay, 0.6)
        self.assertLessEqual(decay, 1.0)

    def test_extract_related_docs(self):
        sample_text = "See [architecture](docs/arch.md) and import os\nfrom memory_gateway.search import hybrid_search"
        related = search._extract_related_docs(sample_text)
        self.assertIn("docs/arch.md", related)
        self.assertIn("memory_gateway/search.py", related)

    @patch("memory_gateway.search.requests.get")
    def test_get_document_api_fallback(self, mock_get):
        # Test fallback to AnythingLLM API when lexical DB is missing or doesn't have the doc
        config.LEXICAL_DB = "/non_existent_path.db"

        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.json.return_value = {
            "document": {"title": "fallback_doc.md"}
        }
        mock_get.return_value = mock_resp

        res = search.get_document("fallback_doc.md", max_chars=100)
        self.assertTrue(res["found"])
        self.assertEqual(res["source"], "anythingllm-metadata")
        self.assertEqual(res["title"], "fallback_doc.md")

    def test_get_document_empty_id(self):
        res = search.get_document("")
        self.assertFalse(res["found"])
        self.assertEqual(res["error"], "empty doc_id")


if __name__ == "__main__":
    unittest.main()
