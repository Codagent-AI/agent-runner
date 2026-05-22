package native

import "github.com/codagent/agent-runner/internal/agentplugin"

// DefaultPluginInstaller delegates to the agentplugin package functions.
type DefaultPluginInstaller struct{}

func (DefaultPluginInstaller) Resolve(clis []string, scope string) (*agentplugin.Plan, error) {
	return agentplugin.Resolve(&agentplugin.Request{CLIs: clis, Scope: scope})
}

func (DefaultPluginInstaller) DryRun(plan *agentplugin.Plan) (*agentplugin.Preview, error) {
	return agentplugin.DryRun(plan)
}

func (DefaultPluginInstaller) Install(plan *agentplugin.Plan) (*agentplugin.Result, error) {
	return agentplugin.Install(plan)
}
