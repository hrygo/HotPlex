package checkers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

func init() {
	cli.DefaultRegistry.Register(agentConfigDirChecker{})
	cli.DefaultRegistry.Register(agentConfigGlobalFilesChecker{})
}

// agentConfigDirChecker validates the agent-configs directory structure,
// ensuring platform subdirectories contain only recognized config files.
type agentConfigDirChecker struct {
	dir string // override for testing; defaults to config.HotplexHome()/agent-configs
}

func (c agentConfigDirChecker) Name() string     { return "agent.directory_structure" }
func (c agentConfigDirChecker) Category() string { return "agent_config" }

func (c agentConfigDirChecker) scanDir() string {
	if c.dir != "" {
		return c.dir
	}
	return filepath.Join(config.HotplexHome(), "agent-configs")
}

var validConfigFiles = map[string]bool{
	agentconfig.FileSoul:   true,
	agentconfig.FileAgents: true,
	agentconfig.FileTools:  true,
	agentconfig.FileUser:   true,
	agentconfig.FileMemory: true,
}

// ignoredFiles are non-config files allowed in any directory without warning.
var ignoredFiles = map[string]bool{
	".gitkeep": true, "README.md": true, ".DS_Store": true,
}

func (c agentConfigDirChecker) Check(_ context.Context) cli.Diagnostic {
	dir := c.scanDir()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Cannot read agent config directory: " + err.Error(),
		}
	}

	var warnings []string
	c.checkScope(dir, ".", entries, &warnings)

	if len(warnings) == 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Agent config directory structure is valid",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusWarn,
		Message:  fmt.Sprintf("Agent config issues: %s", strings.Join(warnings, "; ")),
		FixHint:  "Use only canonical names: SOUL.md, AGENTS.md, TOOLS.md, USER.md, MEMORY.md",
	}
}

func (c agentConfigDirChecker) checkScope(scopeDir, scope string, entries []os.DirEntry, warnings *[]string) {
	hasTools := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		hasTools = hasTools || name == agentconfig.FileTools
		if validConfigFiles[name] || ignoredFiles[name] {
			continue
		}
		if strings.HasSuffix(name, ".md") {
			*warnings = append(*warnings, relativeConfigPath(scope, name)+" is unrecognized")
		}
	}

	toolsPath := relativeConfigPath(scope, agentconfig.FileTools)
	if hasTools {
		empty, err := boundedFileEmpty(filepath.Join(scopeDir, agentconfig.FileTools))
		if err != nil {
			*warnings = append(*warnings, "cannot inspect "+toolsPath+": "+err.Error())
		} else if empty {
			*warnings = append(*warnings, toolsPath+" is present-empty and acts as an explicit clear")
		}
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		childScope := filepath.Join(scope, e.Name())
		childDir := filepath.Join(scopeDir, e.Name())
		childEntries, err := os.ReadDir(childDir)
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("cannot read %s config dir: %v", childScope, err))
			continue
		}
		c.checkScope(childDir, childScope, childEntries, warnings)
	}
}

func relativeConfigPath(scope, name string) string {
	if scope == "." {
		return name
	}
	return filepath.Join(scope, name)
}

func boundedFileEmpty(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return true, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, agentconfig.MaxFileChars+1))
	if err != nil {
		return false, err
	}
	return agentconfig.EffectiveContentEmpty(string(data)), nil
}

// agentConfigGlobalFilesChecker detects config files at the global level
// that lack a per-bot directory, meaning they are shared across all bots.
type agentConfigGlobalFilesChecker struct {
	dir string
}

func (c agentConfigGlobalFilesChecker) Name() string     { return "agent.global_files" }
func (c agentConfigGlobalFilesChecker) Category() string { return "agent_config" }

func (c agentConfigGlobalFilesChecker) scanDir() string {
	if c.dir != "" {
		return c.dir
	}
	return filepath.Join(config.HotplexHome(), "agent-configs")
}

func (c agentConfigGlobalFilesChecker) Check(_ context.Context) cli.Diagnostic {
	dir := c.scanDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return cli.Diagnostic{Name: c.Name(), Category: c.Category(), Status: cli.StatusPass, Message: "Agent config directory not yet created"}
		}
		return cli.Diagnostic{Name: c.Name(), Category: c.Category(), Status: cli.StatusWarn, Message: "Cannot read: " + err.Error()}
	}

	var global []string
	for _, e := range entries {
		if !e.IsDir() && validConfigFiles[e.Name()] {
			global = append(global, e.Name())
		}
	}
	if len(global) == 0 {
		return cli.Diagnostic{Name: c.Name(), Category: c.Category(), Status: cli.StatusPass, Message: "No global agent-config files (using per-bot configs)"}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusWarn,
		Message:  fmt.Sprintf("Global config files apply to all bots: %s", strings.Join(global, ", ")),
		Detail:   dir,
		FixHint:  fmt.Sprintf("Move to per-bot directory for isolation:\n  mkdir -p %s/slack/<botName>\n  mv %s %s/slack/<botName>/", dir, filepath.Join(dir, global[0]), dir),
	}
}
