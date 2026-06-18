package main

import (
	"context"
	"os"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/core/resources"
	"github.com/chainreactors/aiscan/core/runner"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/scan"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	"github.com/chainreactors/aiscan/skills"
)

func init() {
	runner.ScannerInitFunc = scannerInit
}

func scannerInit(ctx context.Context, app *runner.App, rc cfg.RuntimeConfig, logger telemetry.Logger) {
	engineSet := initEngines(ctx, rc.Scanner, logger)
	app.Engines = engineSet
	registerScannerCommands(app.Commands, engineSet, rc.Scanner, rc.Tools, app.Provider, app.ProviderConfig.Model, app.Skills, logger)
}

func initEngines(ctx context.Context, sc cfg.ScannerConfig, logger telemetry.Logger) *engine.Set {
	engineSet, err := engine.InitWithOptions(ctx, resources.Options{
		CyberhubURL: sc.CyberhubURL,
		APIKey:      sc.CyberhubKey,
		Mode:        sc.CyberhubMode,
		Proxy:       sc.Proxy,
	}, logger)
	if err != nil {
		logger.Warnf("scanner engines init error=%q action=continue_without_scanners", err)
		return nil
	}
	engineSet.SetupUncover(engine.ReconOptions{
		FofaEmail:    sc.FofaEmail,
		FofaKey:      sc.FofaKey,
		HunterToken:  sc.HunterToken,
		HunterAPIKey: sc.HunterAPIKey,
		IngressProxy: sc.ReconProxy,
		Limit:        sc.ReconLimit,
	}, logger)
	return engineSet
}

func registerScannerCommands(cmdReg *commands.CommandRegistry, engineSet *engine.Set, scanCfg cfg.ScannerConfig, toolCfg cfg.ToolConfig, llmProvider agent.Provider, model string, skillStore *skills.Store, logger telemetry.Logger) {
	var scanOpts []any
	if scanCfg.AIEnabled && llmProvider != nil {
		scanOpts = append(scanOpts, scan.WithParent(agent.NewAgent(agent.Config{
			Provider: llmProvider,
			Tools:    cmdReg,
			Model:    model,
			Logger:   logger,
		})))
		scanOpts = append(scanOpts, scan.WithDeepBrowserFunc(func(ctx context.Context, targetURL string) (string, error) {
			return runner.CollectDeepBrowserArtifacts(ctx, cmdReg, targetURL, logger)
		}))
		if skillStore != nil {
			scanOpts = append(scanOpts, scan.WithSkillReader(func(name string) string {
				content, ok, err := skillStore.ReadVirtual("aiscan://skills/scan/" + name + ".md")
				if !ok || err != nil {
					return ""
				}
				return content
			}))
		}
	}
	scanOpts = append(scanOpts, scan.WithLogger(logger))

	workDir, _ := os.Getwd()
	deps := &commands.Deps{
		WorkDir:      workDir,
		BashTimeout:  toolCfg.BashTimeout,
		SkillStore:   skillStore,
		EngineSet:    engineSet,
		ScannerProxy: scanCfg.Proxy,
		ScanOpts:     scanOpts,
		Logger:       logger,
		Model:        model,
		TavilyKeys:   toolCfg.TavilyKeys,
	}
	if engineSet != nil {
		deps.Resources = engineSet.Resources
	}
	commands.BuildGroup("scanner", deps, cmdReg)
	commands.BuildGroup("proxy", deps, cmdReg)
	commands.BuildGroup("ioa", deps, cmdReg)
	logger.Infof("scanner commands ready: %v", cmdReg.GroupNames("scanner"))
}
