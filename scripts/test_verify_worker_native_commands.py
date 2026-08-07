#!/usr/bin/env python3
"""Unit tests for the native Worker command live verifier."""

from __future__ import annotations

import importlib.util
import json
import pathlib
import sys
import unittest


SCRIPT_PATH = pathlib.Path(__file__).with_name("verify_worker_native_commands.py")


def load_verifier():
    spec = importlib.util.spec_from_file_location(
        "verify_worker_native_commands", SCRIPT_PATH
    )
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load verifier module")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


class NativeCommandVerifierContractTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.verifier = load_verifier()

    def test_claude_frame_preserves_canonical_slash_command_and_arguments(self):
        frame = self.verifier.build_claude_user_frame("probe-skill", "nonce-123")

        self.assertEqual(
            {
                "type": "user",
                "message": {"role": "user", "content": "/probe-skill nonce-123"},
            },
            frame,
        )

    def test_opencode_body_requires_command_and_arguments(self):
        body = self.verifier.build_opencode_command_body("probe-skill", "nonce-123")

        self.assertEqual(
            {"command": "probe-skill", "arguments": "nonce-123"},
            body,
        )

    def test_http_body_decoder_preserves_non_json_health_response(self):
        self.assertEqual(
            "<!doctype html><title>OpenCode</title>",
            self.verifier.decode_http_body("<!doctype html><title>OpenCode</title>"),
        )
        self.assertEqual(
            {"healthy": True}, self.verifier.decode_http_body('{"healthy":true}')
        )

    def test_codex_turn_uses_the_authoritative_descriptor_path(self):
        response = {
            "data": [
                {
                    "cwd": "/tmp/work",
                    "skills": [
                        {
                            "name": "probe-skill",
                            "description": "probe",
                            "path": "/private/tmp/work/.agents/skills/probe-skill/SKILL.md",
                            "enabled": True,
                        },
                    ],
                },
            ],
        }
        descriptor = self.verifier.select_codex_skill(response, "probe-skill")
        turn_input = self.verifier.build_codex_turn_input(descriptor, "nonce-123")

        self.assertEqual(
            [
                {
                    "type": "skill",
                    "name": "probe-skill",
                    "path": "/private/tmp/work/.agents/skills/probe-skill/SKILL.md",
                },
                {"type": "text", "text": "$probe-skill nonce-123"},
            ],
            turn_input,
        )

    def test_acp_catalog_parser_accepts_only_available_commands_update(self):
        valid = {
            "method": "session/update",
            "params": {
                "update": {
                    "sessionUpdate": "available_commands_update",
                    "availableCommands": [
                        {"name": "context", "description": "Show usage"},
                        {"name": "version", "description": "Show version"},
                    ],
                },
            },
        }
        unrelated = {
            "method": "session/update",
            "params": {"update": {"sessionUpdate": "agent_message_chunk"}},
        }

        self.assertEqual(
            ["context", "version"], self.verifier.parse_acp_commands(valid)
        )
        self.assertEqual([], self.verifier.parse_acp_commands(unrelated))

    def test_report_evidence_redacts_secrets_paths_and_raw_model_text(self):
        raw = (
            "api_key=super-secret token=also-secret "
            "/Users/alice/private/project response=confidential model answer"
        )
        evidence = self.verifier.evidence_from_text(raw)
        encoded = json.dumps(evidence, sort_keys=True)

        self.assertNotIn("super-secret", encoded)
        self.assertNotIn("also-secret", encoded)
        self.assertNotIn("alice", encoded)
        self.assertNotIn("confidential model answer", encoded)
        self.assertEqual(len(raw.encode("utf-8")), evidence["byte_count"])
        self.assertRegex(evidence["sha256_12"], r"^[0-9a-f]{12}$")

    def test_exit_code_distinguishes_failures_from_environment_blocks(self):
        result = self.verifier.ProbeResult(worker="claude_code", status="PASS")
        blocked = self.verifier.ProbeResult(worker="opencode_server", status="BLOCKED")
        failed = self.verifier.ProbeResult(worker="codex_cli", status="FAIL")

        self.assertEqual(0, self.verifier.report_exit_code([result]))
        self.assertEqual(2, self.verifier.report_exit_code([result, blocked]))
        self.assertEqual(1, self.verifier.report_exit_code([result, failed]))

    def test_environment_block_recognizes_localized_authentication_error(self):
        self.assertTrue(self.verifier.is_environment_block("APIError 身份验证失败。"))

    def test_opencode_serve_env_isolates_host_provider_pollution(self):
        import tempfile

        with tempfile.TemporaryDirectory() as tmpdir:
            workdir = pathlib.Path(tmpdir) / "probe"
            workdir.mkdir()

            old_environ = dict(self.verifier.os.environ)
            try:
                self.verifier.os.environ.clear()
                self.verifier.os.environ.update(
                    {
                        "HOME": "/real/home",
                        "PATH": "/usr/bin",
                        "BIGMODEL_API_KEY": "sk-secret",
                        "AI_PROVIDER_NAME": "provider-auth-big",
                        "OPENCODE_PID": "1234",
                        "OPENCODE": "1",
                        "AGENT": "Sisyphus - ultraworker",
                        "XDG_CONFIG_HOME": "/real/xdg",
                        "KEEP_ME": "value",
                    }
                )
                env = self.verifier.opencode_serve_env(workdir)
            finally:
                self.verifier.os.environ.clear()
                self.verifier.os.environ.update(old_environ)

            self.assertEqual(str(workdir / ".probe-home"), env["HOME"])
            for key in (
                "BIGMODEL_API_KEY",
                "AI_PROVIDER_NAME",
                "OPENCODE_PID",
                "OPENCODE",
                "AGENT",
                "XDG_CONFIG_HOME",
            ):
                self.assertNotIn(key, env, f"{key} must be stripped")
            self.assertEqual("value", env["KEEP_ME"])

            fake_auth = (
                workdir
                / ".probe-home"
                / ".local"
                / "share"
                / "opencode"
                / "auth.json"
            )
            real_auth = (
                pathlib.Path("/real/home")
                / ".local"
                / "share"
                / "opencode"
                / "auth.json"
            )
            if real_auth.is_file():
                self.assertTrue(fake_auth.is_file(), "auth.json must be copied")


if __name__ == "__main__":
    unittest.main()
