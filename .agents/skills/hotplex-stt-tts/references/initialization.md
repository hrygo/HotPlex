# HotPlex 本地 STT/TTS 初始化流程

本参考只在执行初始化或修复时读取。命令中的 `$HOTPLEX_HOME` 表示当前 HotPlex 实例根目录；如果环境未设置，通常为 `~/.hotplex`。不要把本机绝对路径、令牌或完整 `.env` 内容写入仓库或回复。

## 1. 预检与边界

先确认 CLI 实际支持的参数，再进行写操作：

```bash
hotplex doctor --help
hotplex config validate
hotplex doctor --json
```

记录以下非敏感事实：当前 HotPlex home/config 路径、OS/架构、Python 版本、磁盘余量、STT/TTS provider、模型目录、服务是否已安装。不要打印 `APP_SECRET`、Bot token、Admin token 或完整环境文件。

若用户只要求诊断，停在这里并转交 `hotplex-cli`/`hotplex-diagnostics`；若用户已授权初始化，继续执行缺失项。

## 2. Python 运行时

STT 可使用 Python 3.9+；MOSS 当前官方运行时建议 Python 3.10+。两个运行时优先共用一个专用 venv，避免把依赖装入系统 Python：

```bash
PYTHON_BIN="$(command -v python3.12 || command -v python3.11 || command -v python3)"
"$PYTHON_BIN" -m venv "$HOTPLEX_HOME/venv"
"$HOTPLEX_HOME/venv/bin/python" -m pip install --upgrade pip
```

安装后必须用 HotPlex 实际解析到的 `python3` 验证，而不是只验证 venv 的绝对路径：

```bash
command -v python3
python3 -c 'import sys; print(sys.executable); print(sys.version)'
```

如果服务的 `PATH` 只能解析到系统 Python，把 `$HOTPLEX_HOME/venv/bin` 放到服务环境的 PATH 前部，或创建一个明确可审计的可执行 wrapper 来 `exec` venv Python。不要只建立指向 venv Python 的软链接：某些 Python 构建通过调用路径判断 venv，软链接可能导致 `site-packages` 消失。

## 3. STT：funasr_onnx + SenseVoiceSmall

在专用 venv 安装 HotPlex checker 和 STT server 所需依赖：

```bash
"$HOTPLEX_HOME/venv/bin/python" -m pip install \
  funasr-onnx onnxruntime onnx modelscope
```

预下载官方 SenseVoiceSmall 模型到 ModelScope 缓存，避免首次语音请求才联网：

```bash
"$HOTPLEX_HOME/venv/bin/python" - <<'PY'
from modelscope import snapshot_download
snapshot_download("iic/SenseVoiceSmall")
PY
```

验证依赖：

```bash
python3 - <<'PY'
import funasr_onnx
import onnxruntime
import onnx
print("stt imports ok")
PY
```

只有在用户要求启用本地 STT 或当前配置明确缺失时，才设置 `HOTPLEX_MESSAGING_STT_PROVIDER=local` 或 `feishu+local`，以及对应的 `HOTPLEX_MESSAGING_STT_LOCAL_CMD`。如果现有 provider 是云端 `feishu`，不要擅自改成 local。

## 4. TTS：MOSS-TTS-Nano ONNX

MOSS 的官方参考实现和 ONNX 资产来自：

- 源码：[OpenMOSS/MOSS-TTS-Nano](https://github.com/OpenMOSS/MOSS-TTS-Nano)
- TTS 模型：`openmoss/MOSS-TTS-Nano-100M-ONNX`
- Codec 模型：`openmoss/MOSS-Audio-Tokenizer-Nano-ONNX`

模型目录建议保持以下结构：

```text
$HOTPLEX_HOME/models/moss-tts-nano/
├── app_onnx.py
├── onnx_tts_runtime.py
├── moss_tts_nano_runtime.py
├── MOSS-TTS-Nano-100M-ONNX/
│   └── browser_poc_manifest.json
└── MOSS-Audio-Tokenizer-Nano-ONNX/
    └── codec_browser_onnx_meta.json
```

从官方仓库取得 sidecar 所需脚本，从 ModelScope 下载两个 ONNX 制品：

```bash
tmp_dir="$(mktemp -d)"
git clone --depth 1 https://github.com/OpenMOSS/MOSS-TTS-Nano "$tmp_dir/moss-tts-nano"
mkdir -p "$HOTPLEX_HOME/models/moss-tts-nano"

# 复制前先核对脚本来源和内容；只复制运行时需要的官方脚本。
cp "$tmp_dir/moss-tts-nano/app_onnx.py" \
   "$tmp_dir/moss-tts-nano/onnx_tts_runtime.py" \
   "$tmp_dir/moss-tts-nano/ort_cpu_runtime.py" \
   "$tmp_dir/moss-tts-nano/text_normalization_pipeline.py" \
   "$tmp_dir/moss-tts-nano/tts_robust_normalizer_single_script.py" \
   "$tmp_dir/moss-tts-nano/moss_tts_nano_runtime.py" \
   "$HOTPLEX_HOME/models/moss-tts-nano/"
cp -R "$tmp_dir/moss-tts-nano/moss_tts_nano" \
   "$HOTPLEX_HOME/models/moss-tts-nano/"

"$HOTPLEX_HOME/venv/bin/modelscope" download \
  openmoss/MOSS-TTS-Nano-100M-ONNX \
  --local-dir "$HOTPLEX_HOME/models/moss-tts-nano/MOSS-TTS-Nano-100M-ONNX" \
  --include '*.onnx' '*.data' '*.json' 'tokenizer.model'

"$HOTPLEX_HOME/venv/bin/modelscope" download \
  openmoss/MOSS-Audio-Tokenizer-Nano-ONNX \
  --local-dir "$HOTPLEX_HOME/models/moss-tts-nano/MOSS-Audio-Tokenizer-Nano-ONNX" \
  --include '*.onnx' '*.data' '*.json'
```

官方当前 Python 运行时还需要 numpy、sentencepiece、ONNX Runtime、FastAPI/Uvicorn、音频和文本规范化依赖；以官方仓库当前依赖为准安装。macOS 若 `pynini`/`WeTextProcessing` 构建失败，先安装对应 OpenFst 开发文件，再重试；不要通过删掉文本规范化依赖来掩盖失败。

```bash
"$HOTPLEX_HOME/venv/bin/python" -m pip install \
  numpy sentencepiece onnxruntime fastapi uvicorn \
  python-multipart soundfile huggingface_hub \
  torch torchaudio transformers
```

验证 MOSS import：

```bash
python3 - <<'PY'
import fastapi, onnxruntime, sentencepiece, torch, torchaudio, transformers
import tn
print("moss imports ok")
PY
```

只有在用户要求启用 MOSS 或当前 provider 已是 `moss`/`edge+moss` 时，才补齐：

```text
HOTPLEX_MESSAGING_TTS_PROVIDER=moss        # 或 edge+moss
HOTPLEX_MESSAGING_TTS_MOSS_MODEL_DIR=~/.hotplex/models/moss-tts-nano
```

保留现有 Edge TTS 配置和音色；`edge+moss` 是优先 Edge、MOSS 兜底，不应被初始化流程擅自改成纯 MOSS。

## 5. 运行时冒烟与服务

先做不改变持久服务状态的 MOSS warmup。使用专用 venv 的绝对解释器启动 localhost sidecar，轮询直到 `ready=true` 或 `failed=true`，测试完通过 Ctrl-C 正常退出：

```bash
"$HOTPLEX_HOME/venv/bin/python" \
  "$HOTPLEX_HOME/models/moss-tts-nano/app_onnx.py" \
  --host 127.0.0.1 --port 18083 \
  --model-dir "$HOTPLEX_HOME/models/moss-tts-nano"

curl -fsS http://127.0.0.1:18083/api/warmup-status
```

若配置或服务环境发生变化：

```bash
hotplex config validate
hotplex service restart --level user
hotplex status --format json
```

服务未安装时，按用户授权范围执行 `hotplex service install --level user`，随后用 `hotplex service status --level user --json` 验证；不要用手工 `stop && sleep && start` 替代原子 restart。

## 6. 最终验收和失败处理

```bash
hotplex doctor --json
hotplex service status --level user --json
hotplex status --format json
```

必须单独核对 `stt.runtime`、`tts.runtime`、`agent.directory_structure`、`agent.global_files` 和消息渠道状态。`doctor` 的其他告警（例如 worker 权限模式）要原样报告，不要为了让总数变绿而越权修改。

以下情况停止并报告，不要自动降级或删除已有环境：模型源不可达、模型清单/哈希不完整、Python ABI 不兼容、磁盘不足、现有 provider 与用户意图冲突、服务端口被未知进程占用，或需要新的凭据/权限。
