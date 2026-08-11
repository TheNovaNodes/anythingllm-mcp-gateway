# Архитектура `anythingllm-mcp-gateway`

Данный документ описывает архитектуру и механизмы работы `anythingllm-mcp-gateway`, высокопроизводительного шлюза по протоколу Model Context Protocol (MCP).

## Визуализация Архитектуры

```mermaid
graph TD
    Client[MCP Client <br> agy / LLM]
    Gateway[anythingllm-mcp-gateway <br> FastMCP Server]
    VectorLayer[Vector Layer <br> AnythingLLM REST API]
    LexicalLayer[Lexical Layer <br> SQLite FTS5]
    FusionEngine[Hybrid Fusion Engine]

    Client -- FastMCP JSON-RPC --> Gateway
    Gateway -- Vector Search --> VectorLayer
    Gateway -- Lexical Search --> LexicalLayer

    VectorLayer -. Scores .-> FusionEngine
    LexicalLayer -. Scores .-> FusionEngine

    FusionEngine -. Calibrates & Fuses .-> Gateway
    Gateway -. Result JSON .-> Client

    subgraph "Vector Layer (AnythingLLM)"
    VectorLayer
    VL1[REST API /vector-search]
    VL2[Fan-out Throttle]
    VL3[Memory Cache TTL 120s]
    end

    subgraph "Lexical Layer (FTS5)"
    LexicalLayer
    LL1[SQLite docs_fts BM25]
    LL2[Read-Only Access]
    LL3[Paragraph Context Assembly]
    end

    subgraph "Hybrid Fusion Engine"
    FusionEngine
    FE1[Score Calibration]
    FE2[Temporal Decay Scale]
    FE3[Token Budget Trimmer]
    end
```

## Внутренняя Логика и Алгоритмы

### 1. Гибридный Поиск (Score-Calibrated Weighted Fusion)
Шлюз использует комбинацию семантического (векторного) и лексического (FTS5) поиска. В отличие от базового алгоритма Reciprocal Rank Fusion (RRF), шлюз применяет скоринговую нормализацию в диапазоне `[0, 1]`.

Формула:
`S_fused = α * MinMax(S_vec) + (1 - α) * MinMax(S_lex)`

Где `α = 0.6` по умолчанию, что отдает приоритет семантическому вектору, но сохраняет вес точных лексических совпадений из SQLite.

### 2. Учет Свежести Документов (Temporal Decay Scaling)
Для того чтобы ИИ не использовал устаревшие данные, к итоговому скору применяется экспоненциальное затухание в зависимости от возраста документа.

Формула:
`D_temporal = max(0.6, exp(-λ * Δt_days))`

Где `λ = 0.005`. Свежие документы имеют вес `1.0`, в то время как старые постепенно снижаются до порога в `0.6`.

### 3. Адаптивное Бюджетирование Токенов (Adaptive Token Budgeting)
Шлюз поддерживает ограничение выдачи по количеству токенов (`max_token_budget`). Когда бюджет приближается к концу:
- Размеры токенов вычисляются аппроксимацией (`символы / 4`).
- Последний фрагмент, превышающий бюджет, обрезается по границам предложений.
- В результат добавляется флаг `trimmed_to_budget: true`, сообщая клиенту о том, что результат был усечен.

### 4. Сборка Контекста (Context Assembly)
Когда параметр `expand_context` включен, результаты поиска расширяются за счет подтягивания соседних абзацев из оригинального документа (в пределах того же файла). Это обеспечивает агента более связным и полным контекстом для анализа, что критично для сложных логических рассуждений.
