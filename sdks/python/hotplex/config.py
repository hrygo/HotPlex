"""HotPlex SDK Configuration"""

from dataclasses import dataclass, field
from typing import Optional


@dataclass
class Config:
    """Configuration for HotPlex execution."""

    work_dir: str
    session_id: str
    task_instructions: Optional[str] = None

    def to_dict(self) -> dict:
        """Convert to dictionary for JSON serialization."""
        result = {
            "work_dir": self.work_dir,
            "session_id": self.session_id,
        }
        if self.task_instructions:
            result["task_instructions"] = self.task_instructions
        return result


@dataclass
class ClientConfig:
    """Configuration for HotPlexClient."""

    url: str = "ws://localhost:8080/ws/v1/agent"
    timeout: float = 300.0
    reconnect: bool = True
    reconnect_attempts: int = 5
    reconnect_delay: float = 1.0
    log_level: str = "INFO"
    api_key: Optional[str] = None
