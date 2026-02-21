// Package security provides WAF-like protection for the HotPlex engine.
//
// This package implements a Web Application Firewall (WAF) that inspects
// LLM-generated commands before they are dispatched to the host shell.
// It enforces strict security boundaries regardless of the model's own
// safety alignment.
//
// The Detector uses regex pattern matching to identify and block potentially
// dangerous operations such as:
//   - Command injection attempts ($(), backticks, eval)
//   - Privilege escalation (sudo, su, pkexec)
//   - Network penetration (reverse shells)
//   - Persistence mechanisms (crontab, systemd)
//   - Information gathering (reading /etc/passwd, SSH keys)
//   - Container escape (privileged docker, chroot)
//   - Kernel manipulation (insmod, modprobe)
//   - Destructive file operations (rm -rf /)
//
// Usage:
//
//	detector := security.NewDetector(logger)
//	detector.SetAdminToken("secret-token")
//
//	if event := detector.CheckInput(userInput); event != nil {
//	    // Block dangerous operation
//	    return ErrDangerBlocked
//	}
package security
