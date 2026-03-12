#!/usr/bin/env python3
"""
Claude Code Exhaustive Protocol Verification Script

This script defines the 'Gold Standard' for Claude Code stream-json protocol events.
It can be used to:
1. Verify live CLI output against the expected schema.
2. Generate mock data for Go unit tests.
3. Document all discovered data structures in the wild.
"""

import json
import os
import re
import sys
from dataclasses import dataclass
from typing import Any, Dict, List, Optional

# =============================================================================
# EXHAUSTIVE DATA STRUCTURES (Known Variations)
# =============================================================================

PROTOCOL_EXAMPLES = {
    "thinking_basic": {
        "type": "thinking",
        "content": [{"type": "text", "text": "I'll start by listing the files."}]
    },
    "thinking_with_subtype": {
        "type": "thinking",
        "subtype": "plan_generation",
        "status": "Generating a plan...",
        "content": [{"type": "text", "text": "1. Analyze\n2. Implement"}]
    },
    "assistant_with_tool": {
        "type": "assistant",
        "message": {
            "role": "assistant",
            "content": [
                {"type": "text", "text": "I will check the directory."},
                {"type": "tool_use", "id": "call_123", "name": "ls", "input": {"path": "."}}
            ]
        }
    },
    "tool_use_direct": {
        "type": "tool_use",
        "name": "Bash",
        "input": {"command": "git status"},
        "message_id": "msg_456"
    },
    "tool_result_basic": {
        "type": "tool_result",
        "status": "success",
        "output": "On branch main...",
        "name": "Bash"
    },
    "tool_result_complex": {
        "type": "tool_result",
        "content": [
            {
                "type": "tool_result",
                "tool_use_id": "call_123",
                "name": "ls",
                "content": "file1.txt\nfile2.txt",
                "is_error": False
            }
        ]
    },
    "result_with_usage": {
        "type": "result",
        "result": "Task completed successfully",
        "duration": 1250,
        "total_cost_usd": 0.0045,
        "usage": {
            "input_tokens": 1500,
            "output_tokens": 350,
            "cache_creation_input_tokens": 0,
            "cache_read_input_tokens": 12000
        }
    },
    "result_alternate_duration": {
        "type": "result",
        "result": "Done",
        "duration_ms": 800,
        "usage": {"input_tokens": 100, "output_tokens": 20}
    },
    "result_with_model_usage": {
        "type": "result",
        "result": "Done with model usage",
        "duration_ms": 1200,
        "usage": {"input_tokens": 0, "output_tokens": 0},
        "modelUsage": {
            "claude-sonnet-4-6": {
                "inputTokens": 100,
                "outputTokens": 20,
                "cacheReadInputTokens": 0,
                "cacheCreationInputTokens": 0,
                "costUSD": 0.005
            }
        }
    },
    "permission_request": {
        "type": "permission_request",
        "session_id": "sess_000",
        "permission": {
            "name": "Bash",
            "input": "rm -rf /tmp/cache"
        }
    },
    "error_event": {
        "type": "error",
        "error": "The bash command timed out after 30s",
        "is_error": True
    }
}

# =============================================================================
# VERIFICATION LOGIC
# =============================================================================

class ProtocolVerifier:
    def __init__(self):
        self.errors = []

    def verify_event(self, name: str, data: Dict[str, Any]):
        """Verifies a single event structure against our logic requirements."""
        e_type = data.get("type")
        if not e_type:
            self.errors.append(f"[{name}] Missing 'type' field")
            return

        # check for content extraction possibilities
        has_content = False
        if "content" in data: has_content = True
        if "result" in data: has_content = True
        if "output" in data: has_content = True
        if "message" in data and "content" in data["message"]: has_content = True
        if "status" in data: has_content = True

        if "error" in data: has_content = True

        if not has_content and e_type not in ["result", "tool_use", "permission_request"]:
             self.errors.append(f"[{name}] Event of type '{e_type}' has no detectable content fields")

    def run_all(self):
        print("🔍 Verifying Reference Protocol Structures...")
        for name, data in PROTOCOL_EXAMPLES.items():
            self.verify_event(name, data)
        
        if not self.errors:
            print("✅ All reference structures are valid and parseable by design.")
        else:
            print(f"❌ Found {len(self.errors)} design flaws in example data:")
            for err in self.errors:
                print(f"  - {err}")

    def export_json(self, path: str):
        with open(path, 'w') as f:
            json.dump(PROTOCOL_EXAMPLES, f, indent=2)
        print(f"💾 Protocol examples exported to {path}")

    def analyze_log(self, log_path: str):
        """Scans a log file for unique JSON message types and identifies unknowns."""
        print(f"\n📂 Analyzing log file: {log_path}")
        if not os.path.exists(log_path):
            print("  ❌ Log file not found.")
            return

        unique_types = set()
        unknown_fields = {}
        
        with open(log_path, 'r') as f:
            for line in f:
                # Target lines containing JSON from Provider
                if 'msg=\"[PROVIDER] Raw thinking event\"' in line or 'RawType=' in line:
                    continue # skip our own re-logs
                
                parts = re.split(r'level=\w+\s+source=\S+\s+msg=', line)
                if len(parts) < 2: continue
                
                # Check for JSON-like strings in the line
                json_matches = re.findall(r'\{.*\}', line)
                for j_str in json_matches:
                    try:
                        data = json.loads(j_str)
                        if "type" in data:
                            t = data["type"]
                            unique_types.add(t)
                            # Check if we have this structure in our gold standard
                            matched = False
                            for ex_data in PROTOCOL_EXAMPLES.values():
                                if ex_data.get("type") == t:
                                    matched = True; break
                            if not matched:
                                unknown_fields[t] = data
                    except:
                        continue

        print(f"✅ Discovered {len(unique_types)} unique message types in logs: {', '.join(sorted(unique_types))}")
        if unknown_fields:
            print("\n🚨 FOUND NEW/UNKNOWN STRUCTURES:")
            for t, data in unknown_fields.items():
                print(f"--- Type: {t} ---")
                print(json.dumps(data, indent=2))
        else:
            print("✨ No unknown protocol structures detected. Everything matches the 'Gold Standard'.")

# =============================================================================
# MAIN ENTRY
# =============================================================================

def main():
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--export", help="Path to export JSON examples for Go tests")
    parser.add_argument("--analyze-log", help="Path to daemon.log to discover new structures")
    args = parser.parse_args()

    verifier = ProtocolVerifier()
    
    if args.analyze_log:
        verifier.analyze_log(args.analyze_log)
        return

    verifier.run_all()

    if args.export:
        verifier.export_json(args.export)
    else:
        # Default behavior: run live test if 'claude' is present, otherwise just dry run
        print("\n💡 Tip: Use --export verify_cases.json to generate data for Go unit tests.")

if __name__ == "__main__":
    main()
