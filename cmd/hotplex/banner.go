package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/hrygo/hotplex/internal/cli/output"
)

// ANSI escape codes for TTY output.
const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiCyan  = "\033[36m"
	ansiDim   = "\033[2m"
	ansiGreen = "\033[32m"
	ansiRed   = "\033[31m"
)

//go:embed banner_art.txt
var bannerArt string

//go:generate go run ../../scripts/gen_banner.go -cols 80

// BuildInfo holds compile-time and runtime metadata.
type BuildInfo struct {
	Version   string
	BuildTime string
	GoVersion string
	OS        string
	Arch      string
}

func newBuildInfo() BuildInfo {
	return BuildInfo{
		Version:   versionString(),
		BuildTime: buildTime,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// RuntimeStatus holds component state for the status panel.
type RuntimeStatus struct {
	GatewayAddr     string
	AdminAddr       string
	WebChatAddr     string
	WebChatEmbedded bool
	TLSEnabled      bool
	DBPath          string
	PoolMax         int
	PoolIdle        int
	Adapters        []AdapterStatus
	RetryEnabled    bool
	RetryMax        int
	RetryDelay      string
}

// AdapterStatus reports a single messaging adapter's state.
type AdapterStatus struct {
	Name    string
	BotName string
	Started bool
}

// writeAll writes strings to w, ignoring errors (banner output is best-effort).
func writeAll(w io.Writer, lines ...string) {
	for _, l := range lines {
		_, _ = fmt.Fprintln(w, l)
	}
}

func printStartupBanner(out *os.File, info BuildInfo, s RuntimeStatus, configPath string) {
	tty := output.IsTTY(out)

	bold := func(text string) string {
		if tty {
			return ansiBold + text + ansiReset
		}
		return text
	}
	cyan := func(text string) string {
		if tty {
			return ansiCyan + text + ansiReset
		}
		return text
	}
	dim := func(text string) string {
		if tty {
			return ansiDim + text + ansiReset
		}
		return text
	}
	green := func(text string) string {
		if tty {
			return ansiGreen + text + ansiReset
		}
		return text
	}
	red := func(text string) string {
		if tty {
			return ansiRed + text + ansiReset
		}
		return text
	}

	pad := func(label, value string) string {
		return fmt.Sprintf("  %s%s", bold(fmt.Sprintf("%-11s", label)), value)
	}

	const sectionWidth = 48

	sectionHeader := func(name string) string {
		dashLen := sectionWidth - 2 - len(name) - 1
		if dashLen < 3 {
			dashLen = 3
		}
		return "  " + bold(name) + " " + dim(strings.Repeat("─", dashLen))
	}

	sectionPad := func(label, value string) string {
		return fmt.Sprintf("    %s%s", bold(fmt.Sprintf("%-15s", label)), value)
	}

	wsScheme := "ws"
	if s.TLSEnabled {
		wsScheme = "wss"
	}

	var lines []string

	// ASCII art + build info
	lines = append(lines, "", cyan(bannerArt), "")
	lines = append(lines,
		pad("Version", cyan(info.Version)),
		pad("Build", info.BuildTime),
		pad("Go", fmt.Sprintf("%s · %s/%s", info.GoVersion, info.OS, info.Arch)),
	)
	if configPath != "" {
		lines = append(lines, pad("Config", configPath))
	}

	// ── Endpoints ────────────────────────────────────────────
	lines = append(lines, "", sectionHeader("Endpoints"))
	lines = append(lines, sectionPad("Gateway", "http://"+s.GatewayAddr))
	lines = append(lines, sectionPad("WebSocket", wsScheme+"://"+s.GatewayAddr+"/ws"))
	lines = append(lines, sectionPad("Health", "http://"+s.GatewayAddr+"/health"))
	if s.WebChatEmbedded {
		lines = append(lines, sectionPad("WebChat", "http://"+s.GatewayAddr+"/ "+dim("(embedded)")))
		lines = append(lines, sectionPad("Admin UI", "http://"+s.GatewayAddr+"/admin "+dim("(embedded)")))
	} else if s.WebChatAddr != "" {
		lines = append(lines, sectionPad("WebChat", "http://"+s.WebChatAddr))
		lines = append(lines, sectionPad("Admin UI", "http://"+s.WebChatAddr+"/admin"))
	}
	lines = append(lines, sectionPad("Docs", "http://"+s.GatewayAddr+"/docs/"))
	if s.AdminAddr != "" {
		lines = append(lines, sectionPad("Admin API", "http://"+s.AdminAddr))
	}

	// ── Bots ─────────────────────────────────────────────────
	if len(s.Adapters) > 0 {
		lines = append(lines, "", sectionHeader("Bots"))
		for _, a := range s.Adapters {
			name := a.Name
			if a.BotName != "" {
				name += "/" + a.BotName
			}
			if a.Started {
				lines = append(lines, "    "+name+"  "+green("✓"))
			} else {
				lines = append(lines, "    "+name+"  "+red("✗"))
			}
		}
	}

	// ── Resources ────────────────────────────────────────────
	lines = append(lines, "", sectionHeader("Resources"))
	lines = append(lines, sectionPad("Database", s.DBPath))
	lines = append(lines, sectionPad("Pool", fmt.Sprintf("%d sessions / %d idle per user", s.PoolMax, s.PoolIdle)))
	if s.RetryEnabled {
		lines = append(lines, sectionPad("LLM Retry", green(fmt.Sprintf("✓ %d retries, %s delay", s.RetryMax, s.RetryDelay))))
	}

	lines = append(lines, "")
	writeAll(out, lines...)
}
