package loader

import (
	"fmt"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
)

// ValidateComposition walks all sub-workflows reachable from workflowFile and
// fails if the same named session is declared with different agent values
// across files. On success, every per-file workflow has also been loaded and
// validated. Sub-workflow paths containing unresolved `{{param}}` placeholders
// are skipped since their targets cannot be determined statically — such
// references are still checked per-file and at runtime when the sub-workflow
// is actually dispatched.
func ValidateComposition(workflowFile string) error {
	workflow, err := LoadWorkflow(workflowFile, Options{})
	if err != nil {
		return err
	}
	decls := map[string]sessionSource{}
	visited := map[string]bool{}
	return walkComposition(&workflow, workflowFile, decls, visited)
}

// sessionSource records the file path that first declared a given session name
// so that conflict errors can cite both source locations.
type sessionSource struct {
	agent  string
	source string
}

func walkComposition(w *model.Workflow, workflowFile string, decls map[string]sessionSource, visited map[string]bool) error {
	sourceID := SourceID(workflowFile)
	if visited[sourceID] {
		return nil
	}
	visited[sourceID] = true

	for _, decl := range w.Sessions {
		existing, ok := decls[decl.Name]
		if !ok {
			decls[decl.Name] = sessionSource{agent: decl.Agent, source: workflowFile}
			continue
		}
		if existing.agent != decl.Agent {
			return fmt.Errorf(
				"incompatible named session declaration %q: agent %q in %s, agent %q in %s",
				decl.Name, existing.agent, existing.source, decl.Agent, workflowFile,
			)
		}
	}

	return walkSubWorkflows(w.Steps, workflowFile, w.Scope, decls, visited)
}

func walkSubWorkflows(steps []model.Step, parentFile string, parentScope model.Scope, decls map[string]sessionSource, visited map[string]bool) error {
	for i := range steps {
		step := &steps[i]
		if step.Workflow != "" && !strings.Contains(step.Workflow, "{{") {
			subPath := ResolveRelativeWorkflowPath(parentFile, step.Workflow)
			subWorkflow, err := LoadWorkflow(subPath, Options{IsSubWorkflow: true})
			if err != nil {
				return fmt.Errorf("loading sub-workflow %s: %w", subPath, err)
			}
			if err := validateScopeComposition(parentScope, subWorkflow.Scope); err != nil {
				return fmt.Errorf("sub-workflow %s: %w", subPath, err)
			}
			if err := walkComposition(&subWorkflow, subPath, decls, visited); err != nil {
				return err
			}
		}
		if len(step.Steps) > 0 {
			if err := walkSubWorkflows(step.Steps, parentFile, parentScope, decls, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateScopeComposition validates only the relationship which cannot be
// expressed by a single workflow model: a repository body cannot call back
// into workspace work. Unscoped children deliberately retain legacy behavior.
func validateScopeComposition(parent, child model.Scope) error {
	if parent == model.ScopeRepositories && child == model.ScopeWorkspace {
		return fmt.Errorf("repository-scoped workflows cannot invoke a workspace-scoped child; invoke the workspace child from a workspace-scoped parent")
	}
	return nil
}
