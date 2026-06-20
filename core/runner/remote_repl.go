package runner

import (
	"context"
	"fmt"
	"io"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/remotepty"
	"github.com/chainreactors/aiscan/pkg/tui"
	rlterm "github.com/reeflective/readline/terminal"
)

func NewRemoteREPLOpener(rt *AgentRuntime, mgr *tmux.Manager) remotepty.OpenFunc {
	return func(ctx context.Context, spec remotepty.OpenSpec) (remotepty.OpenResult, error) {
		if rt == nil || rt.App == nil {
			return remotepty.OpenResult{}, fmt.Errorf("remote repl requires an agent runtime")
		}
		if mgr == nil {
			return remotepty.OpenResult{}, fmt.Errorf("pty manager unavailable")
		}
		option := rt.Option
		if option == nil {
			option = &cfg.Option{}
		}
		session := agent.NewAgent(rt.Config.
			WithSystemPrompt(rt.SystemPrompt).
			WithStream(tui.AgentStreamingEnabled(option)))
		appInfo := tui.AppInfo{
			Provider:          rt.App.Provider,
			ProviderConfig:    rt.App.ProviderConfig,
			ProviderFallbacks: rt.App.ProviderFallbacks,
			Commands:          rt.App.Commands,
			Skills:            rt.App.Skills,
			OnProviderChange: func(provider agent.Provider, providerConfig agent.ProviderConfig) {
				rt.App.Provider = provider
				rt.App.ProviderConfig = providerConfig
				rt.Config.Provider = provider
				rt.Config.Model = providerConfig.Model
			},
		}
		control := rlterm.NewControl(true, 80, 24)
		info, err := mgr.CreateInteractiveFunc(ctx, spec.Name, "aiscan remote repl", remotepty.DefaultSessionTimeout, false, func(replCtx context.Context, input io.Reader, output io.Writer) error {
			return tui.RunRemoteAgentConsoleWithControl(replCtx, option, appInfo, session, input, output, control, rt.Bus)
		})
		if err != nil {
			return remotepty.OpenResult{}, err
		}
		mgr.SetKind(info.ID, "repl")
		info.Kind = "repl"
		return remotepty.OpenResult{
			Info: info,
			Resize: func(cols, rows int) {
				control.SetSize(cols, rows)
			},
		}, nil
	}
}
