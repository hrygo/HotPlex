"""HotPlex Python SDK Client"""

import json
import logging
import threading
import time
from typing import Callable, Optional
from queue import Queue, Empty

try:
    import websocket
except ImportError:
    websocket = None

from .config import Config, ClientConfig
from .errors import (
    ConnectionError,
    TimeoutError,
    ExecutionError,
    DangerBlockedError,
)
from .events import Event, EventType, SessionStats


logger = logging.getLogger("hotplex")


class HotPlexClient:
    """
    Python client for HotPlex AI Agent Control Plane.

    Usage:
        client = HotPlexClient(url="ws://localhost:8080/ws/v1/agent")
        client.connect()

        for event in client.execute(
            prompt="Hello, world!",
            config=Config(work_dir="/tmp", session_id="test")
        ):
            print(f"{event.type}: {event.data}")

        client.close()
    """

    def __init__(self, config: Optional[ClientConfig] = None):
        if websocket is None:
            raise ImportError(
                "websocket-client is required. Install with: pip install websocket-client"
            )

        self.config = config or ClientConfig()
        self._ws: Optional[websocket.WebSocket] = None
        self._connected = False
        self._event_queue: Queue = Queue()
        self._receive_thread: Optional[threading.Thread] = None
        self._request_id = 0

    def connect(self) -> None:
        """Connect to the HotPlex server."""
        if self._connected:
            return

        try:
            headers = {}
            if self.config.api_key:
                headers["Authorization"] = f"Bearer {self.config.api_key}"

            self._ws = websocket.create_connection(
                self.config.url,
                header=headers,
            )
            self._connected = True
            self._receive_thread = threading.Thread(
                target=self._receive_loop, daemon=True
            )
            self._receive_thread.start()
            logger.info(f"Connected to HotPlex at {self.config.url}")
        except Exception as e:
            raise ConnectionError(f"Failed to connect: {e}") from e

    def close(self) -> None:
        """Close the connection."""
        self._connected = False
        if self._ws:
            try:
                self._ws.close()
            except Exception as e:
                logger.debug(f"Error closing WebSocket: {e}")
            self._ws = None
        logger.info("Disconnected from HotPlex")

    def execute(
        self,
        prompt: str,
        config: Config,
        on_event: Optional[Callable[[Event], None]] = None,
        timeout: Optional[float] = None,
    ) -> list[Event]:
        """
        Execute a prompt and collect events.

        Args:
            prompt: The prompt to send
            config: Execution configuration
            on_event: Optional callback for each event
            timeout: Optional timeout in seconds

        Returns:
            List of events received during execution
        """
        if not self._connected:
            self.connect()

        timeout = timeout or self.config.timeout
        events = []
        done = False

        self._request_id += 1
        request_id = self._request_id

        request = {
            "request_id": request_id,
            "type": "execute",
            "prompt": prompt,
            "config": config.to_dict(),
        }

        try:
            self._ws.send(json.dumps(request))
        except Exception as e:
            raise ConnectionError(f"Failed to send request: {e}") from e

        start_time = time.time()

        while not done:
            elapsed = time.time() - start_time
            if elapsed > timeout:
                raise TimeoutError(f"Execution timed out after {timeout}s")

            try:
                event = self._event_queue.get(timeout=1.0)
            except Empty:
                continue

            if event is None:
                break

            events.append(event)

            if on_event:
                on_event(event)

            if event.type == EventType.SESSION_STATS:
                done = True
            elif event.type == EventType.ERROR:
                raise ExecutionError(str(event.data))
            elif event.type == EventType.DANGER_BLOCK:
                raise DangerBlockedError(str(event.data))

        return events

    def execute_stream(
        self,
        prompt: str,
        config: Config,
        timeout: Optional[float] = None,
    ):
        """
        Execute a prompt and yield events as they arrive.

        This is a generator that yields Event objects.
        """
        if not self._connected:
            self.connect()

        timeout = timeout or self.config.timeout

        self._request_id += 1
        request_id = self._request_id

        request = {
            "request_id": request_id,
            "type": "execute",
            "prompt": prompt,
            "config": config.to_dict(),
        }

        try:
            self._ws.send(json.dumps(request))
        except Exception as e:
            raise ConnectionError(f"Failed to send request: {e}") from e

        start_time = time.time()

        while True:
            elapsed = time.time() - start_time
            if elapsed > timeout:
                raise TimeoutError(f"Execution timed out after {timeout}s")

            try:
                event = self._event_queue.get(timeout=1.0)
            except Empty:
                continue

            if event is None:
                break

            yield event

            if event.type == EventType.SESSION_STATS:
                break
            elif event.type == EventType.ERROR:
                raise ExecutionError(str(event.data))
            elif event.type == EventType.DANGER_BLOCK:
                raise DangerBlockedError(str(event.data))

    def _receive_loop(self) -> None:
        """Background thread to receive messages."""
        while self._connected and self._ws:
            try:
                message = self._ws.recv()
                if not message:
                    continue

                data = json.loads(message)
                event = Event.from_dict(data)
                self._event_queue.put(event)

            except websocket.WebSocketConnectionClosedException:
                self._connected = False
                self._event_queue.put(None)
                break
            except json.JSONDecodeError as e:
                logger.error(f"Failed to decode message: {e}")
            except Exception as e:
                logger.error(f"Error in receive loop: {e}")
                self._connected = False
                self._event_queue.put(None)
                break

    def __enter__(self) -> "HotPlexClient":
        self.connect()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        self.close()
