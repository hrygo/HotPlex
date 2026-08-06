package gateway

import (
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
)

func resolveSkillInvocation(content string, catalog []skills.Skill) (worker.SkillInvocation, bool, error) {
	parsed, matched, err := skills.ParseInvocation(content, catalog)
	if err != nil || !matched {
		return worker.SkillInvocation{}, matched, err
	}
	for _, skill := range catalog {
		if skill.Name == parsed.Name {
			return worker.SkillInvocation{
				Name: parsed.Name,
				Args: parsed.Args,
				Path: skill.FilePath,
			}, true, nil
		}
	}
	return worker.SkillInvocation{}, false, nil
}
