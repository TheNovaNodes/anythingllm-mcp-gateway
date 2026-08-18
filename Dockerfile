FROM python:3.12-slim AS builder

WORKDIR /app
COPY . /app

RUN pip install --no-cache-dir build \
    && python -m build --wheel \
    && pip install --no-cache-dir dist/*.whl

FROM python:3.12-slim
WORKDIR /app
COPY --from=builder /usr/local/bin/anythingllm-mcp-gateway /usr/local/bin/
COPY --from=builder /usr/local/lib/python3.12/site-packages /usr/local/lib/python3.12/site-packages

# Create volumes for data directories mentioned in .env
VOLUME ["/data"]

# Add a non-root user
RUN useradd -m appuser \
    && chown -R appuser:appuser /app \
    && mkdir -p /data \
    && chown -R appuser:appuser /data

USER appuser

ENTRYPOINT ["anythingllm-mcp-gateway"]
