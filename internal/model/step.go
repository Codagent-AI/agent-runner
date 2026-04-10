package model

import (
	"fmt"
	"slices"
	"strings"
)

// StepMode defines how a step executes.
type StepMode string

// Step mode constants.
const (
	ModeInteractive StepMode = "interactive"
	ModeHeadless    StepMode = "headless"
)

// SessionStrategy defines how an agent session is managed.
type SessionStrategy string

// Session strategy constants.
const (
	SessionNew     SessionStrategy = "new"
	SessionResume  SessionStrategy = "resume"
	SessionInherit SessionStrategy = "inherit"
)

// Loop defines iteration behavior for a step.
type Loop struct {
	Max            *int   `yaml:"max,omitempty" json:"max,omitempty"`
	Over           string `yaml:"over,omitempty" json:"over,omitempty"`
	As             string `yaml:"as,omitempty" json:"as,omitempty"`
	RequireMatches *bool  `yaml:"require_matches,omitempty" json:"require_matches,omitempty"`
}

// Validate checks that a Loop has valid field combinations.
func (l *Loop) Validate() error {
	hasMax := l.Max != nil
	hasOver := l.Over != ""
	hasAs := l.As != ""

	if hasMax && (hasOver || hasAs) {
		return fmt.Errorf(`loop must use either "max" or both "over" and "as", not both`)
	}

	if !hasMax && hasOver != hasAs {
		return fmt.Errorf(`loop requires both "over" and "as"`)
	}

	if !hasMax && !hasOver && !hasAs {
		return fmt.Errorf(`loop requires "max" or both "over" and "as"`)
	}

	if hasMax && *l.Max <= 0 {
		return fmt.Errorf(`loop "max" must be a positive integer`)
	}

	return nil
}

// Step defines a single workflow step.
type Step struct {
	ID                string            `yaml:"id" json:"id"`
	Prompt            string            `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Command           string            `yaml:"command,omitempty" json:"command,omitempty"`
	Agent             string            `yaml:"agent,omitempty" json:"agent,omitempty"`
	Mode              StepMode          `yaml:"mode,omitempty" json:"mode,omitempty"`
	Session           SessionStrategy   `yaml:"session,omitempty" json:"session,omitempty"`
	CLI               string            `yaml:"cli,omitempty" json:"cli,omitempty"`
	Capture           string            `yaml:"capture,omitempty" json:"capture,omitempty"`
	CaptureStderr     bool              `yaml:"capture_stderr,omitempty" json:"capture_stderr,omitempty"`
	ContinueOnFailure bool              `yaml:"continue_on_failure,omitempty" json:"continue_on_failure,omitempty"`
	SkipIf            string            `yaml:"skip_if,omitempty" json:"skip_if,omitempty"`
	BreakIf           string            `yaml:"break_if,omitempty" json:"break_if,omitempty"`
	Model             string            `yaml:"model,omitempty" json:"model,omitempty"`
	Workdir           string            `yaml:"workdir,omitempty" json:"workdir,omitempty"`
	Workflow          string            `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	Loop              *Loop             `yaml:"loop,omitempty" json:"loop,omitempty"`
	Params            map[string]string `yaml:"params,omitempty" json:"params,omitempty"`
	Steps             []Step            `yaml:"steps,omitempty" json:"steps,omitempty"`
}

// ApplyDefaults sets default values for fields that were not specified.
// For individual steps; see Workflow.ApplyDefaults for session strategy defaults.
func (s *Step) ApplyDefaults() {
	// No-op for individual steps. Session defaults are applied at the workflow level.
}

// StepType returns the classification of this step based on which fields are set.
func (s *Step) StepType() string {
	if s.Command != "" {
		return "shell"
	}
	if s.Prompt != "" || s.Agent != "" {
		return "agent"
	}
	if s.Loop != nil && len(s.Steps) > 0 {
		return "loop"
	}
	if s.Workflow != "" {
		return "sub-workflow"
	}
	if s.Loop == nil && len(s.Steps) > 0 {
		return "group"
	}
	return ""
}

func hasExactlyOneStepType(s *Step) bool {
	isShell := s.Command != ""
	isAgent := s.Prompt != "" || s.Agent != ""
	isLoop := s.Loop != nil && len(s.Steps) > 0
	isSubWorkflow := s.Workflow != ""
	isGroup := s.Loop == nil && len(s.Steps) > 0

	count := 0
	for _, b := range []bool{isShell, isAgent, isLoop, isSubWorkflow, isGroup} {
		if b {
			count++
		}
	}
	return count == 1
}

// isAgentContext returns true if the step is an agent step (has a prompt or
// an agent field), as opposed to a shell, loop, sub-workflow, or group step.
func (s *Step) isAgentContext() bool {
	return s.Prompt != "" || s.Agent != ""
}

func validateAgentOnlyField(fieldName string, isAgent bool) error {
	if !isAgent {
		return fmt.Errorf(`%q is only allowed on agent steps`, fieldName)
	}
	return nil
}

func validateCLIName(cliValue string, knownCLIs []string) error {
	if knownCLIs == nil {
		return nil
	}
	if slices.Contains(knownCLIs, cliValue) {
		return nil
	}
	return fmt.Errorf(`unknown cli adapter: %q`, cliValue)
}

// Validate checks that a Step has a valid configuration.
// knownCLIs is the list of registered CLI adapter names; if nil, CLI name
// validation is skipped (useful for tests that don't care about CLI names).
func (s *Step) Validate(knownCLIs []string) error {
	if !hasExactlyOneStepType(s) {
		return fmt.Errorf(`step must have exactly one of: command, prompt/mode, loop+steps, workflow, or steps (group)`)
	}

	if err := s.validateFieldConstraints(knownCLIs); err != nil {
		return err
	}

	if s.Loop != nil {
		if err := s.Loop.Validate(); err != nil {
			return err
		}
	}

	for i := range s.Steps {
		if err := s.Steps[i].Validate(knownCLIs); err != nil {
			return fmt.Errorf("steps[%d]: %w", i, err)
		}
	}

	return nil
}

// validateAgentField checks that the agent field is used correctly:
// required on session:new agent steps, forbidden on resume/inherit and shell steps.
// Also rejects unknown session strategy values on agent steps.
func (s *Step) validateAgentField(isAgent, isShell bool) error {
	if isAgent {
		switch s.Session {
		case SessionNew:
			if s.Agent == "" {
				return fmt.Errorf(`"agent" is required on agent steps with session "new"`)
			}
		case SessionResume, SessionInherit:
			if s.Agent != "" {
				return fmt.Errorf(`"agent" cannot be specified on %s steps`, s.Session)
			}
		default:
			if s.Session != "" {
				return fmt.Errorf(`invalid session strategy %q (must be new, resume, or inherit)`, s.Session)
			}
		}
	}
	if isShell && s.Agent != "" {
		return fmt.Errorf(`"agent" is not valid on shell steps`)
	}
	return nil
}

// validateCaptureFields checks capture and capture_stderr constraints.
func (s *Step) validateCaptureFields(isAgent, isShell bool) error {
	if s.Capture != "" && !isShell && isAgent {
		// When mode is explicit, validate it directly. When mode is unset and
		// a profile is configured (s.Agent), the profile's default_mode may
		// resolve to headless at execution time — defer the check.
		if s.Mode != "" && s.Mode != ModeHeadless {
			return fmt.Errorf(`"capture" is only allowed on shell and headless steps`)
		}
		if s.Mode == "" && s.Agent == "" {
			return fmt.Errorf(`"capture" is only allowed on shell and headless steps`)
		}
	}
	if s.CaptureStderr && s.Capture == "" {
		return fmt.Errorf(`"capture_stderr" requires "capture"`)
	}
	if s.CaptureStderr && !isShell {
		return fmt.Errorf(`"capture_stderr" is only allowed on shell steps`)
	}
	return nil
}

func (s *Step) validateFieldConstraints(knownCLIs []string) error {
	isAgent := s.isAgentContext()
	isShell := s.Command != ""

	if isAgent && s.Prompt == "" {
		return fmt.Errorf(`agent steps require "prompt"`)
	}

	if err := s.validateAgentField(isAgent, isShell); err != nil {
		return err
	}

	if err := s.validateCaptureFields(isAgent, isShell); err != nil {
		return err
	}

	if s.Workdir != "" && !isShell && !isAgent {
		return fmt.Errorf(`"workdir" is only allowed on shell and agent steps`)
	}

	if s.Model != "" {
		if err := validateAgentOnlyField("model", isAgent); err != nil {
			return err
		}
	}

	if s.CLI != "" {
		if err := validateAgentOnlyField("cli", isAgent); err != nil {
			return err
		}
		if err := validateCLIName(s.CLI, knownCLIs); err != nil {
			return err
		}
	}

	if s.Loop != nil && len(s.Steps) == 0 {
		return fmt.Errorf(`"loop" requires a non-empty "steps" array`)
	}

	if s.Params != nil && s.Workflow == "" {
		return fmt.Errorf(`"params" is only allowed on sub-workflow steps`)
	}

	if s.SkipIf != "" && s.SkipIf != "previous_success" {
		return fmt.Errorf(`invalid skip_if value: %q`, s.SkipIf)
	}

	if s.BreakIf != "" && s.BreakIf != "success" && s.BreakIf != "failure" {
		return fmt.Errorf(`invalid break_if value: %q`, s.BreakIf)
	}

	if s.Mode != "" && s.Mode != ModeInteractive && s.Mode != ModeHeadless {
		return fmt.Errorf(`invalid mode: %q`, s.Mode)
	}

	return nil
}

// Param defines a workflow parameter.
type Param struct {
	Name     string `yaml:"name" json:"name"`
	Required *bool  `yaml:"required,omitempty" json:"required,omitempty"`
	Default  string `yaml:"default,omitempty" json:"default,omitempty"`
}

// IsRequired returns whether this param is required (defaults to true).
func (p *Param) IsRequired() bool {
	if p.Required == nil {
		return true
	}
	return *p.Required
}

// EngineConfig defines an engine configuration block.
type EngineConfig struct {
	Type   string         `yaml:"type" json:"type"`
	Extras map[string]any `yaml:",inline" json:"-"`
}

// Workflow defines a complete workflow.
type Workflow struct {
	Name        string        `yaml:"name" json:"name"`
	Description string        `yaml:"description,omitempty" json:"description,omitempty"`
	Params      []Param       `yaml:"params,omitempty" json:"params,omitempty"`
	Steps       []Step        `yaml:"steps" json:"steps"`
	Engine      *EngineConfig `yaml:"engine,omitempty" json:"engine,omitempty"`
}

// ApplyDefaults sets default values for Workflow fields.
// For agent steps, the first agentic step (one with a prompt) defaults to
// session: new; all subsequent agentic steps default to session: resume.
// Explicit session values are never overwritten.
func (w *Workflow) ApplyDefaults() {
	if w.Params == nil {
		w.Params = []Param{}
	}
	seenFirstAgentic := false
	applyStepDefaults(w.Steps, &seenFirstAgentic)
}

// applyStepDefaults recursively applies session strategy defaults.
func applyStepDefaults(steps []Step, seenFirstAgentic *bool) {
	for i := range steps {
		s := &steps[i]
		isAgentic := s.Prompt != "" || s.Agent != ""
		if isAgentic && s.Session == "" {
			if !*seenFirstAgentic {
				s.Session = SessionNew
				*seenFirstAgentic = true
			} else {
				s.Session = SessionResume
			}
		} else if isAgentic && s.Session != "" {
			if !*seenFirstAgentic {
				*seenFirstAgentic = true
			}
		}
		// Recurse into nested steps (groups/loops).
		if len(s.Steps) > 0 {
			applyStepDefaults(s.Steps, seenFirstAgentic)
		}
	}
}

// Validate checks that a Workflow has a valid configuration.
// knownCLIs is the list of registered CLI adapter names; if nil, CLI name
// validation is skipped.
func (w *Workflow) Validate(knownCLIs []string) error {
	if w.Name == "" {
		return fmt.Errorf("workflow name is required")
	}

	if len(w.Steps) == 0 {
		return fmt.Errorf("workflow must have at least one step")
	}

	if w.Engine != nil && w.Engine.Type == "" {
		return fmt.Errorf("engine type is required")
	}

	var errs []string
	for i := range w.Steps {
		if err := w.Steps[i].Validate(knownCLIs); err != nil {
			errs = append(errs, fmt.Sprintf("steps[%d]: %v", i, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("workflow validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}
