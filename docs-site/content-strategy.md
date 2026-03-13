# HotPlex Documentation Content Strategy

**Version:** 1.0  
**Status:** Draft for Review  
**Last Updated:** 2026-03-01

---

## Executive Summary

This document establishes the editorial guidelines, content structure, and quality standards for all HotPlex documentation. The strategy prioritizes **technical accuracy**, **practical utility**, and **approachable expertise** over marketing language.

**Guiding Principle:** Documentation should answer "How do I...?" and "Why should I...?" with clarity, while being honest about limitations and trade-offs.

---

## 1. Tone & Voice Guidelines

### 1.1 Core Voice

| Attribute | Target | Avoid |
|-----------|--------|-------|
| **Formality** | Semi-formal, professional but not stiff | Academic jargon, casual slang |
| **Perspective** | Second-person ("you") | Passive voice, third-person marketing |
| **Confidence** | Direct and actionable | Hedging, uncertainty without justification |
| **Empathy** | Acknowledge user pain points | Dismissing difficulties, assuming expertise |

### 1.2 Tone Spectrum by Content Type

| Content Type | Tone | Rationale |
|--------------|------|-----------|
| **Introduction/Overview** | Welcoming, clear, brief | First impression; respect reader's time |
| **Tutorials** | Practical, step-by-step, encouraging | Learning by doing; errors are expected |
| **Reference** | Precise, complete, dry | Authority; users need exact information |
| **Blog/Roadmap** | Forward-looking but grounded | Vision without over-promising |
| **Troubleshooting** | Empathetic, methodical | Users are frustrated; they need help |

### 1.3 Metaphor Guidelines

**Status: RESTRICTED**

Metaphors obscure technical reality. Use them sparingly and always ground them with concrete explanations.

#### ✅ Acceptable Metaphors (Limited Use)

- **"Bridge"** — Only when explicitly explaining the protocol translation layer
- **"Heartbeat"** — Only for health check/heartbeat mechanisms
- **"Gateway"** — Only for entry-point/adaptor components

#### ❌ Metaphors to Avoid

| Current Usage | Issue | Recommended Replacement |
|---------------|-------|------------------------|
| "Strategic Bridge" | Overused, marketing-heavy | "Integration layer" or "protocol adapter" |
| "Duplex Harmony" | Vague, poetic | "Full-duplex streaming" or "bidirectional communication" |
| "Nervous System" | Imprecise | "Event system" or "lifecycle hooks" |
| "The Bridge" (repeated) | Identity confusion | Use component names: "ChatApp adapter", "engine" |
| "Amnesia Problem" | Cute but unclear | "Context loss" or "state not persisted" |
| "Strategic Horizon" | Corporate jargon | Remove; use timeline: "Q1 2026 focus: Trust" |
| "Soul" / "Magic" | Implies unknowability | "Intelligence", "capability", or remove entirely |
| "Reflexes" | Confuses biological with technical | "Event handlers" or "hooks" |

#### Example Transformation

**Before:**
> "HotPlex is the Bridge that connects the Mundane Infrastructure to the Agentic Magic."

**After:**
> "HotPlex is an integration layer that connects AI CLI tools (like Claude Code) to your application's workflow, handling session persistence and security so you can focus on building agent behavior."

### 1.4 Phrase Library

#### ✅ Preferred Phrases

- "HotPlex provides..." — Direct feature statement
- "To achieve X, use Y..." — Action-oriented
- "This is useful when..." — Contextual guidance
- "Note: This requires..." — Prerequisite callout
- "Known limitation: ..." — Honesty about constraints

#### ❌ Phrases to Avoid

| Avoid | Reason | Replace With |
|-------|--------|--------------|
| "Game-changing" | Hyperbole | Remove or be specific |
| "Revolutionary" | Marketing fluff | Describe the actual benefit |
| "Effortlessly" | Unsubstantiated | "With minimal configuration" |
| "Seamlessly" | Meaningless | Explain the integration step |
| "Simply" | Dismisses difficulty | "You need to..." |
| "Of course" | Assumes knowledge | "Note that..." |
| "As you can see" | Condescending | Remove |

### 1.5 The "Soul" Quote

**Current:** "We handle the state, you handle the soul."

**Analysis:** This quote encapsulates several tone issues:
- "Soul" is undefined and metaphorical
- Implies HotPlex has agency ("we handle")
- Creates an artificial division between "boring" and "interesting" work

**Recommendation:** Retire this quote from all documentation. If a tagline is needed, use:

> **"HotPlex handles the infrastructure. You build the agent."**

---

## 2. Content Depth Levels

### 2.1 Level Definitions

| Level | Time to Read | Purpose | Structure |
|-------|--------------|---------|-----------|
| **Overview** | 30 seconds | What is this? Do I care? | 1-2 sentences + key benefits |
| **Tutorial** | 5 minutes | How do I use this? | Numbered steps + minimal explanation |
| **Guide** | 15-30 minutes | How do I solve a problem? | Conceptual + code + explanation |
| **Reference** | Variable | What's every option? | API specs, tables, exhaustive |

### 2.2 When to Use Each Level

| Document Type | Primary Level | Secondary Level |
|---------------|---------------|-----------------|
| **Introduction Page** | Overview | Guide (linked) |
| **Getting Started** | Tutorial | Overview |
| **Architecture** | Guide | Reference (links) |
| **API Reference** | Reference | None |
| **Troubleshooting** | Guide | Tutorial |
| **Blog Posts** | Overview | Guide |

### 2.3 Content Depth Templates

#### Overview Template (50-100 words)

```markdown
## [Feature Name]

[Brief description: what it does in 1 sentence.]

### When to Use
- Use case 1
- Use case 2

### Requirements
- Prerequisite 1
- Prerequisite 2

[Link to Tutorial] → [Link to Reference]
```

#### Tutorial Template (300-500 words)

```markdown
## [Task Name]

**Time:** X minutes  
**Prerequisites:** [Link to prerequisites]

### Goal
[One sentence: what you'll accomplish]

### Steps

1. [First action]
   ```bash
   # command or code
   ```

2. [Second action]
   ```go
   // code with comments
   ```

3. [Verification step]
   ```bash
   # verify it worked
   ```

### Next Steps
- [Link to deeper guide]
- [Link to reference]
```

#### Guide Template (800-2000 words)

```markdown
## [Topic]

### Concept
[Explain the concept with a diagram if helpful]

### Prerequisites
- [Prerequisite 1]
- [Prerequisite 2]

### Implementation

#### Step 1: [Sub-task]
[Explanation + code]

#### Step 2: [Sub-task]
[Explanation + code]

### Best Practices
- [Practice 1 with rationale]
- [Practice 2 with rationale]

### Common Issues
| Issue | Cause | Solution |
|-------|-------|----------|
| [Error] | [Root cause] | [Fix] |

### Related
- [Link to related guides]
- [Link to reference]
```

### 2.4 Depth Transition Signals

Use these phrases to signal depth changes:

| Signal | Meaning |
|--------|---------|
| "In short:..." | Summary (Overview) |
| "Here's how to..." | Tutorial start |
| "For example:..." | Concrete illustration |
| "Under the hood:..." | Deep dive |
| "See also:..." | Reference material |
| "Technical details:..." | Expert-level content |

---

## 3. Code Example Standards

### 3.1 Example Classification

| Type | Use Case | Complexity |
|------|----------|------------|
| **Minimal** | Quick demonstration, imports | 10-20 lines, no error handling |
| **Standard** | Common use cases | 30-80 lines, basic error handling |
| **Production** | Real-world deployment | 100+ lines, full error handling, logging |

### 3.2 Required Elements by Type

#### Minimal Example
- ✅ Working code (copy-paste runs)
- ✅ Import statements
- ✅ Basic configuration
- ❌ No error handling required
- ❌ No logging
- ❌ No cleanup (defer)

#### Standard Example
- ✅ All minimal elements
- ✅ Error handling (at least `if err != nil`)
- ✅ Basic logging (optional)
- ✅ Context propagation
- ❌ No retry logic
- ❌ No advanced configuration

#### Production Example
- ✅ All standard elements
- ✅ Retry logic
- ✅ Timeout handling
- ✅ Proper logging
- ✅ Metrics/observability hooks
- ✅ Graceful shutdown
- ✅ Configuration via env vars or config file

### 3.3 Code Example Annotation Guidelines

Every code example beyond minimal must include:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/hrygo/hotplex"
)

// Config defines application configuration.
// In production, load from env vars or config file.
type Config struct {
    WorkDir     string
    SessionID   string
    Timeout     time.Duration
}

func main() {
    // 1. Initialize the engine.
    // Best practice: Create once, reuse across requests.
    engine, err := hotplex.NewEngine(hotplex.EngineOptions{
        Timeout:      5 * time.Minute,
        AllowedTools: []string{"Bash", "Read", "Edit"},
    })
    if err != nil {
        // Always handle initialization errors.
        // In production, use structured logging.
        fmt.Fprintf(os.Stderr, "failed to create engine: %v\n", err)
        os.Exit(1)
    }
    defer engine.Close() // Clean up resources.

    // 2. Execute with session persistence.
    ctx := context.Background()
    err = engine.Execute(ctx, &hotplex.Config{
        WorkDir:    "/tmp/sandbox",
        SessionID:  "user-123",
    }, "Hello, world!", func(event string, data any) error {
        fmt.Printf("event: %s, data: %v\n", event, data)
        return nil
    })
    if err != nil {
        fmt.Fprintf(os.Stderr, "execution failed: %v\n", err)
    }
}
```

**Annotation requirements:**
1. **Section comments** ("1. Initialize...") — explain each step
2. **"Best practice" comments** — call out recommended patterns
3. **"In production" notes** — indicate what's different in real use
4. **Error handling** — always show, even if just logging

### 3.4 Code Example Quality Checklist

- [ ] Code compiles (verified)
- [ ] Variables are named clearly
- [ ] Each block has a comment explaining purpose
- [ ] Error handling present and explained
- [ ] "Best practice" notes for production use
- [ ] Minimal version available (for quick testing)
- [ ] Links to full reference documentation
- [ ] Language matches the page (Go examples on Go pages)

---

## 4. Visual Content Requirements

### 4.1 Diagram Decision Criteria

| Use Diagrams When | Don't Use When |
|-------------------|----------------|
| Showing relationships between components | Explaining linear steps (use numbered list) |
| Visualizing data flow | Showing code structure (use code blocks) |
| Explaining architecture | Describing configuration (use tables) |
| Comparing options | Listing features (use bullet points) |

### 4.2 Diagram Complexity Guidelines

**Maximum elements per diagram:** 7 (±2)

If a diagram has more elements:
1. Break into multiple diagrams
2. Use分层 (layers) with clear boundaries
3. Create a "high-level" and "detailed" version

### 4.3 Diagram Style Requirements

| Element | Standard |
|---------|----------|
| **Labels** | All boxes and arrows labeled |
| **Direction** | Left-to-right or top-to-bottom flow |
| **Colors** | Max 3 colors per diagram; use gray for optional |
| **Technology** | Mermaid.js preferred; SVG for complex diagrams |
| **Accessibility** | All diagrams have alt text |

### 4.4 Interactive Elements

**Recommended interactive features:**
- Tabbed code examples (language switching)
- Collapsible "Technical details" sections
- Copy-to-clipboard for code blocks
- Version switches for API docs

**Not recommended:**
- Interactive architecture diagrams (maintenance burden)
- Animated sequences (accessibility issues)

---

## 5. Localization Strategy

### 5.1 Language Priority

| Language | Status | Priority |
|----------|--------|----------|
| **English (en)** | Primary | 1 |
| **Chinese Simplified (zh-CN)** | Secondary | 2 |

### 5.2 Development Model: English-First with Translation

```
[English Draft] → [Translation] → [Chinese Review]
      ↓                ↓
  Canonical      Natural Chinese
  Reference     Adaptation
```

**Rationale:**
- English is the lingua franca of open source
- HotPlex targets global开发者社区
- Translation catches documentation gaps

### 5.3 What Must Be Translated

| Content Type | Translation Required | Notes |
|--------------|---------------------|-------|
| **Overview/Intro** | ✅ Yes | Critical first impression |
| **Tutorials** | ✅ Yes | Core user journey |
| **API Reference** | ✅ Yes | Must match English signatures |
| **Error Messages** | ✅ Yes | User-facing text |
| **Blog Posts** | ⚠️ Optional | Lower priority |
| **Roadmaps** | ⚠️ Optional | Lower priority |

### 5.4 What Can Be Reused

| Content | Reuse Strategy |
|---------|---------------|
| **Code Examples** | 100% reuse — same API |
| **Mermaid Diagrams** | 100% reuse — language-neutral |
| **Configuration Tables** | 100% reuse — same schema |
| **URLs/Permalinks** | 100% reuse — consistent routing |

### 5.5 Translation Quality Standards

**Hard requirements:**
- [ ] Technical terms consistent with HotPlex codebase (see glossary)
- [ ] Code comments translated (or kept English with Chinese explanation)
- [ ] No machine translation without human review
- [ ] Chinese uses simplified characters (zh-CN)

**Style differences:**
- Chinese can be slightly more formal
- Chinese may use more complete sentences
- Avoid direct word-for-word translation

### 5.6 Terminology Glossary (EN → ZH)

| English | Chinese | Notes |
|---------|---------|-------|
| Engine | 引擎 | Core component |
| Session | 会话 | Persistent conversation |
| Hook | 钩子 | Event interception |
| Sandbox | 沙箱 | Security isolation |
| Provider | 提供商 | CLI tool adapter |
| ChatApp | 聊天应用 | Slack, Feishu |
| WorkDir | 工作目录 | Working directory |
| Full-duplex | 全双工 | Bidirectional streaming |
| Stateful | 有状态的 | Persistent context |

---

## 6. Truthfulness Standards

### 6.1 Core Principles

1. **Never exaggerate** — If a feature has limits, document them
2. **Never hide trade-offs** — Every choice has costs
3. **Never promise future features** — Only document shipped functionality
4. **Always provide evidence** — Claims require data

### 6.2 Performance Claims

**Rule:** Every performance claim must have a benchmark or test result.

| Claim Type | Required Evidence |
|------------|-------------------|
| "Sub-second latency" | Benchmark in `/docs/benchmark-*` |
| "1000+ concurrent sessions" | Load test results |
| "X% faster than Y" | Comparative benchmark |
| "Minimal overhead" | Baseline vs. HotPlex comparison |

**Example:**

> **Claim:** "HotPlex eliminates cold start overhead."
>
> **With Evidence:**
> "HotPlex eliminates cold start overhead. In local testing with Claude Code, the first response arrives in ~200ms (engine warm) vs. ~3000ms (cold start), based on 50 runs averaged."

### 6.3 Known Limitations

Every feature page must include a "Limitations" or "Known Issues" section:

```markdown
### Limitations

- **Platform:** Currently only supports macOS and Linux. Windows support is experimental.
- **Concurrency:** Maximum 1000 concurrent sessions per engine instance.
- **Providers:** Not all Claude Code features are exposed via HotPlex.
```

### 6.4 Feature Flags and Stability

| Status | Meaning | Documentation |
|--------|---------|---------------|
| **Stable** | Production-ready | Full docs + examples |
| **Beta** | Tested but may change | Docs with "Beta" tag |
| **Experimental** | Untested | Minimal docs, warning label |
| **Deprecated** | Being removed | Docs + migration guide |

---

## 7. Quality Checklist for New Content

### 7.1 Pre-Publish Checklist

**Tone & Voice:**
- [ ] No marketing hyperbole ("revolutionary", "game-changing")
- [ ] Metaphors limited and grounded
- [ ] Second-person voice ("you")
- [ ] Active voice predominates

**Content Depth:**
- [ ] Overview level available (30-second summary)
- [ ] Tutorial level has numbered steps
- [ ] Reference level linked for exhaustive details
- [ ] Depth signals used appropriately

**Code Examples:**
- [ ] At least one working example
- [ ] Error handling included
- [ ] "Best practice" annotations present
- [ ] Tested/verified to compile

**Visuals:**
- [ ] Diagrams used for component relationships
- [ ] Tables used for configuration/options
- [ ] Diagrams have under 7 elements

**Truthfulness:**
- [ ] No unsubstantiated performance claims
- [ ] Limitations documented
- [ ] Feature stability indicated

**Localization:**
- [ ] English version complete
- [ ] Chinese translation scheduled

### 7.2 Content Review Questions

1. **Can a new user understand what this does in 30 seconds?**
2. **Can a developer copy-paste this code and have it work?**
3. **Is every technical term defined or linked?**
4. **Are the limitations visible?**
5. **Does this match the tone guidelines?**

---

## 8. Before/After Examples

### Example 1: Introduction Page

**Before (current):**
```markdown
# The Philosophy of the Bridge

## Beyond Infrastructure: The HotPlex Manifesto

In the era of Generative AI, the world is divided into two realms: the **Mundane Infrastructure** (state, security, protocols) and the **Agentic Magic** (reasoning, creativity, autonomy). Most developers are forced to choose between them.

**HotPlex is the Bridge.**

We believe that for an AI agent to be truly useful, it must be **Stateful**, **Secure**, and **Seamlessly Integrated**. We handle the "boring" complexity of lifecycle management so you can focus on the "soul" of your agent.
```

**After:**
```markdown
# HotPlex Overview

HotPlex is an integration layer that connects AI CLI tools (like Claude Code and OpenCode) to your application, providing:

- **Persistent sessions** — Agents remember context across requests
- **Process isolation** — Safe execution with PGID-level containment
- **Full-duplex streaming** — Real-time token delivery via WebSocket

## Who is this for?

| User Type | Use Case |
|-----------|----------|
| Product teams | Add AI agents to Slack, Feishu, websites |
| DevOps engineers | Automate CLI-based workflows |
| Platform builders | Embed AI capabilities into products |

## Quick Example

[Minimal working example]

## Next Steps

- [Getting Started](/guide/getting-started) — 5-minute tutorial
- [Architecture](/guide/architecture) — Deep dive
- [API Reference](/reference/api) — Full documentation
```

### Example 2: Architecture Page Section

**Before (current):**
```markdown
### The Pillars of Structural Integrity

HotPlex isn't just a runtime; it's a nervous system. While raw LLMs provide the "brain," HotPlex provides the reflexes, memory, and skin that allow an agent to survive and thrive in production.
```

**After:**
```markdown
### Core Components

HotPlex consists of three main layers:

| Layer | Responsibility |
|-------|----------------|
| **Access Layer** | SDK and protocol interfaces (Go, WebSocket, HTTP) |
| **Engine Layer** | Session management, security enforcement, lifecycle orchestration |
| **Process Layer** | Isolated subprocess execution with PGID containment |

Each layer communicates via bounded Go channels, ensuring deterministic I/O under load.
```

### Example 3: Feature Description

**Before:**
```markdown
### Duplex Harmony

Communication should be a conversation, not a series of requests. Our binary-powered streaming engine provides sub-millisecond event latency for real-time reactivity.
```

**After:**
```markdown
### Full-Duplex Streaming

HotPlex maintains persistent bidirectional channels to the agent process. This means:

- **Output streams continuously** — Tokens arrive as generated, not after completion
- **Input can be injected** — Send commands to the agent during execution
- **Latency is minimal** — Sub-second from LLM token to your callback

```go
// Example: Streaming response
engine.Execute(ctx, cfg, "Explain quantum computing", 
    func(event string, data any) error {
        if event == "token" {
            fmt.Print(data) // Prints immediately as tokens arrive
        }
        return nil
    })
```
```

---

## 9. Action Items

### Immediate (Before Task 4)
- [ ] Remove "Soul" quote from all pages
- [ ] Audit introduction page for metaphor removal
- [ ] Create terminology glossary file

### During Content Overhaul (Task 4)
- [ ] Apply depth level template to each page
- [ ] Add "Limitations" sections to feature pages
- [ ] Add benchmark links to performance claims
- [ ] Ensure all code examples compile
- [ ] Generate Chinese translations

### Ongoing
- [ ] New features require docs at all depth levels
- [ ] Performance claims require benchmarks
- [ ] Review PRs against this checklist

---

## Appendix: Quick Reference Cards

### Tone Cheat Sheet
```
✅ Do:     "Use X to achieve Y"
✅ Do:     "This requires Z"  
✅ Do:     "Known limitation: ..."
❌ Don't:  "Revolutionary", "game-changing"
❌ Don't:  "Simply", "effortlessly"
❌ Don't:  Undefined metaphors
```

### Depth Level Quick Guide
```
30 sec   → Overview (What is this?)
5 min    → Tutorial (How do I do X?)
15 min   → Guide (How do I solve problem Y?)
∞        → Reference (All options)
```

### Code Example Quick Guide
```
Minimal  → 10-20 lines, no error handling, quick demo
Standard → 30-80 lines, basic error handling, learning
Production → 100+ lines, full error handling, logging, metrics
```

---

*Document Status: Ready for Review*  
*Next Review: After Task 4 (Content Overhaul) Completion*
