package loader

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/validate"
	"github.com/codagent/agent-runner/internal/workflowcatalog"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

// Options controls workflow loading behavior.
type Options struct {
	IsSubWorkflow bool
}

// LoadWorkflow reads a YAML file and returns a validated Workflow.
func LoadWorkflow(filePath string, opts Options) (model.Workflow, error) {
	if _, err := workflowcatalog.Parse(filePath); err != nil {
		return model.Workflow{}, err
	}

	data, err := ReadWorkflowFile(filePath)
	if err != nil {
		return model.Workflow{}, fmt.Errorf("cannot read workflow file: %w", err)
	}

	return ParseWorkflowSource(data, filePath, opts)
}

// ParseWorkflow parses workflow YAML bytes and returns a validated Workflow.
func ParseWorkflow(data []byte, opts Options) (model.Workflow, error) {
	var w model.Workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return model.Workflow{}, fmt.Errorf("invalid YAML: %w", err)
	}

	w.ApplyDefaults()

	if err := w.Validate(cli.KnownCLIs()); err != nil {
		return model.Workflow{}, err
	}

	if err := validate.WorkflowConstraints(&w, validate.Options{
		IsSubWorkflow: opts.IsSubWorkflow,
	}); err != nil {
		return model.Workflow{}, err
	}

	return w, nil
}

// ParseWorkflowSource parses workflow YAML bytes and validates them against
// their exact source filename.
func ParseWorkflowSource(data []byte, sourcePath string, opts Options) (model.Workflow, error) {
	definition, err := workflowcatalog.Parse(sourcePath)
	if err != nil {
		return model.Workflow{}, err
	}

	workflow, err := ParseWorkflow(data, opts)
	if err != nil {
		return model.Workflow{}, err
	}
	if workflow.Name != definition.LogicalName {
		return model.Workflow{}, fmt.Errorf(
			"workflow name does not match source filename: expected %q, got %q",
			definition.LogicalName,
			workflow.Name,
		)
	}
	if workflowUsesSubmitRoute(&workflow) && (opts.IsSubWorkflow || sourcePath != builtinworkflows.Ref("core/intake-v1.0.yaml") || !isIntakeRouteStep(&workflow)) {
		return model.Workflow{}, fmt.Errorf("submit_route is reserved for the top-level built-in core:intake plan step")
	}
	return workflow, nil
}

func workflowUsesSubmitRoute(workflow *model.Workflow) bool {
	if workflow == nil {
		return false
	}
	for index := range workflow.Steps {
		if workflow.Steps[index].HasTool(model.RunnerToolSubmitRoute) {
			return true
		}
	}
	return false
}

func isIntakeRouteStep(workflow *model.Workflow) bool {
	if workflow == nil || len(workflow.Steps) != 1 {
		return false
	}
	step := workflow.Steps[0]
	return step.ID == "plan" && step.HasTool(model.RunnerToolSubmitRoute)
}

func ReadWorkflowFile(filePath string) ([]byte, error) {
	if builtinworkflows.IsRef(filePath) {
		return builtinworkflows.ReadFile(filePath)
	}
	return os.ReadFile(filePath) // #nosec G304 -- workflow file path is user-specified CLI input
}

func ResolveRelativeWorkflowPath(parentFile, workflowField string) string {
	if parentFile == "" {
		return workflowField
	}
	if builtinworkflows.IsRef(parentFile) {
		relParent, err := builtinworkflows.RefPath(parentFile)
		if err != nil {
			return workflowField
		}
		return builtinworkflows.Ref(path.Join(path.Dir(relParent), workflowField))
	}
	return filepath.Join(filepath.Dir(parentFile), workflowField)
}

func SourceID(filePath string) string {
	if builtinworkflows.IsRef(filePath) {
		return filePath
	}
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return filepath.Clean(filePath)
	}
	return abs
}

var filePlaceholderRe = regexp.MustCompile(`\{\{file:(\w+)\}\}`)
var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// InterpolateParams replaces {{paramName}} and {{file:paramName}} placeholders.
func InterpolateParams(template string, params map[string]string) (string, error) {
	// First pass: replace {{file:paramName}} with sentinel tokens.
	var fileContents []string
	var firstErr error

	result := filePlaceholderRe.ReplaceAllStringFunc(template, func(match string) string {
		if firstErr != nil {
			return match
		}
		key := filePlaceholderRe.FindStringSubmatch(match)[1]
		filePath, ok := params[key]
		if !ok {
			firstErr = fmt.Errorf("missing parameter: {{file:%s}}", key)
			return match
		}
		content, err := os.ReadFile(filePath) // #nosec G304 -- file: param reference resolved from workflow
		if err != nil {
			firstErr = fmt.Errorf("cannot read file for parameter {{file:%s}}: %q", key, filePath)
			return match
		}
		block := strings.Join([]string{
			"The following file was provided as context for this step. Use it to inform your work:",
			"",
			fmt.Sprintf(`<file path=%q>`, filePath),
			strings.TrimSpace(string(content)),
			"</file>",
		}, "\n")
		idx := len(fileContents)
		fileContents = append(fileContents, block)
		return fmt.Sprintf("\x00FILE_SENTINEL_%d\x00", idx)
	})

	if firstErr != nil {
		return "", firstErr
	}

	// Second pass: resolve {{paramName}}.
	result = placeholderRe.ReplaceAllStringFunc(result, func(match string) string {
		if firstErr != nil {
			return match
		}
		key := placeholderRe.FindStringSubmatch(match)[1]
		value, ok := params[key]
		if !ok {
			firstErr = fmt.Errorf("missing parameter: {{%s}}", key)
			return match
		}
		return value
	})

	if firstErr != nil {
		return "", firstErr
	}

	// Third pass: replace sentinels with file contents.
	for i, content := range fileContents {
		result = strings.Replace(result, fmt.Sprintf("\x00FILE_SENTINEL_%d\x00", i), content, 1)
	}

	return result, nil
}
