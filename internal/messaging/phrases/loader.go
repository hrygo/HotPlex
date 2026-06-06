package phrases

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hrygo/hotplex/internal/agentconfig"
)

// ErrInvalidBotName is an alias for agentconfig.ErrInvalidBotName.
// Kept for backward compatibility; validation is delegated to agentconfig.ValidateBotName.
var ErrInvalidBotName = agentconfig.ErrInvalidBotName

// Load reads PHRASES.md from all levels with cascade-append:
//
//  1. dir/PHRASES.md (global, weight 2)
//  2. dir/{platform}/PHRASES.md (platform, weight 1)
//  3. dir/{platform}/{botName}/PHRASES.md (bot, weight 4)
//
// Each level's entries are appended to the pool, never replaced.
// Higher-level entries have higher selection weight in Random().
// Code defaults (weight 1) are only included as fallback when no
// external configuration exists for a given category.
// Missing directory or file is not an error — skips gracefully.
func Load(dir, platform, botName string) (*Phrases, error) {
	type loadLevel struct {
		path   string
		weight int
	}

	levels := []loadLevel{
		{filepath.Join(dir, "PHRASES.md"), WeightGlobal},
		{filepath.Join(dir, platform, "PHRASES.md"), WeightPlatform},
	}

	if botName != "" {
		if err := agentconfig.ValidateBotName(botName); err != nil {
			return nil, err
		}
		levels = append(levels, loadLevel{
			path:   filepath.Join(dir, platform, botName, "PHRASES.md"),
			weight: WeightBot,
		})
	} else if platform != "" {
		// Legacy backward compat: before PR #679, single-bot mode used "default"
		// as botName. If a user created phrases under {platform}/default/ between
		// #678 and #679, this fallback ensures they are still discovered.
		levels = append(levels, loadLevel{
			path:   filepath.Join(dir, platform, "default", "PHRASES.md"),
			weight: WeightBot,
		})
	}

	// Collect external entries by category.
	external := make(map[string][]entry)
	for _, lvl := range levels {
		data, err := os.ReadFile(lvl.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("phrases: read %s: %w", lvl.path, err)
		}
		parsed := parseMarkdown(string(data))
		for k, vals := range parsed {
			for _, v := range vals {
				external[k] = append(external[k], entry{text: v, weight: lvl.weight})
			}
		}
	}

	// Build final entries: external overrides defaults per-category.
	// If a category has any external entries, defaults are excluded.
	defaults := Defaults()
	merged := make(map[string][]entry)
	for cat, defEntries := range defaults.entries {
		if ext, ok := external[cat]; ok && len(ext) > 0 {
			merged[cat] = ext
		} else {
			merged[cat] = defEntries
		}
	}
	// Add external categories not present in defaults.
	for cat, ext := range external {
		if _, ok := merged[cat]; !ok {
			merged[cat] = ext
		}
	}

	return &Phrases{entries: merged}, nil
}
