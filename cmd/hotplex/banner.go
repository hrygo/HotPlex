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
	DBDriver        string
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
	Name         string
	BotName      string
	WorkerType   string
	WorkerDetail string // e.g. ACP agent command
	Started      bool
}

// writeAll writes strings to w, ignoring errors (banner output is best-effort).
func writeAll(w io.Writer, lines ...string) {
	for _, l := range lines {
		_, _ = fmt.Fprintln(w, l)
	}
}

func formatBannerURL(tty bool, scheme, addr, path string) string {
	if addr == "" {
		return ""
	}
	host := addr
	switch {
	case strings.HasPrefix(host, ":"):
		host = "127.0.0.1" + host
	case host == "0.0.0.0":
		host = "127.0.0.1"
	case strings.HasPrefix(host, "0.0.0.0:"):
		host = "127.0.0.1" + host[7:]
	}
	url := scheme + "://" + host + path
	if tty {
		return "\033[38;2;100;149;237m" + url + ansiReset
	}
	return url
}

func printStartupBanner(out *os.File, info BuildInfo, s RuntimeStatus, configPath string) {
	tty := output.IsTTY(out)

	bold := func(text string) string {
		if tty {
			return ansiBold + text + ansiReset
		}
		return text
	}
	orange := func(text string) string {
		if tty {
			return "\033[38;2;255;138;0m" + text + ansiReset
		}
		return text
	}
	teal := func(text string) string {
		if tty {
			return "\033[38;2;0;185;203m" + text + ansiReset
		}
		return text
	}
	emerald := func(text string) string {
		if tty {
			return "\033[38;2;40;167;69m" + text + ansiReset
		}
		return text
	}
	rose := func(text string) string {
		if tty {
			return "\033[38;2;220;53;69m" + text + ansiReset
		}
		return text
	}
	gray := func(text string) string {
		if tty {
			return "\033[38;2;120;120;120m" + text + ansiReset
		}
		return text
	}

	pad := func(label, value string) string {
		return fmt.Sprintf("  %s%s", gray(fmt.Sprintf("%-11s", label)), value)
	}

	const sectionWidth = 48

	sectionHeader := func(name string) string {
		dashLen := sectionWidth - 2 - len(name) - 1
		if dashLen < 3 {
			dashLen = 3
		}
		return "  " + teal(bold(name)) + " " + gray(strings.Repeat("─", dashLen))
	}

	sectionPad := func(label, value string) string {
		return fmt.Sprintf("    %s%s", gray(fmt.Sprintf("%-15s", label)), value)
	}

	formatURL := func(scheme, addr, path string) string {
		return formatBannerURL(tty, scheme, addr, path)
	}

	wsScheme := "ws"
	if s.TLSEnabled {
		wsScheme = "wss"
	}

	var lines []string

	// ASCII art + build info
	lines = append(lines, "", bannerArt, "")
	lines = append(lines,
		pad("Version", orange(info.Version)),
		pad("Build", info.BuildTime),
		pad("Go", fmt.Sprintf("%s · %s/%s", info.GoVersion, info.OS, info.Arch)),
	)
	if configPath != "" {
		lines = append(lines, pad("Config", configPath))
	}

	// ── Endpoints ────────────────────────────────────────────
	lines = append(lines, "", sectionHeader("Endpoints"))
	lines = append(lines, sectionPad("Gateway", formatURL("http", s.GatewayAddr, "")))
	lines = append(lines, sectionPad("WebSocket", formatURL(wsScheme, s.GatewayAddr, "/ws")))
	lines = append(lines, sectionPad("Health", formatURL("http", s.GatewayAddr, "/health")))
	if s.WebChatEmbedded {
		lines = append(lines, sectionPad("WebChat", formatURL("http", s.GatewayAddr, "/")+" "+gray("(embedded)")))
		lines = append(lines, sectionPad("Admin UI", formatURL("http", s.GatewayAddr, "/admin")+" "+gray("(embedded)")))
	} else if s.WebChatAddr != "" {
		lines = append(lines, sectionPad("WebChat", formatURL("http", s.WebChatAddr, "")))
		lines = append(lines, sectionPad("Admin UI", formatURL("http", s.WebChatAddr, "/admin")))
	}
	lines = append(lines, sectionPad("Docs", formatURL("http", s.GatewayAddr, "/docs/")))
	lines = append(lines, sectionPad("API Console", formatURL("http", s.GatewayAddr, "/docs/reference/api-console.html")))
	if s.AdminAddr != "" {
		lines = append(lines, sectionPad("Admin API", formatURL("http", s.AdminAddr, "")))
	}

	// ── Bots ─────────────────────────────────────────────────
	if len(s.Adapters) > 0 {
		lines = append(lines, "", sectionHeader("Bots"))
		for _, a := range s.Adapters {
			name := a.Name
			if a.BotName != "" {
				name += "/" + a.BotName
			}
			icon := emerald("✓")
			if !a.Started {
				icon = rose("✗")
			}
			wt := a.WorkerType
			if a.WorkerDetail != "" {
				wt += "/" + a.WorkerDetail
			}
			lines = append(lines, fmt.Sprintf("    %-20s %s %s", name, icon, gray(wt)))
		}
	}

	// ── Resources ────────────────────────────────────────────
	lines = append(lines, "", sectionHeader("Resources"))
	if strings.EqualFold(s.DBDriver, "postgres") {
		lines = append(lines, sectionPad("Database", "PostgreSQL"))
	} else {
		lines = append(lines, sectionPad("Database", s.DBPath))
	}
	lines = append(lines, sectionPad("Pool", fmt.Sprintf("%d sessions / %d idle per user", s.PoolMax, s.PoolIdle)))
	if s.RetryEnabled {
		lines = append(lines, sectionPad("LLM Retry", emerald(fmt.Sprintf("✓ %d retries, %s delay", s.RetryMax, s.RetryDelay))))
	}

	lines = append(lines, "")
	writeAll(out, lines...)
}
