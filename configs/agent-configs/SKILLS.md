---
version: 1
description: "HotPlex platform capabilities and tool usage guide"
---

# SKILLS.md - Tool Usage Guide

## Platform Overview

You are running within **HotPlex Worker Gateway** — a unified access layer that manages AI coding agent sessions. Your output is streamed to end users via messaging platforms. Understanding the platform capabilities helps you deliver better responses.

## Session Lifecycle

Sessions follow a 5-state machine:

`Created → Running → Idle → Terminated → Deleted`

Key behaviors:
- Sessions persist across message exchanges (stateful conversation)
- Idle sessions can be resumed; terminated sessions cannot
- Each session is bound to a specific worker process and work directory

## User Commands

Users interact with you through messaging platforms. Two categories of commands are available:

### Session Control

These manage session lifecycle — they do NOT reach the worker process.

| Commands | Effect |
|:---------|:-------|
| `/gc`, `/park` | Hibernate session — stop worker, preserve session for later resume |
| `/reset`, `/restart`, `/new` | Reset context — same session ID, fresh start from scratch |

### Worker Operations

These are forwarded to the worker process as in-place operations — session state is NOT affected.

| Command | Effect |
|:--------|:-------|
| `/context` | View context window usage |
| `/mcp` | View MCP server status |
| `/model <name>` | Switch AI model |
| `/perm <mode>` | Set permission mode |
| `/effort <level>` | Set reasoning effort level |
| `/compact` | Compress conversation history |
| `/clear` | Clear conversation |
| `/rewind` | Undo last conversation turn |
| `/commit` | Create git commit |

Natural language equivalents exist with `$` prefix (e.g., `$gc` = hibernate, `$休眠` = hibernate, `$重置` = reset, `$上下文` = context usage). These prevent accidental activation from normal conversation.

## Messaging Channels

Your text output is delivered to users through the following channels:

### Slack
- Real-time streaming output (updates appear as you generate them)
- Long responses are automatically split into multiple messages
- Supports image rendering and file uploads
- Thread-based tracking: each session maps to a Slack thread

### Feishu
- Interactive cards for permission requests and user Q&A
- Streaming card updates for real-time output
- Voice message support with automatic transcription

## Voice Input

Users may send voice messages through messaging platforms. These are automatically transcribed to text via STT (Speech-to-Text) before delivery to you. Treat transcribed voice input identically to regular text input — the transcription is transparent.

## Permission Requests

When you need user approval (e.g., to execute a command, access a resource), HotPlex sends an interactive permission request through the messaging platform. Users can approve or deny these requests inline. Unanswered requests auto-deny after 5 minutes to prevent indefinite blocking.

## Admin & Monitoring

HotPlex provides an Admin API for monitoring and management:
- Session listing, inspection, and termination
- System health and configuration checks
- Runtime metrics and statistics

## Output Considerations

- Responses are streamed — structure your output for incremental readability
- Avoid excessively long responses; users can request elaboration if needed
- Use `file:line` format when referencing specific code locations
- Prefer Markdown formatting for structured output (tables, lists, code blocks)
