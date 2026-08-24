package messaging

import (
	"context"
	"strings"
)

// GatewayCommandKind identifies a command in the reserved /gateway namespace.
type GatewayCommandKind string

const (
	GatewayCommandRestart GatewayCommandKind = "restart"
	GatewayCommandHelp    GatewayCommandKind = "help"
)

// GatewayCommand is the result of parsing a reserved Gateway command.
type GatewayCommand struct {
	Kind GatewayCommandKind
}

// GatewayRestartRequest carries only the platform context needed by the
// Gateway-owned restart controller. Reply is supplied by the adapter so the
// controller does not depend on a concrete messaging platform.
type GatewayRestartRequest struct {
	ActorID        string
	BotName        string
	ChatType       string
	ChatID         string
	ThreadKey      string
	MessageID      string
	ReplyToMessage string
	PlatformKey    map[string]string
	Reply          func(context.Context, string) error
}

// GatewayCommandHandler handles reserved Gateway commands without creating a
// session or invoking a Worker.
type GatewayCommandHandler interface {
	HandleGatewayCommand(context.Context, GatewayCommand, GatewayRestartRequest) error
}

// ParseGatewayCommand parses the reserved /gateway namespace. The boolean is
// true for every user input whose first token is /gateway, including malformed
// subcommands, so callers can never fall back to Worker processing.
//
// Markdown fenced blocks are not commands. They remain ordinary user text and
// cannot trigger a host operation even when their contents look like a command.
func ParseGatewayCommand(text string) (GatewayCommand, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "```") {
		return GatewayCommand{}, false
	}

	parts := strings.Fields(trimmed)
	if len(parts) == 0 || !strings.EqualFold(parts[0], "/gateway") {
		return GatewayCommand{}, false
	}
	if len(parts) == 2 && strings.EqualFold(parts[1], "restart") {
		return GatewayCommand{Kind: GatewayCommandRestart}, true
	}
	return GatewayCommand{Kind: GatewayCommandHelp}, true
}
