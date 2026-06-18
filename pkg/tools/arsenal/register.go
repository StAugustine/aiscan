package arsenal

import (
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

func init() {
	commands.RegisterFactory(commands.Factory{
		Group: "tools",
		Build: func(deps *commands.Deps, reg *commands.CommandRegistry) {
			logger, _ := deps.Logger.(telemetry.Logger)
			if logger == nil {
				logger = telemetry.NopLogger()
			}

			tool, err := NewArsenalTool(logger)
			if err != nil {
				logger.Warnf("arsenal init: %v", err)
				return
			}
			reg.RegisterTool(tool)
		},
	})
}
