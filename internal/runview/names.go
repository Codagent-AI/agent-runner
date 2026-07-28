package runview

import (
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/workflowcatalog"
)

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

func recordedWorkflowVersion(workflowFile string) string {
	definition, err := workflowcatalog.Parse(workflowFile)
	if err != nil {
		return "unversioned"
	}
	return definition.DisplayLabel
}
