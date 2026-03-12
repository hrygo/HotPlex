#!/usr/bin/env python3
"""Test Claude CLI via HotPlex WebSocket - check for thinking tags in Answer"""

import asyncio
import json
import websockets

async def main():
    uri = "ws://localhost:8080/ws/v1/agent?api_key=2fead6aca5ae310628a1586fda5821f6f0b1086ed6c63acc5ee4eadc1f50cb2a"

    prompt = """请分析这个代码库的结构，不需要使用工具，只需要简单描述你看到了什么。"""

    print(f"🔌 Connecting to {uri}...")
    print(f"📝 Prompt: {prompt[:50]}...")
    print("-" * 60)

    thinking_in_answer = 0
    thinking_count = 0
    answer_count = 0

    try:
        async with websockets.connect(uri) as ws:
            # Send start request
            req = {
                "type": "start",
                "prompt": prompt,
                "systemPrompt": "You are a helpful assistant.",
                "maxTurns": 1
            }
            await ws.send(json.dumps(req))
            print("📤 Sent start request")

            # Receive events
            async for msg in ws:
                data = json.loads(msg)
                evt_type = data.get("type", "")

                if evt_type == "thinking":
                    thinking_count += 1
                    content = data.get("content", [])
                    if content and isinstance(content, list):
                        text = content[0].get("text", "")[:80] if isinstance(content[0], dict) else str(content)[:80]
                        print(f"🔹 thinking: {text}...")

                elif evt_type == "answer" or evt_type == "message":
                    answer_count += 1

                    # Check for thinking tags in the answer
                    raw = json.dumps(data)

                    # Check various possible locations for thinking content
                    content = data.get("content", [])
                    message = data.get("message", {})
                    if message:
                        msg_content = message.get("content", [])

                        # Check if any content block is of type "thinking"
                        for block in (content if isinstance(content, list) else []):
                            if isinstance(block, dict) and block.get("type") == "thinking":
                                thinking_in_answer += 1
                                print(f"⚠️  Found thinking block in {evt_type}: {block.get('text', '')[:80]}...")

                        for block in (msg_content if isinstance(msg_content, list) else []):
                            if isinstance(block, dict) and block.get("type") == "thinking":
                                thinking_in_answer += 1
                                print(f"⚠️  Found thinking in message.content: {block.get('text', '')[:80]}...")

                    # Also check raw string
                    if "<think>" in raw or "thinking" in raw:
                        print(f"⚠️  Raw contains thinking: {raw[:150]}...")

                    print(f"📩 {evt_type}: content length = {len(raw)}")

                elif evt_type == "result":
                    print(f"✅ Result: {data.get('result', '')[:100]}...")
                    break

                elif evt_type == "error":
                    print(f"❌ Error: {data.get('error', '')}")
                    break

    except Exception as e:
        print(f"❌ Connection error: {e}")
        return

    print("-" * 60)
    print(f"📊 Summary:")
    print(f"   - thinking events: {thinking_count}")
    print(f"   - answer/message events: {answer_count}")
    print(f"   - thinking in answer: {thinking_in_answer}")

    if thinking_in_answer > 0:
        print("\n✅ CLAUDE CLI DOES return thinking in Answer!")
    else:
        print("\n❌ CLAUDE CLI does NOT return thinking in Answer")

if __name__ == "__main__":
    asyncio.run(main())
