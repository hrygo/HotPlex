"""
HotPlex Python SDK

A production-ready Python client for the HotPlex AI Agent Control Plane.
"""

__version__ = "0.1.0"
__author__ = "HotPlex Team"

from .client import HotPlexClient
from .errors import (
    HotPlexError,
    ConnectionError,
    TimeoutError,
    ExecutionError,
    DangerBlockedError,
    SessionError,
)
from .events import Event, EventType
from .config import Config

__all__ = [
    "HotPlexClient",
    "HotPlexError",
    "ConnectionError",
    "TimeoutError",
    "ExecutionError",
    "DangerBlockedError",
    "SessionError",
    "Event",
    "EventType",
    "Config",
]
