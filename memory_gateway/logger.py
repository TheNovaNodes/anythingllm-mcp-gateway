"""Simple and reliable logging for memory-gateway.

Writes to a file (config.LOG_FILE) with size rotation + duplicates WARNING+
to stderr. Never crashes the process due to logging issues.
"""
import logging
import os
import sys
from logging.handlers import RotatingFileHandler

from . import config

_logger = None


def get_logger():
    global _logger
    if _logger is not None:
        return _logger

    log = logging.getLogger("memory-gateway")
    log.setLevel(getattr(logging, config.LOG_LEVEL.upper(), logging.INFO))
    log.propagate = False

    fmt = logging.Formatter(
        "%(asctime)s %(levelname)s %(name)s %(message)s",
        datefmt="%Y-%m-%dT%H:%M:%S",
    )

    # File handler (best-effort: if directory is inaccessible, fallback to stderr only).
    try:
        os.makedirs(config.LOG_DIR, exist_ok=True)
        fh = RotatingFileHandler(
            config.LOG_FILE, maxBytes=5 * 1024 * 1024, backupCount=3, encoding="utf-8"
        )
        fh.setFormatter(fmt)
        log.addHandler(fh)
    except Exception as e:  # noqa: BLE001 — logging should not crash the app
        sys.stderr.write(f"[memory-gateway] file log disabled: {e}\n")

    # stderr for WARNING+ (stdout is occupied by MCP stdio transport — do not use it!).
    sh = logging.StreamHandler(sys.stderr)
    sh.setLevel(logging.WARNING)
    sh.setFormatter(fmt)
    log.addHandler(sh)

    _logger = log
    return log
