package checkers

import (
	"context"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/internal/agentspec"
	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

// effectivePlanChecker probes the desired-state EffectiveRuntimePlan the
// current config would produce for each config-driven (messaging) platform
// (#946 spec §6.6). It uses the shared agentspec resolver against the local
// config only — strictly read-only, no DB, no network, no config mutation.
// Request-driven platforms (webchat) are not probed: their plan depends on
// per-request metadata a local doctor run cannot know.
type effectivePlanChecker struct{}

func (c effectivePlanChecker) Name() string     { return "runtime.effective_plan" }
func (c effectivePlanChecker) Category() string { return "runtime" }

// planProbeResult is one platform's probe outcome.
type planProbeResult struct {
	platform string
	hash     string
	worker   string
	perm     string
	sandbox  string
	warnings []string
	blocked  []string
}

// messagingProbePlatforms are the config-driven platforms whose worker chain
// the local config fully determines (the documented 5-level fallback).
var messagingProbePlatforms = []string{"slack", "feishu", "yuanxin"}

func (c effectivePlanChecker) Check(ctx context.Context) cli.Diagnostic {
	cfg, err := loadConfig()
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Cannot load config for runtime plan probe",
			Detail:   err.Error(),
			FixHint:  "Fix the config first (hotplex config validate), then re-run doctor",
		}
	}
	if cfg == nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "No config loaded; runtime plan probe skipped",
		}
	}

	probes := probeEffectivePlans(ctx, cfg)
	lines := make([]string, 0, len(probes))
	var blockedPlatforms []string
	for _, p := range probes {
		if len(p.blocked) > 0 && p.hash == "" {
			// Fully blocked before any field resolved: the codes are the payload.
			lines = append(lines, fmt.Sprintf("%s: BLOCKED=%s", p.platform, strings.Join(p.blocked, ",")))
			blockedPlatforms = append(blockedPlatforms, p.platform)
			continue
		}
		line := fmt.Sprintf("%s: plan=%s worker=%s permission=%s sandbox=%s",
			p.platform, p.hash, p.worker, displayOr(p.perm, "default"), displayOr(p.sandbox, "-"))
		if len(p.warnings) > 0 {
			line += " warnings=" + strings.Join(p.warnings, ",")
		}
		if len(p.blocked) > 0 {
			line += " BLOCKED=" + strings.Join(p.blocked, ",")
			blockedPlatforms = append(blockedPlatforms, p.platform)
		}
		lines = append(lines, line)
	}

	if len(blockedPlatforms) > 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  fmt.Sprintf("Effective runtime plan blocked on %d platform(s)", len(blockedPlatforms)),
			Detail:   strings.Join(lines, "\n"),
			FixHint: "Fix the blocked config values (worker_type / permission mode / sandbox mode) " +
				"listed above, then restart the gateway — a blocked plan is never a silent success",
		}
	}
	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  fmt.Sprintf("Effective runtime plans resolved for %d platform(s)", len(probes)),
		Detail:   strings.Join(lines, "\n"),
	}
}

// probeEffectivePlans resolves one desired-state plan per messaging platform
// using the shared resolver. Pure given cfg — the checker and its tests call
// this directly. A probe that resolves nil config never happens here (Check
// guards it). The resolver is the zero value: plan validation uses the live
// worker registry, so doctor sees the same boundary the gateway sees.
func probeEffectivePlans(ctx context.Context, cfg *config.Config) []planProbeResult {
	results := make([]planProbeResult, 0, len(messagingProbePlatforms))
	for _, platform := range messagingProbePlatforms {
		if ctx.Err() != nil {
			break
		}
		plan, err := (agentspec.Resolver{}).ResolvePlan(agentspec.Input{Cfg: cfg, Platform: platform})
		res := planProbeResult{platform: platform}
		if err != nil {
			for _, b := range plan.Blocked {
				res.blocked = append(res.blocked, b.Code)
			}
			results = append(results, res)
			continue
		}
		view := plan.Redacted()
		res.hash = plan.PlanHash
		res.worker = displayOr(view.WorkerType, "unresolved")
		res.perm = view.PermissionMode
		res.sandbox = view.SandboxMode
		for _, w := range plan.Warnings {
			res.warnings = append(res.warnings, w.Code)
		}
		results = append(results, res)
	}
	return results
}

func displayOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func init() {
	cli.DefaultRegistry.Register(effectivePlanChecker{})
}
