---
name: hotplex-stt-tts
description: "Initialize or repair HotPlex local STT and MOSS TTS runtimes, including Python dependencies, official models, configuration, and verification. Use only for explicitly authorized host mutations; use hotplex-cli or hotplex-diagnostics for read-only checks."
---

# HotPlex STT/TTS

Use this Skill when HotPlex reports missing `funasr_onnx`, missing MOSS dependencies/models, or when a user explicitly asks to initialize or repair local voice runtimes.

This is an operator workflow. Installing packages, downloading models, changing `.env`/config, and restarting a service are mutations; perform them only when the user explicitly authorizes the requested scope. Do not expose credentials, full environment files, or private model/cache metadata.

For the detailed procedure, read [references/initialization.md](references/initialization.md) before changing the host.

## Scope

- STT: HotPlex `local` or `feishu+local` using `funasr-onnx` and SenseVoiceSmall.
- TTS: HotPlex `moss` or `edge+moss` using the MOSS-TTS-Nano ONNX sidecar.
- Verification: imports, model assets, MOSS warmup, `hotplex doctor`, gateway health, and configured message-channel connectivity.

Do not use this Skill for voice feature code changes, arbitrary Python environment cleanup, generic model selection, or read-only diagnosis without an initialization/repair request.

## Operating rules

1. Inspect the installed CLI help and current configuration before mutation. Preserve existing providers, credentials, user files, and unrelated environment variables.
2. Prefer a dedicated venv under the active HotPlex home. HotPlex checkers and the MOSS process resolve `python3` through `PATH`; verify that the same interpreter sees every installed package. Do not assume a symlink to a venv interpreter preserves venv detection.
3. Download models from the official ModelScope repositories listed in the reference. Use the official MOSS source repository for sidecar scripts; do not use piped shell installers, unreviewed mirrors, or arbitrary model files.
4. Make the smallest configuration change necessary. Validate configuration before restarting. Use the atomic `hotplex service restart` command for an installed service; do not split it into manual stop/start operations.
5. A successful install is not enough: run the verification checklist and report pass, warning, failure, and any intentionally untouched state separately.

## Completion criteria

The task is complete only when all requested runtime checks pass, or when a documented external blocker remains. At minimum, verify:

- `funasr_onnx`, `onnxruntime`, and `onnx` import from the interpreter HotPlex will call.
- SenseVoiceSmall assets are present or the configured cache can resolve them.
- MOSS TTS and codec ONNX assets are complete, and `/api/warmup-status` reaches `ready` in a localhost smoke test.
- `hotplex doctor --json` has no STT/TTS/AgentConfig failures relevant to the request.
- The gateway and requested message channel remain healthy after any restart.
