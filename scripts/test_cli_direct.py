#!/usr/bin/env python3
"""Direct test of Claude CLI output - bypassing hotplex"""

import subprocess
import os
import json
import sys

def main():
    # Create a completely clean environment
    env = os.environ.copy()
    # Remove all Claude Code related env vars
    for key in list(env.keys()):
        if 'CLAUDE' in key.upper() or 'ANTHROPIC' in key.upper():
            del env[key]

    prompt = "请解释快速排序算法的原理，需要一些思考过程"

    print(f"Testing Claude CLI directly...")
    print(f"Prompt: {prompt}")
    print("-" * 60)

    proc = subprocess.Popen(
        ['claude', '-p', '--output-format=stream-json',
         '--include-partial-messages',
         '--verbose',
         prompt],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=env,
        text=True,
        bufsize=1
    )

    thinking_count = 0
    answer_count = 0
    thinking_in_answer = False
    raw_output = []

    try:
        # Read stdout and stderr
        import select

        while True:
            # Use select for non-blocking read
            reads = [proc.stdout.fileno(), proc.stderr.fileno()]
            readable, _, _ = select.select(reads, [], [], 1.0)

            if not readable:
                # Check if process exited
                if proc.poll() is not None:
                    break
                continue

            for fd in readable:
                if fd == proc.stdout.fileno():
                    line = proc.stdout.readline()
                    if line:
                        raw_output.append(line)
                        try:
                            data = json.loads(line.strip())
                            evt_type = data.get('type', '')

                            if evt_type == 'thinking':
                                thinking_count += 1
                                content = data.get('content', [])
                                if content:
                                    text = content[0].get('text', '')[:100] if isinstance(content[0], dict) else str(content)[:100]
                                    print(f"🔹 thinking: {text}...")

                            elif evt_type in ['assistant', 'message', 'answer']:
                                answer_count += 1
                                raw = json.dumps(data)
                                if '<think>' in raw or 'thinking' in raw.lower():
                                    thinking_in_answer = True
                                    print(f"⚠️  Found thinking in {evt_type}: {raw[:200]}...")

                        except json.JSONDecodeError:
                            pass

                elif fd == proc.stderr.fileno():
                    err = proc.stderr.readline()
                    if err and 'thinking' in err.lower():
                        print(f"STDERR: {err.strip()[:100]}")

        proc.wait(timeout=10)

    except subprocess.TimeoutExpired:
        proc.kill()
    except Exception as e:
        print(f"Error: {e}")
        proc.kill()

    print("-" * 60)
    print(f"Results:")
    print(f"  thinking events: {thinking_count}")
    print(f"  answer/assistant events: {answer_count}")
    print(f"  thinking in answer: {thinking_in_answer}")

    if thinking_count > 0:
        if thinking_in_answer:
            print("\n⚠️  CLAUDE CLI includes thinking tags IN answer messages!")
        else:
            print("\n✅ CLAUDE CLI separates thinking from answer (correct)")
    else:
        print("\n❓ No thinking events detected from CLI")

    return 0

if __name__ == "__main__":
    sys.exit(main())
