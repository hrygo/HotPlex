#!/usr/bin/env python3
"""
Claude Code /reset Command Implementation

SCHEME (verified working):
1. Delete ~/.claude/projects/{workspace}/{ProviderSessionID}.jsonl
2. Terminate Claude Code process  
3. Next message cold-starts with fresh context

TEST RESULT:
- Before reset: "ping" → "hi" (context active)
- After reset:  "ping" → "Pong!" (context cleared)
"""

print(__doc__)

print("\n=== Implementation ===")
print("""
File: chatapps/slack/adapter.go

Functions:
1. handleResetCommand() - Main /reset handler
2. deleteClaudeCodeSessionFile() - Deletes project session file  
3. deleteHotPlexMarker() - Deletes HotPlex session marker

Flow:
  /reset command
      ↓
  1. Delete ~/.claude/projects/{workspace}/{sessionID}.jsonl
  2. Delete ~/.hotplex/sessions/{sessionID}.lock
  3. Stop session process
      ↓
  Next message → Cold start → Fresh context ✅
""")

print("\n=== Comparison ===")
print("""
| Command | Session File | Marker | Process | Next Message |
|---------|-------------|--------|---------|--------------|
| /dc     | Keep        | Keep   | Kill    | Resume       |
| /reset  | DELETE      | DELETE | Kill    | COLD START   |
""")

print("\n=== Usage in Slack ===")
print("""
1. Set context: "记住：ping 回复 hi"
2. Test: "ping" → "hi"
3. Send: /reset
4. Test: "ping" → Fresh response (no memory of "hi")
""")
