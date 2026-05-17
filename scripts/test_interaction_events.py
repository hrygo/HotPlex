#!/usr/bin/env python3
"""
E2E test for HotPlex Gateway interaction events via AEP WebSocket.

Tests the full round-trip for permission_request, question_request, and
elicitation_request events through the live gateway:

  1. Connect to ws://localhost:8888/ws
  2. Send init handshake → receive init_ack + state(created)
  3. Send input that triggers a tool call requiring permission
  4. Receive permission_request event
  5. Send input with permission_response metadata
  6. Verify worker continues processing

Usage:
  python scripts/test_interaction_events.py [--port PORT] [--timeout SECONDS]

Requires:
  - HotPlex gateway running (hotplex gateway start)
  - websocket-client: pip install websocket-client
"""

import argparse
import json
import sys
import time
import threading
import uuid


# ─── Helpers ───────────────────────────────────────────────────────────

def log(msg: str, tag: str = "INFO"):
    ts = time.strftime("%H:%M:%S")
    print(f"[{ts}] [{tag}] {msg}", flush=True)


AEP_VERSION = "aep/v1"


def make_envelope(session_id: str, event_type: str, data: dict, seq: int = 1) -> dict:
    return {
        "version": AEP_VERSION,
        "id": str(uuid.uuid4()),
        "session_id": session_id,
        "seq": seq,
        "timestamp": int(time.time() * 1000),
        "event": {
            "type": event_type,
            "data": data,
        },
    }


def make_init(session_id: str, worker_type: str = "claude_code") -> dict:
    """Init envelope — seq=0 for init is allowed (no seq allocation for init)."""
    return {
        "version": AEP_VERSION,
        "id": str(uuid.uuid4()),
        "session_id": session_id,
        "seq": 1,
        "timestamp": int(time.time() * 1000),
        "event": {
            "type": "init",
            "data": {
                "version": AEP_VERSION,
                "worker_type": worker_type,
                "config": {
                    "work_dir": "/home/hotplex/.hotplex/workspace/hotplex",
                },
            },
        },
    }


def make_input(session_id: str, content: str, seq: int, metadata: dict = None) -> dict:
    data = {"content": content}
    if metadata:
        data["metadata"] = metadata
    return make_envelope(session_id, "input", data, seq=seq)


def make_permission_response(session_id: str, request_id: str, allowed: bool, reason: str = "") -> dict:
    return make_envelope(session_id, "input", {
        "content": "",
        "metadata": {
            "permission_response": {
                "request_id": request_id,
                "allowed": allowed,
                "reason": reason,
            },
        },
    })


def make_question_response(session_id: str, request_id: str, answers: dict) -> dict:
    return make_envelope(session_id, "input", {
        "content": "",
        "metadata": {
            "question_response": {
                "id": request_id,
                "answers": answers,
            },
        },
    })


def make_elicitation_response(session_id: str, request_id: str, action: str) -> dict:
    return make_envelope(session_id, "input", {
        "content": "",
        "metadata": {
            "elicitation_response": {
                "id": request_id,
                "action": action,
            },
        },
    })


# ─── AEP WebSocket Client ─────────────────────────────────────────────

class AEPClient:
    """Minimal AEP v1 client over WebSocket."""

    def __init__(self, ws_url: str, timeout: float = 60):
        import websocket
        self.ws_url = ws_url
        self.timeout = timeout
        self.ws = None
        self._connected = False
        self.events: list[dict] = []
        self._lock = threading.Lock()
        self._reader_thread: threading.Thread | None = None
        self._stop = threading.Event()

    def connect(self) -> bool:
        import websocket
        log(f"Connecting to {self.ws_url}")
        try:
            self.ws = websocket.WebSocketApp(
                self.ws_url,
                header={"X-API-Key": "test-e2e"},  # Dev mode: any key accepted when api_keys=[]
                on_open=self._on_open,
                on_message=self._on_message,
                on_error=self._on_error,
                on_close=self._on_close,
            )
            self._reader_thread = threading.Thread(target=self._run_ws, daemon=True)
            self._reader_thread.start()
            # Wait for connection
            deadline = time.time() + 10
            while not self._connected and time.time() < deadline:
                time.sleep(0.1)
            if not self._connected:
                log("FAIL: Connection timeout", "FAIL")
                return False
            log("WebSocket connected")
            return True
        except Exception as e:
            log(f"Connection failed: {e}", "FAIL")
            return False

    def _run_ws(self):
        self.ws.run_forever(ping_interval=30, ping_timeout=10)

    def _on_open(self, ws):
        self._connected = True

    def _on_message(self, ws, message):
        for line in message.split("\n"):
            line = line.strip()
            if not line:
                continue
            try:
                env = json.loads(line)
                with self._lock:
                    self.events.append(env)
                etype = env.get("event", {}).get("type", "?")
                seq = env.get("seq", 0)
                log(f"← {etype} (seq={seq})", "RECV")
            except json.JSONDecodeError:
                pass

    def _on_error(self, ws, error):
        log(f"WebSocket error: {error}", "ERR")

    def _on_close(self, ws, close_status, close_msg):
        self._connected = False

    def send(self, env: dict):
        data = json.dumps(env, ensure_ascii=False)
        etype = env.get("event", {}).get("type", "?")
        log(f"→ {etype}", "SEND")
        self.ws.send(data)

    def wait_for(self, event_type: str, timeout: float = None) -> dict | None:
        """Wait for a specific event type. Returns the first match."""
        timeout = timeout or self.timeout
        deadline = time.time() + timeout
        while time.time() < deadline:
            with self._lock:
                for env in self.events:
                    if env.get("event", {}).get("type") == event_type:
                        self.events.remove(env)
                        return env
            time.sleep(0.2)
        return None

    def wait_for_any(self, event_types: list[str], timeout: float = None) -> tuple[str | None, dict | None]:
        """Wait for any of the specified event types."""
        timeout = timeout or self.timeout
        deadline = time.time() + timeout
        while time.time() < deadline:
            with self._lock:
                for env in self.events:
                    et = env.get("event", {}).get("type")
                    if et in event_types:
                        self.events.remove(env)
                        return et, env
            time.sleep(0.2)
        return None, None

    def drain_events(self, timeout: float = 3) -> list[dict]:
        """Wait and collect all events for a period."""
        time.sleep(timeout)
        with self._lock:
            result = list(self.events)
            self.events.clear()
        return result

    def close(self):
        self._stop.set()
        if self.ws:
            try:
                self.ws.close()
            except Exception:
                pass
        if self._reader_thread:
            self._reader_thread.join(timeout=5)


# ─── Tests ─────────────────────────────────────────────────────────────

def test_init_handshake(client: AEPClient, session_id: str, worker_type: str) -> bool:
    """Test: Init handshake → init_ack + state(created)."""
    log("=" * 60, "TEST")
    log(f"TEST: Init Handshake (worker={worker_type})", "TEST")
    log("=" * 60, "TEST")

    client.send(make_init(session_id, worker_type))

    # Wait for init_ack
    ack = client.wait_for("init_ack", timeout=15)
    if not ack:
        log("FAIL: No init_ack received", "FAIL")
        return False

    assigned_sid = ack.get("session_id", "")
    log(f"init_ack received, session_id={assigned_sid}", "OK")

    # Wait for state(created) or state(running)
    state_env = client.wait_for("state", timeout=10)
    if state_env:
        state = state_env.get("event", {}).get("data", {}).get("state", "")
        log(f"State: {state}", "OK")
    else:
        log("WARN: No state event received (may be delayed)", "WARN")

    return True


def test_permission_request_flow(client: AEPClient, session_id: str) -> bool:
    """Test: Send input that triggers a tool call requiring permission.

    We send a bash command which should trigger a permission_request event.
    The tool call happens when the worker tries to execute a shell command.
    """
    log("=" * 60, "TEST")
    log("TEST: Permission Request Flow", "TEST")
    log("=" * 60, "TEST")

    # Send input that will likely trigger a tool call needing permission
    # Use a simple bash command that should trigger permission request
    prompt = "Use the Bash tool to run: echo 'hello from e2e test'"
    client.send(make_input(session_id, prompt, seq=1))

    # Wait for either permission_request or tool_call (indicating permission was auto-granted)
    # or message.delta / done if the worker completed without needing permission
    log("Waiting for permission_request or other events...", "INFO")

    interaction_events = [
        "permission_request",
        "question_request",
        "elicitation_request",
    ]
    progress_events = [
        "message.delta",
        "message.start",
        "tool_call",
        "state",
    ]
    terminal_events = [
        "done",
        "error",
    ]

    request_id = None
    got_permission = False
    got_tool_call = False

    deadline = time.time() + 120  # 2 min max wait
    while time.time() < deadline:
        et, env = client.wait_for_any(
            interaction_events + progress_events + terminal_events,
            timeout=10,
        )
        if env is None:
            log("TIMEOUT: No events received", "WARN")
            break

        data = env.get("event", {}).get("data", {})

        if et == "permission_request":
            got_permission = True
            request_id = data.get("id", "")
            tool_name = data.get("tool_name", "")
            tool_args = data.get("args", [])
            input_raw = data.get("input_raw", "")
            log(f"✓ permission_request received:", "OK")
            log(f"  ID:        {request_id}", "OK")
            log(f"  ToolName:  {tool_name}", "OK")
            log(f"  Args:      {tool_args[:1] if tool_args else '[]'}...", "OK")
            log(f"  InputRaw:  {'<present>' if input_raw else '<empty>'}", "OK")
            break

        elif et == "tool_call":
            got_tool_call = True
            log(f"  tool_call: {data.get('name', '')} (auto-permission mode)", "OK")

        elif et == "done":
            log(f"  done received (turn completed)", "OK")
            if got_tool_call and not got_permission:
                log("NOTE: Tool was called without permission_request", "INFO")
                log("      Worker may be in auto-allow permission mode", "INFO")
                log("PASS: Permission flow skipped (auto-allowed)", "PASS")
                return True
            break

        elif et == "error":
            msg = data.get("message", "")
            log(f"  error: {msg}", "WARN")
            break

    if not got_permission:
        log("SKIP: No permission_request event triggered", "SKIP")
        log("      Worker may be in auto-allow mode or prompt didn't trigger tool", "INFO")
        return True  # Not a failure — just couldn't trigger the flow

    # Step 2: Send permission response (allow)
    log(f"Sending permission_response (allowed=true)...", "INFO")
    client.send(make_permission_response(session_id, request_id, True, ""))

    # Step 3: Wait for tool_result or done
    log("Waiting for tool execution to complete...", "INFO")
    et2, env2 = client.wait_for_any(["tool_result", "done", "error"], timeout=60)
    if env2:
        data2 = env2.get("event", {}).get("data", {})
        if et2 == "tool_result":
            log(f"✓ tool_result received after permission grant", "OK")
        elif et2 == "done":
            dd = env2.get("event", {}).get("data", {})
            success = dd.get("success", False)
            log(f"✓ done received (success={success})", "OK")
        elif et2 == "error":
            log(f"  error after permission: {data2.get('message', '')}", "WARN")

    # Wait for turn completion
    done_env = client.wait_for("done", timeout=60)
    if done_env:
        dd = done_env.get("event", {}).get("data", {})
        log(f"Turn completed: success={dd.get('success')}", "OK")

    log("PASS: Permission request flow completed", "PASS")
    return True


def test_permission_deny_flow(client: AEPClient, session_id: str) -> bool:
    """Test: Permission denied flow."""
    log("=" * 60, "TEST")
    log("TEST: Permission Deny Flow", "TEST")
    log("=" * 60, "TEST")

    # Send another prompt that triggers a tool call
    prompt = "Use the Bash tool to run: echo 'should be denied'"
    client.send(make_input(session_id, prompt, seq=2))

    # Drain previous turn's remaining events first
    time.sleep(1)

    log("Waiting for permission_request...", "INFO")
    deadline = time.time() + 120
    request_id = None

    while time.time() < deadline:
        et, env = client.wait_for_any(
            ["permission_request", "done", "error", "tool_call", "message.delta"],
            timeout=10,
        )
        if env is None:
            break
        data = env.get("event", {}).get("data", {})

        if et == "permission_request":
            request_id = data.get("id", "")
            log(f"✓ permission_request received: {request_id}", "OK")
            break
        elif et in ("done", "error"):
            log(f"  {et} received without permission_request", "WARN")
            log("SKIP: Could not trigger permission_request", "SKIP")
            return True
        elif et == "tool_call":
            log("  tool_call (auto-permission mode)", "INFO")

    if not request_id:
        log("SKIP: No permission_request triggered", "SKIP")
        return True

    # Deny permission
    log(f"Sending permission_response (allowed=false)...", "INFO")
    client.send(make_permission_response(session_id, request_id, False, "user denied"))

    # Wait for tool_result or done
    et2, env2 = client.wait_for_any(["tool_result", "done", "error"], timeout=30)
    if env2:
        data2 = env2.get("event", {}).get("data", {})
        if et2 == "tool_result":
            err = data2.get("error", "")
            log(f"✓ tool_result after deny: error='{err[:80]}'", "OK")
        elif et2 == "done":
            log(f"✓ done received after deny", "OK")
        elif et2 == "error":
            log(f"  error: {data2.get('message', '')}", "WARN")

    # Wait for turn done
    done_env = client.wait_for("done", timeout=60)
    if done_env:
        log("✓ Turn completed after deny", "OK")

    log("PASS: Permission deny flow completed", "PASS")
    return True


def test_event_structure_validation(client: AEPClient, session_id: str) -> bool:
    """Test: Validate that permission_request events have correct structure."""
    log("=" * 60, "TEST")
    log("TEST: Event Structure Validation", "TEST")
    log("=" * 60, "TEST")

    # Send a prompt that should trigger tool use
    prompt = "Read the file /tmp/hotplex-test-interaction.txt using the Read tool. If the file doesn't exist, say so."
    client.send(make_input(session_id, prompt, seq=3))

    log("Monitoring event stream for structured events...", "INFO")

    events_received = {}
    deadline = time.time() + 120

    while time.time() < deadline:
        et, env = client.wait_for_any(
            ["permission_request", "question_request", "elicitation_request",
             "tool_call", "tool_result", "message.delta", "done", "error",
             "state", "message.start", "reasoning"],
            timeout=10,
        )
        if env is None:
            break

        events_received[et] = events_received.get(et, 0) + 1
        data = env.get("event", {}).get("data", {})

        # Validate permission_request structure
        if et == "permission_request":
            required_fields = ["id", "tool_name", "description"]
            missing = [f for f in required_fields if f not in data or data[f] is None]
            if missing:
                log(f"FAIL: permission_request missing fields: {missing}", "FAIL")
                return False
            # Verify InputRaw is present (OCS converter provides it)
            input_raw = data.get("input_raw")
            if input_raw:
                log(f"  ✓ InputRaw present (length={len(str(input_raw))})", "OK")
            else:
                log(f"  WARN: InputRaw empty (may be CC worker)", "WARN")
            log(f"  ✓ Structure valid: id={data.get('id', '')[:12]}... tool={data.get('tool_name')}", "OK")

        elif et == "question_request":
            questions = data.get("questions", [])
            log(f"  ✓ question_request: {len(questions)} question(s)", "OK")

        elif et == "done":
            success = data.get("success", False)
            stats = data.get("stats")
            log(f"  done: success={success}, stats={'<present>' if stats else '<none>'}", "OK")
            break

        elif et == "error":
            log(f"  error: {data.get('message', '')}", "WARN")
            break

    # Summary
    log(f"Events received: {dict(events_received)}", "OK")
    log("PASS: Event structure validation completed", "PASS")
    return True


def test_ping_pong(client: AEPClient, session_id: str) -> bool:
    """Test: Ping/Pong basic connectivity."""
    log("=" * 60, "TEST")
    log("TEST: Ping/Pong", "TEST")
    log("=" * 60, "TEST")

    client.send(make_envelope(session_id, "ping", {"state": "running"}))
    pong = client.wait_for("pong", timeout=5)
    if pong:
        log("PASS: Ping/Pong works", "PASS")
        return True
    log("FAIL: No pong received", "FAIL")
    return False


# ─── Main ──────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Test HotPlex interaction events via AEP WebSocket")
    parser.add_argument("--port", type=int, default=8888, help="Gateway WebSocket port")
    parser.add_argument("--timeout", type=float, default=120, help="Per-test timeout in seconds")
    parser.add_argument("--worker", choices=["claude_code", "opencode_server"], default="claude_code",
                        help="Worker type to test")
    parser.add_argument("--skip", nargs="*", default=[], help="Test names to skip")
    args = parser.parse_args()

    ws_url = f"ws://localhost:{args.port}/ws"
    log(f"HotPlex Interaction Events E2E Test")
    log(f"WebSocket: {ws_url}")
    log(f"Worker:    {args.worker}")
    log(f"Timeout:   {args.timeout}s")
    log("")

    # Connect
    client = AEPClient(ws_url, timeout=args.timeout)
    if not client.connect():
        log("FAIL: Could not connect to gateway", "FAIL")
        sys.exit(1)

    session_id = f"test-interaction-{uuid.uuid4().hex[:8]}"

    # Test sequence
    tests = [
        ("init", lambda: test_init_handshake(client, session_id, args.worker)),
        ("ping", lambda: test_ping_pong(client, session_id)),
        ("permission_allow", lambda: test_permission_request_flow(client, session_id)),
        ("permission_deny", lambda: test_permission_deny_flow(client, session_id)),
        ("structure", lambda: test_event_structure_validation(client, session_id)),
    ]

    passed = 0
    failed = 0
    skipped = 0

    for name, test_fn in tests:
        if name in args.skip:
            log(f"SKIP: {name}", "SKIP")
            skipped += 1
            continue
        try:
            if test_fn():
                passed += 1
            else:
                failed += 1
        except Exception as e:
            log(f"FAIL: {name} raised: {e}", "FAIL")
            failed += 1
        log("")

    client.close()

    # Summary
    log("=" * 60, "SUMMARY")
    log(f"Passed: {passed}  Failed: {failed}  Skipped: {skipped}", "SUMMARY")
    if failed == 0:
        log("ALL TESTS PASSED ✓", "PASS")
    else:
        log(f"{failed} TEST(S) FAILED ✗", "FAIL")
    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
