# Issue: OpenCodeProvider fails to start session in CLI mode without initial message argument

## HotPlex Version
v0.8.0

## Claude Code / OpenCode Version
- Claude Code: 2.1.50
- OpenCode: 1.2.10

## What happened?
The `OpenCodeProvider` fails to execute prompts when using the `opencode` CLI mode. During verification of the `_examples/go_opencode_basic` and `_examples/go_opencode_lifecycle` examples, the engine started the `opencode` process, but the process exited immediately with its help documentation or an error stating "You must provide a message or a command".

This is because `opencode run` (v1.2.10) requires at least one positional argument (the message/prompt) to start a session or continue one. Currently, `OpenCodeProvider` attempts to pass the prompt via `stdin` (following the `ClaudeCodeProvider` pattern), but `opencode` CLI does not wait for `stdin` if no initial message is provided in the command line.

## Steps to Reproduce
1. Ensure `opencode` CLI is installed (v1.2.10).
2. Run the basic Go example:
   ```bash
   go run _examples/go_opencode_basic/main.go
   ```
3. Observe the output logs showing the OpenCode help menu and a "session is dead" error.

## Actual Behavior
```
=== HotPlex OpenCode Provider Demo ===
...
🤔 Thinking: ai.thinking
time=2026-02-22T18:19:47.181+08:00 level=INFO msg="Engine: starting execution pipeline" namespace=opencode_demo session_id=opencode-session-1
time=2026-02-22T18:19:47.183+08:00 level=INFO msg="OS Process started (Cold Start)" pid=95831 pgid=95831
opencode run [message..]
run opencode with a message
...
Error: You must provide a message or a command
```

## Expected Behavior
The `opencode` process should start and wait for input or process the initial prompt provided during execution.

## Proposed Solution
Update the `Provider` interface or `OpenCodeProvider` implementation to allow passing the initial prompt into `BuildCLIArgs`. When a session is being started (Cold Start), the first prompt should be appended as a positional argument to the `opencode run` command.

```go
// In provider/opencode_provider.go
func (p *OpenCodeProvider) BuildCLIArgs(providerSessionID string, opts *ProviderSessionOptions) []string {
    args := []string{"run"}
    // ... other flags ...
    
    // If this is a cold start and we have a prompt (needs interface update or context)
    // args = append(args, initialPrompt)
    
    return args
}
```
