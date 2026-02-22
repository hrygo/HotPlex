"""HotPlex SDK Event Types"""

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Any, Optional


class EventType(str, Enum):
    """Event types emitted during execution."""

    THINKING = "thinking"
    ANSWER = "answer"
    TOOL_USE = "tool_use"
    TOOL_RESULT = "tool_result"
    SESSION_STATS = "session_stats"
    ERROR = "error"
    DANGER_BLOCK = "danger_block"


@dataclass
class EventMeta:
    """Metadata associated with events."""

    tool_name: Optional[str] = None
    tool_id: Optional[str] = None
    status: Optional[str] = None
    duration_ms: Optional[int] = None
    total_duration_ms: Optional[int] = None
    input_summary: Optional[str] = None
    output_summary: Optional[str] = None


@dataclass
class Event:
    """Represents an event from the HotPlex server."""

    type: EventType
    data: Any = None
    meta: Optional[EventMeta] = field(default_factory=EventMeta)
    timestamp: datetime = field(default_factory=datetime.now)

    @classmethod
    def from_dict(cls, data: dict) -> "Event":
        """Create an Event from a dictionary."""
        event_type = EventType(data.get("type", "answer"))
        event_data = data.get("data")

        meta_data = data.get("meta", {})
        meta = EventMeta(
            tool_name=meta_data.get("tool_name"),
            tool_id=meta_data.get("tool_id"),
            status=meta_data.get("status"),
            duration_ms=meta_data.get("duration_ms"),
            total_duration_ms=meta_data.get("total_duration_ms"),
            input_summary=meta_data.get("input_summary"),
            output_summary=meta_data.get("output_summary"),
        )

        return cls(
            type=event_type,
            data=event_data,
            meta=meta,
        )


@dataclass
class SessionStats:
    """Session statistics."""

    session_id: str
    start_time: int
    end_time: int
    total_duration_ms: int
    input_tokens: int
    output_tokens: int
    total_tokens: int
    tool_call_count: int
    tools_used: list[str]
    files_modified: int
    file_paths: list[str]
    model_used: str
    total_cost_usd: float
    is_error: bool
    error_message: Optional[str] = None

    @classmethod
    def from_dict(cls, data: dict) -> "SessionStats":
        """Create SessionStats from a dictionary."""
        return cls(
            session_id=data.get("session_id", ""),
            start_time=data.get("start_time", 0),
            end_time=data.get("end_time", 0),
            total_duration_ms=data.get("total_duration_ms", 0),
            input_tokens=data.get("input_tokens", 0),
            output_tokens=data.get("output_tokens", 0),
            total_tokens=data.get("total_tokens", 0),
            tool_call_count=data.get("tool_call_count", 0),
            tools_used=data.get("tools_used", []),
            files_modified=data.get("files_modified", 0),
            file_paths=data.get("file_paths", []),
            model_used=data.get("model_used", ""),
            total_cost_usd=data.get("total_cost_usd", 0.0),
            is_error=data.get("is_error", False),
            error_message=data.get("error_message"),
        )
