package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const internalCLISurfaceFlag = "--internal-generate-cli-surface"

func runInternalCLISurface(args []string) (bool, error) {
	if len(args) == 0 || args[0] != internalCLISurfaceFlag {
		return false, nil
	}

	flags := flag.NewFlagSet("internal-generate-cli-surface", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	outputPath := flags.String("output", "-", "output path")
	if err := flags.Parse(args[1:]); err != nil {
		return true, err
	}
	rendered, err := renderPublicCLISurface(newRootCmd())
	if err != nil {
		return true, err
	}
	if *outputPath == "-" {
		_, err = os.Stdout.Write(rendered)
		return true, err
	}
	if err := os.WriteFile(*outputPath, rendered, 0o644); err != nil {
		return true, fmt.Errorf("write CLI surface %q: %w", *outputPath, err)
	}
	return true, nil
}

func renderPublicCLISurface(root *cobra.Command) ([]byte, error) {
	if root == nil {
		return nil, fmt.Errorf("nil root command")
	}

	commands := make([]*cobra.Command, 0)
	walkPublicCommands(root, &commands)
	sort.Slice(commands, func(i, j int) bool {
		return commandPath(commands[i]) < commandPath(commands[j])
	})

	var out strings.Builder
	out.WriteString("# Public HotPlex CLI surface\n\n")
	out.WriteString("Generated from the public Cobra command tree. Use installed command help as the final authority for syntax, defaults, and availability.\n\n")
	for _, command := range commands {
		out.WriteString("## ")
		out.WriteString(commandPath(command))
		out.WriteByte('\n')
		if purpose := publicPurpose(command); purpose != "" {
			out.WriteString(purpose)
			out.WriteByte('\n')
		}
		if aliases := publicAliases(command); len(aliases) > 0 {
			out.WriteString("Aliases: ")
			out.WriteString(strings.Join(aliases, " "))
			out.WriteByte('\n')
		}
		flags := publicFlags(command)
		if len(flags) == 0 {
			out.WriteString("\n")
			continue
		}
		out.WriteString("\nOptions: ")
		for i, flag := range flags {
			if i > 0 {
				out.WriteByte(' ')
			}
			out.WriteString(flagShape(flag))
		}
		out.WriteString("\n\n")
	}
	return []byte(strings.TrimRight(out.String(), "\n") + "\n"), nil
}

func publicPurpose(command *cobra.Command) string {
	if command == nil {
		return ""
	}
	purpose := strings.TrimSpace(strings.ReplaceAll(command.Short, "\n", " "))
	if purpose == "" || strings.ContainsAny(purpose, "/\\") || strings.Contains(purpose, "~") {
		return ""
	}
	upper := strings.ToUpper(purpose)
	for _, fragment := range []string{"GATEWAY_", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL"} {
		if strings.Contains(upper, fragment) {
			return ""
		}
	}
	return purpose
}

func publicAliases(command *cobra.Command) []string {
	if command == nil {
		return nil
	}
	aliases := make([]string, 0, len(command.Aliases))
	for _, alias := range command.Aliases {
		if alias == "" || strings.ContainsAny(alias, "/\\") || strings.Contains(alias, " ") {
			continue
		}
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

func flagShape(flag *pflag.Flag) string {
	if flag == nil {
		return ""
	}
	if flag.NoOptDefVal != "" {
		return "--" + flag.Name
	}
	valueType := strings.TrimSpace(flag.Value.Type())
	if valueType == "" {
		return "--" + flag.Name
	}
	return "--" + flag.Name + " <" + valueType + ">"
}

func walkPublicCommands(command *cobra.Command, commands *[]*cobra.Command) {
	if command == nil || command.Hidden {
		return
	}
	*commands = append(*commands, command)
	children := append([]*cobra.Command(nil), command.Commands()...)
	sort.Slice(children, func(i, j int) bool {
		return commandUse(children[i]) < commandUse(children[j])
	})
	for _, child := range children {
		walkPublicCommands(child, commands)
	}
}

func publicFlags(command *cobra.Command) []*pflag.Flag {
	byName := make(map[string]*pflag.Flag)
	collect := func(set *pflag.FlagSet) {
		if set == nil {
			return
		}
		set.VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden {
				return
			}
			byName[flag.Name] = flag
		})
	}
	collect(command.LocalNonPersistentFlags())
	collect(command.PersistentFlags())
	collect(command.InheritedFlags())

	flags := make([]*pflag.Flag, 0, len(byName))
	for _, flag := range byName {
		flags = append(flags, flag)
	}
	sort.Slice(flags, func(i, j int) bool {
		return flags[i].Name < flags[j].Name
	})
	return flags
}

func commandPath(command *cobra.Command) string {
	parts := make([]string, 0, 4)
	for current := command; current != nil; current = current.Parent() {
		parts = append(parts, commandUse(current))
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return strings.Join(parts, " ")
}

func commandUse(command *cobra.Command) string {
	if command == nil {
		return ""
	}
	fields := strings.Fields(command.Use)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
