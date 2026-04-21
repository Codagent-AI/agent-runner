package runview

import "github.com/codagent/agent-runner/internal/model"

// ResolverConfig is a type alias for model.ResolverConfig.
type ResolverConfig = model.ResolverConfig

// CanonicalName delegates to model.CanonicalName.
func CanonicalName(resolvedPath string, cfg ResolverConfig) string {
	return model.CanonicalName(resolvedPath, cfg)
}

// DiscoverWorkflowsRoot delegates to model.DiscoverWorkflowsRoot.
func DiscoverWorkflowsRoot(start string) (string, bool) {
	return model.DiscoverWorkflowsRoot(start)
}
