#!/usr/bin/env python3
"""Test Claude CLI for thinking/</think> tags - simplified version"""

import asyncio
import json
import sys

# Check if websockets is available
try:
    import websockets
except ImportError:
    print("Installing websockets...")
    import subprocess
    subprocess.check_call([sys.executable, "-m", "pip", "install", "websockets", "-q"])
    import websockets

async def test():
    uri = "ws://localhost:8080/ws/v1/agent?api_key=2fead6aca5ae310628a1586fda5821f6f0b1086ed6c63acc5ee4eadc1f50cb2a"

    # Use a prompt that requires thinking
    prompt = "请解释一下快速排序算法的工作原理，不需要使用工具"

    print(f"Connecting to {uri}")
    print(f"Prompt: {prompt}")
    print("-" * 60)

    thinking_events = 0
    answer_events = 0
    thinking_in_answer = False

    try:
        async with websockets.connect(uri, ping_timeout=30) as ws:
            # Send start request
            req = {
                "type": "execute",
                "prompt": prompt,
            }
            await ws.send(json.dumps(req))
            print("Request sent, waiting for events...")

            # Receive events - collect first 20
            for i in range(20):
                msg = await ws.recv()
                data = json.loads(msg)
                evt_type = data.get("type", "")

                # Print event type and a preview
                preview = json.dumps(data)[:120].replace('\n', ' ')
                print(f"[{evt_type}] {preview}...")

                if evt_type == "thinking":
                    thinking_events += 1
                elif evt_type == "answer":
                    answer_events += 1
                    # Check if answer contains thinking
                    raw = json.dumps(data)
                    if "<think>" in raw or "thinking" in raw.lower():
                        thinking_in_answer = True
                        print("  ^^^ FOUND THINKING IN ANSWER!")
                elif evt_type == "completed" or evt_type == "result":
                    print("--- Session completed ---")
                    break

    except Exception as e:
        print(f"Error: {e}")
        return False

    print("-" * 60)
    print(f"Results:")
    print(f"  thinking events: {thinking_events}")
    print(f"  answer events: {answer_events}")
    print(f"  thinking in answer: {thinking_in_answer}")

    if thinking_events > 0 and not thinking_in_answer:
        print("\n✅ CLAUDE CLI separates thinking from answer (no thinking tags in answer)")
    elif thinking_in_answer:
        print("\n⚠️  CLAUDE CLI includes thinking tags in answer messages")
    else:
        print("\n❓ Need more complex prompt to trigger thinking")

    return thinking_in_answer

if __name__ == "__main__":
    result = asyncio.run(test())
    sys.exit(0 if result else 0)  # Always exit 0 for this test
