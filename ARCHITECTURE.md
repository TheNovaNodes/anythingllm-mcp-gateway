# 📐 anythingllm-mcp-gateway Architecture & Design Specification

> **Автор**: Trickster (`trickster@labdoctorm.ru`)  
> **Организация**: TheNovaNodes  
> **Версия**: v0.2.0 (Product-Ready)  
> **Стек**: Python 3.10+ / FastMCP / SQLite FTS5 / AnythingLLM REST API

---

## 🏛️ 1. Обзор архитектуры шлюза

**`anythingllm-mcp-gateway`** (`TheNovaNodes/anythingllm-mcp-gateway`) — это промышленный шлюз по протоколу **Model Context Protocol (MCP)**, обеспечивающий высокопроизводительный доступ агентов ИИ к семантической памяти.

```
                               ┌─────────────────────────┐
                               │  MCP Client (agy / LLM) │
                               └────────────┬────────────┘
                                            │ (FastMCP JSON-RPC)
                                            ▼
                               ┌─────────────────────────┐
                               │ anythingllm-mcp-gateway │
                               │        (v0.2.0)         │
                               └────────────┬────────────┘
                                            │
               ┌────────────────────────────┴────────────────────────────┐
               ▼                                                         ▼
┌──────────────────────────────┐                          ┌──────────────────────────────┐
│  Vector Layer (AnythingLLM)  │                          │    Lexical Layer (FTS5)      │
│  • REST API /vector-search   │                          │  • SQLite docs_fts (BM25)    │
│  • Fan-out Throttle (Sem)    │                          │  • Read-Only Access (mode=ro) │
│  • Memory Cache (TTL 120s)   │                          │  • Paragraph Context Assembly│
└──────────────┬───────────────┘                          └──────────────┬───────────────┘
               │                                                         │
               └────────────────────────────┬────────────────────────────┘
                                            ▼
                               ┌─────────────────────────┐
                               │   Hybrid Fusion Engine  │
                               │ • Score Calibration     │
                               │ • Temporal Decay Scale  │
                               │ • Token Budget Trimmer  │
                               │ • Dependency Graphing   │
                               └─────────────────────────┘
```

---

## 🔬 2. Математические и алгоритмические модели

### 2.1. Score-Calibrated Weighted Fusion
Вместо базового RRF, приводящего позиции к $1/(k+rank)$, шлюз применяет скоринговую нормализацию в диапазоне $[0, 1]$:

$$S_{fused} = \alpha \cdot \text{MinMax}(S_{vec}) + (1 - \alpha) \cdot \text{MinMax}(S_{lex})$$

Где $\alpha = 0.6$ по умолчанию, отдавая небольшой приоритет семантическому вектору, но сохраняя вес точных лексических совпадений FTS5.

### 2.2. Temporal Decay Scaling (Учет свежести документов)
Для защиты агента от использования устаревших кодовых правил и решений к итоговому скору применяется экспоненциальный коэффициент затухания:

$$D_{temporal} = \max\left(0.6, \exp(-\lambda \cdot \Delta t_{days})\right)$$

где $\lambda = 0.005$. Документ, созданный сегодня, получает значение $1.0$, а документ полугодовой давности плавно снижает свой вес до пороговых $0.6$.

### 2.3. Adaptive Token Budgeting
При вызове `search_memory` с параметром `max_token_budget`:
1. Результаты последовательно суммируют объемы токенов ($\approx \text{chars} / 4$).
2. На пороговом значении последний фрагмент аккуратно обрезается по границам предложений и слов с добавлением маркера `trimmed_to_budget: true`.

---

## 🛠️ 3. Спецификация MCP-инструментов

### `search_memory`
- **Параметры**: `query`, `top_k`, `workspace`, `expand_context`, `max_token_budget`, `tier`.
- **Выход**: `{query, count, results[], degraded, layers, total_estimated_tokens}`.

### `store_memory` (Active Ingestion)
- **Параметры**: `content`, `title`, `workspace`, `metadata`, `tier`.
- **Механика**: Прямая загрузка через `/api/v1/document/raw-text` и синхронизация эмбеддингов воркспейса через `/update-embeddings`.

### `get_document`
- **Параметры**: `doc_id`, `max_chars`.
- **Механика**: Извлечение полных raw-данных документа из `docs_fts`.

### `gateway_health`
- **Механика**: Реальная тестовая вылазка гибридного поиска с отчетом о *degraded mode* при падении векторного слоя.

---

## 🧪 4. Тестирование

```bash
python3 -m unittest discover -s tests
```
Все тесты выполняются в изолированной временной директории с генерацией валидного токена доступа.
