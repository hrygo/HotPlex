#!/usr/bin/env python3
"""Test Claude CLI for thinking/</think> tags in output"""

import subprocess
import json
import os
import sys

def main():
    # Unset CLAUDECODE to allow nested sessions
    env = os.environ.copy()
    env.pop('CLAUDECODE', None)

    prompt = "分析这个仓库的 provider 目录结构，总结主要文件和功能"

    print("🚀 Starting Claude CLI test...")
    print(f"📝 Prompt: {prompt}")
    print("-" * 50)

    process = subprocess.Popen(
        ['claude', '-p', '--output-format=stream-json', prompt],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=env,
        text=True,
        bufsize=1
    )

    thinking_count = 0
    answer_count = 0
    thinking_in_answer = 0

    try:
        while True:
            line = process.stdout.readline()
            if not line:
                break

            line = line.strip()
            if not line:
                continue

            try:
                data = json.loads(line)
                event_type = data.get('type', '')

                if event_type == 'thinking':
                    thinking_count += 1
                    content = data.get('content', [])
                    if content:
                        text = content[0].get('text', '')[:100]
                        print(f"🔹 thinking event: {text}...")

                elif event_type == 'answer' or event_type == 'assistant':
                    answer_count += 1
                    # Check if answer contains thinking/</think>
                    content = data.get('content', [])
                    message = data.get('message', {})
                    msg_content = message.get('content', []) if message else []

                    # Check raw content for <think> tags
                    raw = json.dumps(data)
                    if '<think>' in raw or '</thinking>' in raw or '<think>' in str(content) or '<think>' in str(msg_content):
                        thinking_in_answer += 1
                        print(f"⚠️  Found thinking tag in {event_type}: {raw[:200]}...")

                    print(f"📩 {event_type} event received")

            except json.JSONDecodeError:
                continue

    except KeyboardInterrupt:
        process.terminate()

    process.wait()

    print("-" * 50)
    print(f"📊 Results:")
    print(f"   - thinking events: {thinking_count}")
    print(f"   - answer/assistant events: {answer_count}")
    print(f"   - thinking tags in answer: {thinking_in_answer}")

    if thinking_in_answer > 0:
        print("\n✅ RESULT: Claude CLI DOES return thinking tags in Answer messages!")
    else:
        print("\n❌ RESULT: Claude CLI does NOT return thinking tags in Answer messages")

    return thinking_in_answer > 0

if __name__ == "__main__":
    result = main()
    sys.exit(0 if result else 1)
