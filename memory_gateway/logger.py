"""Простое надёжное логирование ошибок memory-gateway.

Пишет в файл (config.LOG_FILE) с ротацией по размеру + дублирует WARNING+
в stderr. Никогда не роняет процесс из-за проблем логирования.
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

    # Файловый handler (best-effort: если каталог недоступен — только stderr).
    try:
        os.makedirs(config.LOG_DIR, exist_ok=True)
        fh = RotatingFileHandler(
            config.LOG_FILE, maxBytes=5 * 1024 * 1024, backupCount=3, encoding="utf-8"
        )
        fh.setFormatter(fmt)
        log.addHandler(fh)
    except Exception as e:  # noqa: BLE001 — логирование не должно падать
        sys.stderr.write(f"[memory-gateway] file log disabled: {e}\n")

    # stderr для WARNING+ (stdout занят stdio-транспортом MCP — туда нельзя!).
    sh = logging.StreamHandler(sys.stderr)
    sh.setLevel(logging.WARNING)
    sh.setFormatter(fmt)
    log.addHandler(sh)

    _logger = log
    return log
