package skills

import (
	"errors"
	"strings"
)

// Invocation is a parsed invocation of a known Skill.
type Invocation struct {
	Name string
	Args string
}

// ErrAmbiguousInvocation indicates that the catalog contains duplicate names
// and cannot provide one canonical Skill to invoke.
var ErrAmbiguousInvocation = errors.New("skills: ambiguous invocation")

// ParseInvocation parses a slash-prefixed invocation against a known Skill
// catalog. It accepts the canonical form "/name args" and the legacy compact
// form "/nameargs" only when the name is present in the catalog. Matching is
// case-sensitive and chooses the longest known name.
func ParseInvocation(content string, catalog []Skill) (Invocation, bool, error) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "/") || len(content) == 1 {
		return Invocation{}, false, nil
	}

	input := content[1:]
	var best Invocation
	bestLen := -1
	seenNames := make(map[string]struct{}, len(catalog))
	for _, skill := range catalog {
		name := skill.Name
		if name == "" {
			continue
		}
		if _, seen := seenNames[name]; seen {
			if strings.HasPrefix(input, name) {
				return Invocation{Name: name, Args: strings.TrimSpace(input[len(name):])}, true, ErrAmbiguousInvocation
			}
			continue
		}
		seenNames[name] = struct{}{}

		if !strings.HasPrefix(input, name) {
			continue
		}
		remainder := input[len(name):]
		if remainder != "" && !isSpace(remainder[0]) {
			// Compact form: /skill-arg. The longest known name wins.
			if len(name) <= bestLen {
				continue
			}
		} else if len(name) < bestLen {
			continue
		}

		candidate := Invocation{Name: name, Args: strings.TrimSpace(remainder)}
		if len(name) == bestLen && candidate != best {
			return candidate, true, ErrAmbiguousInvocation
		}
		best = candidate
		bestLen = len(name)
	}

	if bestLen < 0 {
		return Invocation{}, false, nil
	}
	return best, true, nil
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
