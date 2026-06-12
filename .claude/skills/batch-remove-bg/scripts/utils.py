"""Utility functions for config loading and logging setup."""

import logging
import os
import re

import yaml


def setup_logging(verbose: bool = False) -> None:
    """Configure logging with optional debug level."""
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        datefmt="%H:%M:%S",
    )


def load_yaml(path: str) -> dict:
    """Load a YAML file and return its contents as a dict."""
    with open(path, "r") as f:
        return yaml.safe_load(f)


def resolve_env_vars(config: dict) -> dict:
    """Recursively resolve ${ENV_VAR} and ${ENV_VAR:default} in config values."""
    pattern = re.compile(r"\$\{([^}]+)\}")

    def _resolve(value):
        if isinstance(value, str):
            def replacer(match):
                expr = match.group(1)
                if ":" in expr:
                    var, default = expr.split(":", 1)
                    return os.environ.get(var, default)
                return os.environ.get(expr, match.group(0))
            return pattern.sub(replacer, value)
        elif isinstance(value, dict):
            return {k: _resolve(v) for k, v in value.items()}
        elif isinstance(value, list):
            return [_resolve(item) for item in value]
        return value

    return _resolve(config)
