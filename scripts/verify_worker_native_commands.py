#!/usr/bin/env python3
"""Run bounded Live probes for native command/Skill dispatch in four Workers.

The verifier intentionally invokes real installed Worker CLIs. It creates a
temporary workspace and a deterministic Skill, records only redacted evidence,
and always terminates processes it started. Exit codes:

  0  every selected Worker passed
  1  at least one selected Worker failed
  2  no failures, but at least one Worker was blocked by its environment
"""

from __future__ import annotations

import argparse
import dataclasses
import datetime as dt
import hashlib
import json
import os
import pathlib
import queue
import re
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Callable


PROBE_SKILL = "hotplex-native-probe"
PASS = "PASS"
FAIL = "FAIL"
BLOCKED = "BLOCKED"


@dataclasses.dataclass
class ProbeResult:
    worker: str
    status: str
    version: str = "unknown"
    transport: str = ""
    command: str = ""
    duration_ms: int = 0
    facts: dict[str, Any] = dataclasses.field(default_factory=dict)
    evidence: dict[str, Any] = dataclasses.field(default_factory=dict)
    error: str = ""

    def as_dict(self) -> dict[str, Any]:
        data = dataclasses.asdict(self)
        if not data["error"]:
            data.pop("error")
        if not data["evidence"]:
            data.pop("evidence")
        return data


def build_claude_user_frame(name: str, args: str) -> dict[str, Any]:
    content = "/" + name.strip()
    if args.strip():
        content += " " + args.strip()
    return {"type": "user", "message": {"role": "user", "content": content}}


def build_opencode_command_body(name: str, args: str) -> dict[str, str]:
    return {"command": name.strip(), "arguments": args.strip()}


def select_codex_skill(response: dict[str, Any], name: str) -> dict[str, Any]:
    for entry in response.get("data", []):
        for descriptor in entry.get("skills", []):
            if descriptor.get("name") == name and descriptor.get("enabled", True):
                path = descriptor.get("path")
                if isinstance(path, str) and path:
                    return descriptor
    raise LookupError(f"Codex skills/list did not advertise {name!r}")


def build_codex_turn_input(
    descriptor: dict[str, Any], args: str
) -> list[dict[str, str]]:
    name = str(descriptor["name"])
    text = "$" + name
    if args.strip():
        text += " " + args.strip()
    return [
        {"type": "skill", "name": name, "path": str(descriptor["path"])},
        {"type": "text", "text": text},
    ]


def parse_acp_commands(message: dict[str, Any]) -> list[str]:
    if message.get("method") != "session/update":
        return []
    update = message.get("params", {}).get("update", {})
    if update.get("sessionUpdate") != "available_commands_update":
        return []
    commands: list[str] = []
    for item in update.get("availableCommands", []):
        name = item.get("name")
        if isinstance(name, str) and name.strip():
            commands.append(name.strip())
    return commands


def evidence_from_text(text: str) -> dict[str, Any]:
    data = text.encode("utf-8", errors="replace")
    return {
        "byte_count": len(data),
        "sha256_12": hashlib.sha256(data).hexdigest()[:12],
    }


def report_exit_code(results: list[ProbeResult]) -> int:
    if any(result.status == FAIL for result in results):
        return 1
    if any(result.status == BLOCKED for result in results):
        return 2
    return 0


def log(message: str) -> None:
    print(f"[worker-live-probe] {message}", file=sys.stderr, flush=True)


def safe_error(value: Any) -> str:
    text = str(value).replace("\n", " ").replace("\r", " ")
    text = re.sub(
        r"(?i)(api[_-]?key|authorization|token|secret)\s*[:=]\s*\S+",
        r"\1=<redacted>",
        text,
    )
    text = re.sub(r"/(?:Users|home)/[^\s'\"]+", "<local-path>", text)
    return text[:240]


def command_version(argv: list[str]) -> str:
    try:
        result = subprocess.run(
            argv, text=True, capture_output=True, timeout=10, check=False
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return safe_error(exc)
    for stream in (result.stdout, result.stderr):
        line = stream.strip().splitlines()
        if line:
            return safe_error(line[0])
    return "unknown"


def is_environment_block(text: str) -> bool:
    lower = text.lower()
    patterns = (
        "401",
        "403",
        "authentication",
        "not authenticated",
        "not logged in",
        "api key",
        "api_key",
        "billing",
        "credit balance",
        "model not found",
        "unsupported model",
        "provider not found",
        "身份验证失败",
        "未登录",
    )
    return any(pattern in lower for pattern in patterns)


def create_probe_workspace(root: pathlib.Path, marker: str) -> None:
    body = (
        "---\n"
        f"name: {PROBE_SKILL}\n"
        "description: HotPlex native Worker command Live probe\n"
        "---\n\n"
        f"Reply with exactly `{marker}` and nothing else. "
        "Do not call tools, read files, modify files, or access the network.\n"
    )
    for base in (root / ".agents" / "skills", root / ".claude" / "skills"):
        skill_dir = base / PROBE_SKILL
        skill_dir.mkdir(parents=True, exist_ok=True)
        (skill_dir / "SKILL.md").write_text(body, encoding="utf-8")


def extract_claude_response(stdout: str) -> str:
    texts: list[str] = []
    for line in stdout.splitlines():
        try:
            message = json.loads(line)
        except json.JSONDecodeError:
            continue
        if message.get("type") == "assistant":
            for item in message.get("message", {}).get("content", []):
                if item.get("type") == "text" and isinstance(item.get("text"), str):
                    texts.append(item["text"])
        elif message.get("type") == "result" and isinstance(message.get("result"), str):
            texts.append(message["result"])
    return "\n".join(texts)


def probe_claude(timeout: float, budget_usd: float) -> ProbeResult:
    started = time.monotonic()
    binary = shutil.which("claude")
    if not binary:
        return ProbeResult(
            worker="claude_code", status=BLOCKED, error="claude executable not found"
        )
    version = command_version([binary, "--version"])
    nonce = secrets.token_hex(6)
    marker = f"HOTPLEX_NATIVE_PROBE_OK_{nonce}"
    with tempfile.TemporaryDirectory(prefix="hotplex-claude-live-") as tmp:
        workdir = pathlib.Path(tmp)
        create_probe_workspace(workdir, marker)
        argv = [
            binary,
            "--print",
            "--verbose",
            "--no-session-persistence",
            "--output-format",
            "stream-json",
            "--input-format",
            "stream-json",
            "--tools",
            "",
            "--max-turns",
            "1",
            "--max-budget-usd",
            str(budget_usd),
        ]
        frame = build_claude_user_frame(PROBE_SKILL, nonce)
        try:
            completed = subprocess.run(
                argv,
                cwd=workdir,
                input=json.dumps(frame) + "\n",
                text=True,
                capture_output=True,
                timeout=timeout,
                check=False,
            )
        except subprocess.TimeoutExpired as exc:
            return ProbeResult(
                worker="claude_code",
                status=FAIL,
                version=version,
                transport="stream-json text_command",
                command=f"/{PROBE_SKILL}",
                duration_ms=int((time.monotonic() - started) * 1000),
                error=f"Live command timed out after {exc.timeout}s",
            )
    response = extract_claude_response(completed.stdout)
    combined = response + "\n" + completed.stderr
    status = (
        PASS
        if marker in response
        else (BLOCKED if is_environment_block(combined) else FAIL)
    )
    return ProbeResult(
        worker="claude_code",
        status=status,
        version=version,
        transport="stream-json text_command",
        command=f"/{PROBE_SKILL}",
        duration_ms=int((time.monotonic() - started) * 1000),
        facts={
            "canonical_slash_syntax": True,
            "model_marker_observed": marker in response,
            "process_exit_code": completed.returncode,
        },
        evidence=evidence_from_text(response),
        error="" if status == PASS else safe_error(combined),
    )


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def decode_http_body(raw: str) -> Any:
    if not raw.strip():
        return None
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return raw


def http_json(
    method: str,
    url: str,
    body: dict[str, Any] | None = None,
    timeout: float = 10,
) -> tuple[int, Any]:
    data = json.dumps(body).encode("utf-8") if body is not None else None
    request = urllib.request.Request(url, data=data, method=method)
    request.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw = response.read().decode("utf-8", errors="replace")
            return response.status, decode_http_body(raw)
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", errors="replace")
        return exc.code, decode_http_body(raw)


def wait_for_http(url: str, timeout: float) -> bool:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            status, _ = http_json("GET", url, timeout=1)
            if status == 200:
                return True
        except (OSError, urllib.error.URLError, TimeoutError):
            pass
        time.sleep(0.1)
    return False


def terminate_process(process: subprocess.Popen[str]) -> None:
    if process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=3)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=3)


# Injected host-session provider credentials (e.g. BIGMODEL_API_KEY registered
# as provider-auth-big by an embedding opencode instance). `serve` inherits
# them and may pick that provider over auth.json, failing 401 while the user's
# CLI works. Stripped so the probe measures the user's real environment.
_OPENCODE_HOST_POLLUTION_ENV = {
    "BIGMODEL_API_KEY",
    "AI_PROVIDER_NAME",
    "OPENCODE_PID",
    "OPENCODE",
    "AGENT",
    "SISYPHUS",
}


def opencode_serve_env(workdir: pathlib.Path) -> dict[str, str]:
    """Clean env for `opencode serve`.

    serve resolves the default agent/model from the embedding opencode session
    and inherits injected provider credentials, failing 401 even when the
    user's CLI works. Isolate: fresh HOME with only the user's auth.json, no
    injected provider variables, no XDG overrides.
    """
    fake_home = workdir / ".probe-home"
    fake_data = fake_home / ".local" / "share" / "opencode"
    fake_data.mkdir(parents=True, exist_ok=True)
    auth = pathlib.Path.home() / ".local" / "share" / "opencode" / "auth.json"
    if auth.is_file():
        shutil.copy2(auth, fake_data / "auth.json")

    env = {
        "HOME": str(fake_home),
        "PATH": os.environ.get("PATH", ""),
        "TMPDIR": os.environ.get("TMPDIR", "/tmp"),
        "TERM": os.environ.get("TERM", "dumb"),
        "LANG": os.environ.get("LANG", "C.UTF-8"),
        "USER": os.environ.get("USER", ""),
        "LOGNAME": os.environ.get("LOGNAME", ""),
    }
    for key in list(os.environ):
        if key in _OPENCODE_HOST_POLLUTION_ENV or key.startswith(
            ("OPENCODE_", "XDG_CONFIG_HOME", "XDG_DATA_HOME")
        ):
            continue
        if key not in env:
            env[key] = os.environ[key]
    return env


def opencode_catalog_names(catalog: Any) -> set[str]:
    items = catalog if isinstance(catalog, list) else []
    return {
        str(item.get("name") or item.get("command"))
        for item in items
        if isinstance(item, dict) and (item.get("name") or item.get("command"))
    }


def collect_text_values(value: Any) -> str:
    texts: list[str] = []
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        for item in value:
            text = collect_text_values(item)
            if text:
                texts.append(text)
    elif isinstance(value, dict):
        for key, item in value.items():
            if key in {"text", "content", "message", "error", "name"}:
                text = collect_text_values(item)
                if text:
                    texts.append(text)
            elif isinstance(item, (dict, list)):
                text = collect_text_values(item)
                if text:
                    texts.append(text)
    return "\n".join(texts)


def probe_opencode(timeout: float) -> ProbeResult:
    started = time.monotonic()
    binary = shutil.which("opencode")
    if not binary:
        return ProbeResult(
            worker="opencode_server",
            status=BLOCKED,
            error="opencode executable not found",
        )
    version = command_version([binary, "--version"])
    nonce = secrets.token_hex(6)
    marker = f"HOTPLEX_NATIVE_PROBE_OK_{nonce}"
    port = free_port()
    base_url = f"http://127.0.0.1:{port}"
    process: subprocess.Popen[str] | None = None
    session_id = ""
    with tempfile.TemporaryDirectory(prefix="hotplex-opencode-live-") as tmp:
        workdir = pathlib.Path(tmp)
        create_probe_workspace(workdir, marker)
        try:
            process = subprocess.Popen(
                [
                    binary,
                    "serve",
                    "--port",
                    str(port),
                    "--hostname",
                    "127.0.0.1",
                    "--pure",
                    "--log-level",
                    "ERROR",
                ],
                cwd=workdir,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.PIPE,
                text=True,
                env=opencode_serve_env(workdir),
            )
            if not wait_for_http(base_url + "/health", min(timeout, 15)):
                return ProbeResult(
                    worker="opencode_server",
                    status=BLOCKED,
                    version=version,
                    transport="HTTP POST /session/{id}/command",
                    error="opencode serve did not become healthy",
                )
            catalog_status, catalog = http_json(
                "GET", base_url + "/command", timeout=10
            )
            catalog_names = opencode_catalog_names(catalog)
            if catalog_status != 200 or PROBE_SKILL not in catalog_names:
                return ProbeResult(
                    worker="opencode_server",
                    status=FAIL,
                    version=version,
                    transport="HTTP POST /session/{id}/command",
                    facts={
                        "catalog_endpoint_status": catalog_status,
                        "probe_advertised": False,
                    },
                    error="OpenCode command catalog did not advertise the temporary Skill",
                )
            query = urllib.parse.urlencode({"directory": str(workdir)})
            create_status, created = http_json(
                "POST", base_url + "/session?" + query, {}, timeout=10
            )
            if (
                create_status not in {200, 201}
                or not isinstance(created, dict)
                or not created.get("id")
            ):
                return ProbeResult(
                    worker="opencode_server",
                    status=FAIL,
                    version=version,
                    transport="HTTP POST /session/{id}/command",
                    error=f"OpenCode session creation failed with HTTP {create_status}",
                )
            session_id = str(created["id"])
            invoke_status, invoked = http_json(
                "POST",
                base_url
                + "/session/"
                + urllib.parse.quote(session_id, safe="")
                + "/command",
                build_opencode_command_body(PROBE_SKILL, nonce),
                timeout=timeout,
            )
            response = collect_text_values(invoked)
            status = (
                PASS
                if invoke_status == 200 and marker in response
                else (BLOCKED if is_environment_block(response) else FAIL)
            )
            return ProbeResult(
                worker="opencode_server",
                status=status,
                version=version,
                transport="HTTP POST /session/{id}/command",
                command=PROBE_SKILL,
                duration_ms=int((time.monotonic() - started) * 1000),
                facts={
                    "catalog_endpoint_status": catalog_status,
                    "probe_advertised": True,
                    "invoke_http_status": invoke_status,
                    "model_marker_observed": marker in response,
                },
                evidence=evidence_from_text(response),
                error="" if status == PASS else safe_error(response),
            )
        except (OSError, urllib.error.URLError, TimeoutError) as exc:
            return ProbeResult(
                worker="opencode_server",
                status=BLOCKED if is_environment_block(safe_error(exc)) else FAIL,
                version=version,
                transport="HTTP POST /session/{id}/command",
                error=safe_error(exc),
            )
        finally:
            if session_id:
                try:
                    http_json(
                        "DELETE",
                        base_url
                        + "/session/"
                        + urllib.parse.quote(session_id, safe=""),
                        timeout=3,
                    )
                except Exception:
                    pass
            if process is not None:
                terminate_process(process)


class JsonRpcProcess:
    def __init__(
        self, argv: list[str], cwd: pathlib.Path, env: dict[str, str] | None = None
    ):
        self.process = subprocess.Popen(
            argv,
            cwd=cwd,
            env=env,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )
        self._next_id = 1
        self._queue: queue.Queue[dict[str, Any]] = queue.Queue()
        self._backlog: list[dict[str, Any]] = []
        self._stderr: list[str] = []
        threading.Thread(target=self._read_stdout, daemon=True).start()
        threading.Thread(target=self._read_stderr, daemon=True).start()

    def _read_stdout(self) -> None:
        assert self.process.stdout is not None
        for line in self.process.stdout:
            try:
                message = json.loads(line)
            except json.JSONDecodeError:
                continue
            if isinstance(message, dict):
                self._queue.put(message)

    def _read_stderr(self) -> None:
        assert self.process.stderr is not None
        for line in self.process.stderr:
            if len(self._stderr) < 100:
                self._stderr.append(line)

    def send(self, message: dict[str, Any]) -> None:
        if self.process.stdin is None:
            raise RuntimeError("JSON-RPC stdin unavailable")
        self.process.stdin.write(json.dumps(message, separators=(",", ":")) + "\n")
        self.process.stdin.flush()

    def notify(self, method: str, params: dict[str, Any] | None = None) -> None:
        message: dict[str, Any] = {"jsonrpc": "2.0", "method": method}
        if params is not None:
            message["params"] = params
        self.send(message)

    def request(
        self, method: str, params: dict[str, Any], timeout: float
    ) -> dict[str, Any]:
        request_id = self._next_id
        self._next_id += 1
        self.send(
            {"jsonrpc": "2.0", "id": request_id, "method": method, "params": params}
        )
        return self.wait_for(lambda msg: msg.get("id") == request_id, timeout)

    def wait_for(
        self, predicate: Callable[[dict[str, Any]], bool], timeout: float
    ) -> dict[str, Any]:
        for index, message in enumerate(self._backlog):
            if predicate(message):
                return self._backlog.pop(index)
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            remaining = max(0.01, deadline - time.monotonic())
            try:
                message = self._queue.get(timeout=remaining)
            except queue.Empty as exc:
                raise TimeoutError("JSON-RPC response timed out") from exc
            if predicate(message):
                return message
            self._backlog.append(message)
        raise TimeoutError("JSON-RPC response timed out")

    def backlog(self) -> list[dict[str, Any]]:
        while True:
            try:
                self._backlog.append(self._queue.get_nowait())
            except queue.Empty:
                break
        return list(self._backlog)

    def stderr_text(self) -> str:
        return "".join(self._stderr)

    def close(self) -> None:
        if self.process.stdin is not None:
            try:
                self.process.stdin.close()
            except OSError:
                pass
        terminate_process(self.process)


def rpc_result(message: dict[str, Any], method: str) -> dict[str, Any]:
    if message.get("error"):
        raise RuntimeError(f"{method}: {safe_error(message['error'])}")
    result = message.get("result")
    if not isinstance(result, dict):
        raise RuntimeError(f"{method}: invalid result")
    return result


def codex_agent_text(message: dict[str, Any]) -> str:
    if message.get("method") != "item/completed":
        return ""
    item = message.get("params", {}).get("item", {})
    item_type = str(item.get("type", "")).lower()
    if item_type not in {"agentmessage", "agent_message"}:
        return ""
    return collect_text_values(item)


def probe_codex(timeout: float, model: str) -> ProbeResult:
    started = time.monotonic()
    binary = shutil.which("codex")
    if not binary:
        return ProbeResult(
            worker="codex_cli", status=BLOCKED, error="codex executable not found"
        )
    version = command_version([binary, "--version"])
    nonce = secrets.token_hex(6)
    marker = f"HOTPLEX_NATIVE_PROBE_OK_{nonce}"
    rpc: JsonRpcProcess | None = None
    with tempfile.TemporaryDirectory(prefix="hotplex-codex-live-") as tmp:
        workdir = pathlib.Path(tmp)
        create_probe_workspace(workdir, marker)
        try:
            rpc = JsonRpcProcess([binary, "app-server", "--stdio"], workdir)
            initialize = rpc.request(
                "initialize",
                {
                    "clientInfo": {
                        "name": "hotplex-worker-probe",
                        "title": "HotPlex Worker Probe",
                        "version": "1.0.0",
                    },
                    "capabilities": {"experimentalApi": True},
                },
                min(timeout, 30),
            )
            rpc_result(initialize, "initialize")
            rpc.notify("initialized", {})
            catalog_message = rpc.request(
                "skills/list", {"cwds": [str(workdir)]}, min(timeout, 30)
            )
            catalog = rpc_result(catalog_message, "skills/list")
            descriptor = select_codex_skill(catalog, PROBE_SKILL)
            thread_params: dict[str, Any] = {
                "cwd": str(workdir),
                "sandbox": "read-only",
                "approvalPolicy": "never",
                "ephemeral": True,
            }
            if model:
                thread_params["model"] = model
            thread_message = rpc.request(
                "thread/start", thread_params, min(timeout, 30)
            )
            thread = rpc_result(thread_message, "thread/start").get("thread", {})
            thread_id = thread.get("id")
            if not thread_id:
                raise RuntimeError("thread/start returned no thread id")
            turn_input = build_codex_turn_input(descriptor, nonce)
            turn_message = rpc.request(
                "turn/start",
                {"threadId": thread_id, "input": turn_input},
                min(timeout, 30),
            )
            rpc_result(turn_message, "turn/start")
            deadline = time.monotonic() + timeout
            response_parts: list[str] = []
            terminal: dict[str, Any] | None = None
            while time.monotonic() < deadline:
                message = rpc.wait_for(
                    lambda msg: (
                        msg.get("method")
                        in {"item/completed", "turn/completed", "error"}
                    ),
                    max(0.1, deadline - time.monotonic()),
                )
                text = codex_agent_text(message)
                if text:
                    response_parts.append(text)
                if marker in "\n".join(response_parts):
                    break
                if message.get("method") in {"turn/completed", "error"}:
                    terminal = message
                    break
            response = "\n".join(response_parts)
            terminal_text = collect_text_values(terminal or {})
            status = (
                PASS
                if marker in response
                else (
                    BLOCKED
                    if is_environment_block(
                        response + "\n" + terminal_text + "\n" + rpc.stderr_text()
                    )
                    else FAIL
                )
            )
            return ProbeResult(
                worker="codex_cli",
                status=status,
                version=version,
                transport="app-server skills/list + structured turn/start",
                command="$" + PROBE_SKILL,
                duration_ms=int((time.monotonic() - started) * 1000),
                facts={
                    "catalog_advertised": True,
                    "authoritative_descriptor_path_used": turn_input[0]["path"]
                    == descriptor["path"],
                    "model_marker_observed": marker in response,
                },
                evidence=evidence_from_text(response),
                error=""
                if status == PASS
                else safe_error(terminal_text or rpc.stderr_text()),
            )
        except (OSError, RuntimeError, TimeoutError, LookupError) as exc:
            combined = safe_error(exc) + (
                " " + safe_error(rpc.stderr_text()) if rpc else ""
            )
            return ProbeResult(
                worker="codex_cli",
                status=BLOCKED if is_environment_block(combined) else FAIL,
                version=version,
                transport="app-server skills/list + structured turn/start",
                command="$" + PROBE_SKILL,
                duration_ms=int((time.monotonic() - started) * 1000),
                error=combined,
            )
        finally:
            if rpc is not None:
                rpc.close()


def acp_binary() -> list[str] | None:
    direct = shutil.which("hermes-acp")
    if direct:
        return [direct, "--accept-hooks"]
    hermes = shutil.which("hermes")
    if hermes:
        return [hermes, "acp", "--accept-hooks"]
    return None


def probe_acp(timeout: float) -> ProbeResult:
    started = time.monotonic()
    argv = acp_binary()
    if argv is None:
        return ProbeResult(
            worker="acp", status=BLOCKED, error="Hermes ACP executable not found"
        )
    version_argv = (
        [argv[0], "--version"]
        if pathlib.Path(argv[0]).name == "hermes-acp"
        else [argv[0], "--version"]
    )
    version = command_version(version_argv)
    rpc: JsonRpcProcess | None = None
    with tempfile.TemporaryDirectory(prefix="hotplex-acp-live-") as tmp:
        workdir = pathlib.Path(tmp)
        marker = f"HOTPLEX_NATIVE_PROBE_OK_{secrets.token_hex(6)}"
        create_probe_workspace(workdir, marker)
        try:
            rpc = JsonRpcProcess(argv, workdir)
            initialize = rpc.request(
                "initialize",
                {
                    "protocolVersion": 1,
                    "clientCapabilities": {},
                    "clientInfo": {"name": "hotplex-worker-probe", "version": "1.0.0"},
                },
                min(timeout, 30),
            )
            rpc_result(initialize, "initialize")
            session_message = rpc.request(
                "session/new",
                {"cwd": str(workdir), "mcpServers": []},
                min(timeout, 30),
            )
            session = rpc_result(session_message, "session/new")
            session_id = session.get("sessionId")
            if not session_id:
                raise RuntimeError("session/new returned no sessionId")
            catalog_message = rpc.wait_for(
                lambda msg: bool(parse_acp_commands(msg)), min(timeout, 30)
            )
            commands = parse_acp_commands(catalog_message)
            command = (
                "context"
                if "context" in commands
                else ("version" if "version" in commands else "")
            )
            if not command:
                raise RuntimeError("ACP Agent advertised neither context nor version")
            prompt_message = rpc.request(
                "session/prompt",
                {
                    "sessionId": session_id,
                    "prompt": [{"type": "text", "text": "/" + command}],
                },
                timeout,
            )
            prompt_result = rpc_result(prompt_message, "session/prompt")
            updates = rpc.backlog()
            response = "\n".join(
                collect_text_values(msg)
                for msg in updates
                if msg.get("method") == "session/update"
            )
            stop_reason = prompt_result.get("stopReason")
            passed = (
                stop_reason == "end_turn"
                and command in commands
                and PROBE_SKILL not in commands
            )
            return ProbeResult(
                worker="acp",
                status=PASS if passed else FAIL,
                version=version,
                transport="ACP available_commands_update + session/prompt",
                command="/" + command,
                duration_ms=int((time.monotonic() - started) * 1000),
                facts={
                    "advertised_command_count": len(commands),
                    "selected_command_advertised": command in commands,
                    "filesystem_probe_skill_advertised": PROBE_SKILL in commands,
                    "stop_reason": stop_reason,
                },
                evidence=evidence_from_text(response),
                error=""
                if passed
                else "ACP advertised-command invocation did not finish with end_turn",
            )
        except (OSError, RuntimeError, TimeoutError) as exc:
            combined = safe_error(exc) + (
                " " + safe_error(rpc.stderr_text()) if rpc else ""
            )
            return ProbeResult(
                worker="acp",
                status=BLOCKED if is_environment_block(combined) else FAIL,
                version=version,
                transport="ACP available_commands_update + session/prompt",
                duration_ms=int((time.monotonic() - started) * 1000),
                error=combined,
            )
        finally:
            if rpc is not None:
                rpc.close()


PROBES: dict[str, Callable[..., ProbeResult]] = {
    "claude_code": probe_claude,
    "opencode_server": probe_opencode,
    "codex_cli": probe_codex,
    "acp": probe_acp,
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run direct Live native-command probes against installed HotPlex Workers"
    )
    parser.add_argument(
        "--worker",
        action="append",
        choices=sorted(PROBES),
        help="Worker to probe; repeat for multiple Workers (default: all)",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=90.0,
        help="Per Live invocation timeout in seconds",
    )
    parser.add_argument(
        "--claude-budget-usd",
        type=float,
        default=0.02,
        help="Maximum Claude probe budget",
    )
    parser.add_argument(
        "--codex-model", default="", help="Optional Codex model override"
    )
    parser.add_argument(
        "--json-out",
        type=pathlib.Path,
        help="Optional path for the sanitized JSON report",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    selected = args.worker or ["claude_code", "opencode_server", "codex_cli", "acp"]
    results: list[ProbeResult] = []
    log(
        "direct Live mode enabled; installed Worker credentials and model quotas may be used"
    )
    for name in selected:
        log(f"starting {name}")
        if name == "claude_code":
            result = probe_claude(args.timeout, args.claude_budget_usd)
        elif name == "opencode_server":
            result = probe_opencode(args.timeout)
        elif name == "codex_cli":
            result = probe_codex(args.timeout, args.codex_model)
        else:
            result = probe_acp(args.timeout)
        results.append(result)
        log(f"{name}: {result.status}")

    report = {
        "schema_version": 1,
        "verified_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "mode": "direct-live",
        "summary": {
            "pass": sum(result.status == PASS for result in results),
            "fail": sum(result.status == FAIL for result in results),
            "blocked": sum(result.status == BLOCKED for result in results),
        },
        "workers": [result.as_dict() for result in results],
    }
    encoded = json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    if args.json_out:
        args.json_out.parent.mkdir(parents=True, exist_ok=True)
        args.json_out.write_text(encoded, encoding="utf-8")
    print(encoded, end="")
    return report_exit_code(results)


if __name__ == "__main__":
    raise SystemExit(main())
