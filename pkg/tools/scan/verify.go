package scan

import (
	"context"
	"fmt"
	"strings"

	"github.com/chainreactors/aiscan/core/output"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

type indexedLoot struct {
	index int
	loot  output.Loot
}

func runVerifyPass(ctx context.Context, parent *agent.Agent, readSkill SkillReader, coll *collector, level priority, logger telemetry.Logger) {
	if readSkill == nil {
		return
	}
	skillPrompt := readSkill("verify")
	if skillPrompt == "" {
		logger.Debugf("verify pass: skill content not available, skipping")
		return
	}

	coll.mu.Lock()
	candidates := filterLootsByPriority(coll.loots, level)
	coll.mu.Unlock()

	if len(candidates) == 0 {
		logger.Debugf("verify pass: no loots at or above %s", level)
		return
	}

	logger.Infof("verify pass: %d candidates at or above %s", len(candidates), level)

	for _, c := range candidates {
		if ctx.Err() != nil {
			break
		}
		result := runVerifyAgent(ctx, parent, skillPrompt, c.loot, logger)
		if result != nil {
			coll.mu.Lock()
			annotateLoot(&coll.loots[c.index], result.Status)
			coll.mu.Unlock()
			logger.Infof("verify: %s → %s", c.loot.Description, result.Status)
		}
	}
}

func runSniperPass(ctx context.Context, parent *agent.Agent, readSkill SkillReader, coll *collector, logger telemetry.Logger) {
	if readSkill == nil {
		return
	}
	skillPrompt := readSkill("sniper")
	if skillPrompt == "" {
		logger.Debugf("sniper pass: skill content not available, skipping")
		return
	}

	coll.mu.Lock()
	candidates := filterFingerprintLoots(coll.loots)
	coll.mu.Unlock()

	if len(candidates) == 0 {
		logger.Debugf("sniper pass: no fingerprint loots")
		return
	}

	logger.Infof("sniper pass: %d fingerprint candidates", len(candidates))

	for _, c := range candidates {
		if ctx.Err() != nil {
			break
		}
		result := runSniperAgent(ctx, parent, skillPrompt, c.loot, logger)
		if result != nil {
			coll.mu.Lock()
			annotateLoot(&coll.loots[c.index], result.Status)
			coll.mu.Unlock()
			logger.Infof("sniper: %s → %s", c.loot.Description, result.Status)
		}
	}
}

func runVerifyAgent(ctx context.Context, parent *agent.Agent, skillPrompt string, loot output.Loot, logger telemetry.Logger) *commands.CheckpointResult {
	sub := parent.Derive()
	checkpoint := commands.NewCheckpointTool()
	tools := sub.Cfg.Tools.CloneTools()
	tools.RegisterTool(checkpoint)
	sub.Cfg = sub.Cfg.WithTools(tools).WithSystemPrompt(skillPrompt).WithStream(false)

	prompt := formatVerifyPrompt(loot)
	_, err := sub.Run(ctx, prompt)
	if err != nil {
		logger.Debugf("verify agent error: %s", err)
		return nil
	}
	return checkpoint.Result()
}

func runSniperAgent(ctx context.Context, parent *agent.Agent, skillPrompt string, loot output.Loot, logger telemetry.Logger) *commands.CheckpointResult {
	sub := parent.Derive()
	checkpoint := commands.NewCheckpointTool()
	tools := sub.Cfg.Tools.CloneTools()
	tools.RegisterTool(checkpoint)
	sub.Cfg = sub.Cfg.WithTools(tools).WithSystemPrompt(skillPrompt).WithStream(false)

	prompt := formatSniperPrompt(loot)
	_, err := sub.Run(ctx, prompt)
	if err != nil {
		logger.Debugf("sniper agent error: %s", err)
		return nil
	}
	return checkpoint.Result()
}

func filterLootsByPriority(loots []output.Loot, min priority) []indexedLoot {
	var out []indexedLoot
	for i, l := range loots {
		if priority(l.Priority).atLeast(min) {
			out = append(out, indexedLoot{index: i, loot: l})
		}
	}
	return out
}

func filterFingerprintLoots(loots []output.Loot) []indexedLoot {
	var out []indexedLoot
	for i, l := range loots {
		if l.Kind == output.LootFingerprint {
			focus, _ := l.Data["focus"].(bool)
			if focus {
				out = append(out, indexedLoot{index: i, loot: l})
			}
		}
	}
	return out
}

func annotateLoot(loot *output.Loot, status string) {
	if loot.Data == nil {
		loot.Data = make(map[string]any)
	}
	loot.Data["verification_status"] = commands.NormalizeStatus(status)
}

func formatVerifyPrompt(loot output.Loot) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Verify this loot on target %s:\n\n", loot.Target))
	sb.WriteString(fmt.Sprintf("- Kind: %s\n", loot.Kind))
	sb.WriteString(fmt.Sprintf("- Priority: %s\n", loot.Priority))
	sb.WriteString(fmt.Sprintf("- Description: %s\n", loot.Description))
	if sev, ok := loot.Data["severity"].(string); ok {
		sb.WriteString(fmt.Sprintf("- Severity: %s\n", sev))
	}
	if tid, ok := loot.Data["template_id"].(string); ok {
		sb.WriteString(fmt.Sprintf("- Template: %s\n", tid))
	}
	if svc, ok := loot.Data["service"].(string); ok {
		sb.WriteString(fmt.Sprintf("- Service: %s\n", svc))
	}
	return sb.String()
}

func formatSniperPrompt(loot output.Loot) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Analyze fingerprint on target %s:\n\n", loot.Target))
	sb.WriteString(fmt.Sprintf("- Fingerprints: %s\n", loot.Description))
	if fingers, ok := loot.Data["fingers"].([]string); ok {
		sb.WriteString(fmt.Sprintf("- Names: %s\n", strings.Join(fingers, ", ")))
	}
	return sb.String()
}
