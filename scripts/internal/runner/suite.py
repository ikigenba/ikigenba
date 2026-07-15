"""Runner-injected client surface for ikigenba suite services."""

import hashlib
import json
import os
import shutil
import tempfile
import urllib.parse
import urllib.request


HTTP_TIMEOUT_SECONDS = 30
_EVENT_UNSET = object()
_event_value = _EVENT_UNSET


class ToolError(Exception):
    """A failure returned by a suite service."""

    def __init__(self, code, message):
        super().__init__(f"{code}: {message}")
        self.code = code
        self.message = message


def _runtime_value(name):
    try:
        return os.environ[name]
    except KeyError:
        raise RuntimeError(
            f"suite: not running under the scripts runner (missing {name})"
        ) from None


def event():
    """Return the trigger payload verbatim, parsing it only once."""

    global _event_value
    if _event_value is _EVENT_UNSET:
        _event_value = json.loads(_runtime_value("EVENT_JSON"))
    return _event_value


def mcp(service, verb, arguments=None):
    """Call a service MCP verb (implemented by the next client phase)."""

    json.loads(_runtime_value("SUITE_SERVICES"))
    raise NotImplementedError("suite.mcp is not implemented yet")


class _Files:
    """Namespace reserved for the file-share client surface."""


files = _Files()
