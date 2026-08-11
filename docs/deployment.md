# Инструкция по Развертыванию

В этом документе описаны шаги для запуска и развертывания шлюза `anythingllm-mcp-gateway`.

## Предварительные требования

- Python 3.10 или выше
- Существующий инстанс AnythingLLM с настроенным API Key (Bearer Token).
- Настроенная директория `anythingllm-sync` с файлами `lexical.db` и `workspace_map.json`.

## Локальное развертывание

### 1. Клонирование репозитория

```bash
git clone https://github.com/TheNovaNodes/anythingllm-mcp-gateway.git
cd anythingllm-mcp-gateway
```

### 2. Создание виртуального окружения

Создайте и активируйте изолированное окружение:

```bash
python3 -m venv venv
source venv/bin/activate
```

### 3. Установка пакета

Установите шлюз в режиме разработки (или как стандартный пакет):

```bash
pip install -e .
```

### 4. Конфигурация

Скопируйте пример файла конфигурации:

```bash
cp .env.example .env
```

#### Таблица Переменных Окружения

| Переменная | Описание | Пример значения |
| :--- | :--- | :--- |
| `MG_ALM_BASE` | Базовый URL API AnythingLLM. | `http://127.0.0.1:3001/api/v1` |
| `MG_TOKEN_FILE` | Путь к файлу, содержащему Bearer Token для AnythingLLM. | `/path/to/anythingllm_token.txt` |
| `MG_OPS_DIR` | Директория с операционными файлами. | `/path/to/anythingllm-sync` |
| `MG_LEXICAL_DB` | Путь к локальной лексической БД FTS5/BM25. | `{MG_OPS_DIR}/lexical.db` |
| `MG_MAP_FILE` | Файл с картой воркспейсов. | `/path/to/anythingllm_workspaces.map` |
| `MG_DEFAULT_TOP_K` | Количество результатов поиска по умолчанию. | `5` |
| `MG_FUSION_MODE` | Стратегия объединения (`weighted` или `rrf`). | `weighted` |
| `MG_TRANSPORT` | Протокол связи (`stdio` или `streamable-http`). | `stdio` |
| `MG_HOST` | Хост для сетевого режима. | `127.0.0.1` |
| `MG_PORT` | Порт для сетевого режима. | `8091` |
| `MG_LOG_LEVEL` | Уровень логирования. | `INFO` |

### 5. Запуск Сервера

Запуск шлюза как самостоятельного процесса:

```bash
python3 -m memory_gateway.server
```

> **Примечание:** Если `MG_TRANSPORT=stdio`, сервер не будет выводить логи в консоль стандартным образом, а будет ожидать коммуникации от MCP Клиента через стандартные потоки ввода/вывода (stdin/stdout).

Для интеграции с MCP Клиентом (например, Claude Desktop) настройте конфигурацию клиента для вызова:
```json
{
  "mcpServers": {
    "anythingllm-mcp-gateway": {
      "command": "python",
      "args": ["-m", "memory_gateway.server"],
      "cwd": "/path/to/anythingllm-mcp-gateway",
      "env": {
        "ANYTHINGLLM_API_KEY": "YOUR_ANYTHINGLLM_API_KEY",
        "MG_ALM_BASE": "http://127.0.0.1:3001/api/v1"
      }
    }
  }
}
```

---

## Docker Развертывание

### TODO: Docker Конфигурация
На данный момент официальный Dockerfile и `docker-compose.yml` не представлены в кодовой базе.
В будущем этот раздел должен содержать:
- Инструкции по сборке образа (например, `docker build -t anythingllm-mcp-gateway .`).
- Конфигурацию Docker-контейнеров: сети, тома (для проброса `lexical.db` и `.env`), проброс портов (если используется `streamable-http`).
- Взаимодействие между управляющими нодами.
