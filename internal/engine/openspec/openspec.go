package openspec

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/model"
)

type artifact struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type statusOutput struct {
	ChangeName string     `json:"changeName"`
	ChangeDir  string     `json:"changeDir"`
	Artifacts  []artifact `json:"artifacts"`
}

type dependency struct {
	Path        string `json:"path"`
	Description string `json:"description"`
}

type instructionsOutput struct {
	ArtifactID   string       `json:"artifactId"`
	SchemaName   string       `json:"schemaName"`
	Instruction  string       `json:"instruction"`
	OutputPath   string       `json:"outputPath"`
	Template     string       `json:"template"`
	Dependencies []dependency `json:"dependencies"`
	ChangeDir    string       `json:"changeDir"`
}

// CmdRunner abstracts running openspec CLI commands for testability.
type CmdRunner interface {
	Run(args []string) (string, error)
}

// realCmdRunner runs openspec via os/exec.
type realCmdRunner struct{}

func (r *realCmdRunner) Run(args []string) (string, error) {
	cmd := exec.Command("openspec", args...) // #nosec G204 -- openspec CLI is invoked with controlled arguments
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				return "", fmt.Errorf("%s", stderr)
			}
			return "", fmt.Errorf("openspec command failed with exit code %d", exitErr.ExitCode())
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

type openSpecEngine struct {
	changeParam string
	cmdRunner   CmdRunner
	artifactIDs map[string]bool
}

func getChangeName(changeParam string, params map[string]string) (string, error) {
	name, ok := params[changeParam]
	if !ok || name == "" {
		return "", fmt.Errorf("missing required param %q for openspec engine", changeParam)
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid change name %q: must not contain path separators or traversal", name)
	}
	return name, nil
}

func (e *openSpecEngine) loadArtifactIDs(changeName string) (map[string]bool, error) {
	raw, err := e.cmdRunner.Run([]string{"status", "--change", changeName, "--json"})
	if err != nil {
		return nil, err
	}
	var status statusOutput
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return nil, fmt.Errorf("parse openspec status: %w", err)
	}
	ids := make(map[string]bool)
	for _, a := range status.Artifacts {
		ids[a.ID] = true
	}
	return ids, nil
}

func (e *openSpecEngine) ensureArtifactIDs(changeName string) (map[string]bool, error) {
	if e.artifactIDs == nil {
		ids, err := e.loadArtifactIDs(changeName)
		if err != nil {
			return nil, err
		}
		e.artifactIDs = ids
	}
	return e.artifactIDs, nil
}

func (e *openSpecEngine) ValidateWorkflow(workflow *model.Workflow, params map[string]string, _ string) error {
	changeName, err := getChangeName(e.changeParam, params)
	if err != nil {
		return err
	}

	ids, err := e.loadArtifactIDs(changeName)
	if err != nil {
		// Change doesn't exist yet — skip validation
		return nil
	}
	e.artifactIDs = ids

	stepIDs := make(map[string]bool)
	hasSubWorkflows := false
	for i := range workflow.Steps {
		stepIDs[workflow.Steps[i].ID] = true
		if workflow.Steps[i].Workflow != "" {
			hasSubWorkflows = true
		}
	}

	var unmatched []string
	for id := range ids {
		if !stepIDs[id] {
			unmatched = append(unmatched, id)
		}
	}

	// If the workflow delegates to sub-workflows, unmatched artifacts may
	// live in those sub-workflows — skip the error here and let each
	// sub-workflow validate its own steps.
	if len(unmatched) > 0 && !hasSubWorkflows {
		return fmt.Errorf("workflow is missing steps for openspec artifacts: %s", strings.Join(unmatched, ", "))
	}

	return nil
}

func (e *openSpecEngine) NeedsDeferredValidation() bool {
	return e.artifactIDs == nil
}

func (e *openSpecEngine) EnrichPrompt(stepID string, params map[string]string, opts engine.EnrichOptions) string {
	changeName, err := getChangeName(e.changeParam, params)
	if err != nil {
		return ""
	}
	ids, err := e.ensureArtifactIDs(changeName)
	if err != nil || !ids[stepID] {
		return ""
	}

	raw, err := e.cmdRunner.Run([]string{"instructions", stepID, "--change", changeName, "--json"})
	if err != nil {
		return ""
	}

	var data instructionsOutput
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return ""
	}

	return buildEnrichmentBlock(&data, opts.SessionStrategy)
}

func (e *openSpecEngine) ValidateStep(stepID string, params map[string]string) (bool, error) {
	changeName, err := getChangeName(e.changeParam, params)
	if err != nil {
		return false, err
	}
	ids, err := e.ensureArtifactIDs(changeName)
	if err != nil {
		return false, err
	}
	if !ids[stepID] {
		return true, nil
	}

	raw, err := e.cmdRunner.Run([]string{"status", "--change", changeName, "--json"})
	if err != nil {
		return false, err
	}
	var status statusOutput
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return false, err
	}
	for _, a := range status.Artifacts {
		if a.ID == stepID {
			return a.Status == "done", nil
		}
	}
	return false, nil
}

func buildEnrichmentBlock(data *instructionsOutput, sessionStrategy string) string {
	outputPath := filepath.Join(data.ChangeDir, data.OutputPath)
	templatePath := filepath.Join(data.ChangeDir, "..", "..", "schemas", data.SchemaName, "templates", data.ArtifactID+".md")
	isResumed := sessionStrategy == "resume" || sessionStrategy == "inherit"

	lines := []string{
		fmt.Sprintf("**Output path:** %s", outputPath),
		fmt.Sprintf("**Template:** %s", templatePath),
	}

	if !isResumed && len(data.Dependencies) > 0 {
		lines = append(lines, "", "**Dependencies:**")
		for _, dep := range data.Dependencies {
			absPath := filepath.Join(data.ChangeDir, dep.Path)
			lines = append(lines, fmt.Sprintf("- %s — %s", absPath, dep.Description))
		}
	}

	if data.Instruction != "" {
		lines = append(lines, "", strings.TrimSpace(data.Instruction))
	}

	lines = append(lines, "", "Read the template file for the expected output structure. Write your output to the output path.")

	return strings.Join(lines, "\n")
}

// NewEngine creates an OpenSpec engine with the real CLI runner.
func NewEngine(config map[string]any) engine.Engine {
	return NewEngineWithRunner(config, &realCmdRunner{})
}

// NewEngineWithRunner creates an OpenSpec engine with an injected CLI runner.
func NewEngineWithRunner(config map[string]any, cmdRunner CmdRunner) engine.Engine {
	changeParam, _ := config["change_param"].(string)
	if changeParam == "" {
		changeParam = "change_name"
	}
	return &openSpecEngine{
		changeParam: changeParam,
		cmdRunner:   cmdRunner,
	}
}

func init() {
	engine.Register("openspec", NewEngine)
}
