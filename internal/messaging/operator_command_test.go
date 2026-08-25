package messaging

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGatewayCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		reserved bool
		kind     GatewayCommandKind
	}{
		{name: "exact command", input: "/gateway restart", reserved: true, kind: GatewayCommandRestart},
		{name: "trimmed and case insensitive", input: "  /GATEWAY RESTART  ", reserved: true, kind: GatewayCommandRestart},
		{name: "extra argument is reserved help", input: "/gateway restart now", reserved: true, kind: GatewayCommandHelp},
		{name: "unknown subcommand is reserved help", input: "/gateway status", reserved: true, kind: GatewayCommandHelp},
		{name: "bare namespace is reserved help", input: "/gateway", reserved: true, kind: GatewayCommandHelp},
		{name: "slash variant stays reserved", input: "/gateway/restart", reserved: true, kind: GatewayCommandHelp},
		{name: "adjacent prefix stays reserved", input: "/gatewayx restart", reserved: true, kind: GatewayCommandHelp},
		{name: "mixed case adjacent prefix stays reserved", input: "/GaTeWaY.Status", reserved: true, kind: GatewayCommandHelp},
		{name: "natural language is not reserved", input: "请重启 Gateway", reserved: false},
		{name: "fenced markdown is not a command", input: "```\n/gateway restart\n```", reserved: false},
		{name: "worker output is not a command", input: "worker says /gateway restart", reserved: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reserved := ParseGatewayCommand(tt.input)
			require.Equal(t, tt.reserved, reserved)
			if reserved {
				require.Equal(t, tt.kind, got.Kind)
				return
			}
			require.Equal(t, GatewayCommandKind(""), got.Kind)
		})
	}
}
