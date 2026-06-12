"""Shared utilities for image-resize skill."""

import logging
import os
import re

import yaml


def setup_logging(verbose: bool = False) -> None:
    """Configure logging with appropriate level and format."""
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%H:%M:%S",
    )


def load_yaml(path: str) -> dict:
    """Load a YAML file and return its contents as a dict."""
    with open(path, "r", encoding="utf-8") as f:
        return yaml.safe_load(f) or {}


def resolve_env_vars(config: dict) -> dict:
    """Resolve ${ENV_VAR:default} patterns in config string values."""
    pattern = re.compile(r"\$\{(\w+)(?::([^}]*))?\}")

    def _resolve(value):
        if isinstance(value, str):
            def replacer(m):
                env_var, default = m.group(1), m.group(2)
                return os.environ.get(env_var, default if default is not None else m.group(0))
            return pattern.sub(replacer, value)
        elif isinstance(value, dict):
            return {k: _resolve(v) for k, v in value.items()}
        elif isinstance(value, list):
            return [_resolve(v) for v in value]
        return value

    return _resolve(config)


def get_project_root() -> str:
    """Return the image-resize skill root directory (parent of scripts/)."""
    return os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
