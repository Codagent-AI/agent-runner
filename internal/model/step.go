package model

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/codagent/agent-runner/internal/flowctl"
)

var identifierRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// StepMode defines how a step executes.
type StepMode string

// Step mode constants.
const (
	ModeInteractive StepMode = "interactive"
	ModeHeadless    StepMode = "headless"
	ModeUI          StepMode = "ui"
)

// SessionStrategy defines how an agent session is managed.
type SessionStrategy string

// Session strategy constants.
const (
	SessionNew     SessionStrategy = "new"
	SessionResume  SessionStrategy = "resume"
	SessionInherit SessionStrategy = "inherit"
)

// IsNamedSession reports whether s is a named session reference (not empty and
// not one of the reserved strategy keywords new/resume/inherit).
func IsNamedSession(s SessionStrategy) bool {
	switch s {
	case SessionNew, SessionResume, SessionInherit, "":
		return false
	default:
		return true
	}
}

// SessionDecl declares a named agent session for a workflow.
type SessionDecl struct {
	Name  string `yaml:"name" json:"name"`
	Agent string `yaml:"agent" json:"agent"`
}

// Loop defines iteration behavior for a step.
type Loop struct {
	Max            *int   `yaml:"max,omitempty" json:"max,omitempty"`
	Over           string `yaml:"over,omitempty" json:"over,omitempty"`
	As             string `yaml:"as,omitempty" json:"as,omitempty"`
	AsIndex        string `yaml:"as_index,omitempty" json:"as_index,omitempty"`
	RequireMatches *bool  `yaml:"require_matches,omitempty" json:"require_matches,omitempty"`
}

type UIAction struct {
	Label   string `yaml:"label" json:"label"`
	Outcome string `yaml:"outcome" json:"outcome"`
}

type UIInput struct {
	Kind    string   `yaml:"kind" json:"kind"`
	ID      string   `yaml:"id" json:"id"`
	Prompt  string   `yaml:"prompt" json:"prompt"`
	Options []string `yaml:"options,omitempty" json:"options,omitempty"`
	Default string   `yaml:"default,omitempty" json:"default,omitempty"`
}

type UIStepRequest struct {
	StepID  string
	Title   string
	Body    string
	Actions []UIAction
	Inputs  []UIInput
}

type UIStepResult struct {
	Outcome  string
	Inputs   map[string]string
	Canceled bool
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

	if l.AsIndex != "" && l.AsIndex == l.As {
		return fmt.Errorf(`loop "as_index" must differ from "as"`)
	}

	return nil
}

// Step defines a single workflow step.
type Step struct {
	ID                string            `yaml:"id" json:"id"`
	Prompt            string            `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Command           string            `yaml:"command,omitempty" json:"command,omitempty"`
	Script            string            `yaml:"script,omitempty" json:"script,omitempty"`
	ScriptInputs      map[string]string `yaml:"script_inputs,omitempty" json:"script_inputs,omitempty"`
	CaptureFormat     string            `yaml:"capture_format,omitempty" json:"capture_format,omitempty"`
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
	Title             string            `yaml:"title,omitempty" json:"title,omitempty"`
	Body              string            `yaml:"body,omitempty" json:"body,omitempty"`
	Actions           []UIAction        `yaml:"actions,omitempty" json:"actions,omitempty"`
	Inputs            []UIInput         `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	OutcomeCapture    string            `yaml:"outcome_capture,omitempty" json:"outcome_capture,omitempty"`
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
	if s.Script != "" {
		return "script"
	}
	if s.Mode == ModeUI {
		return "ui"
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
	isScript := s.Script != ""
	isUI := s.Mode == ModeUI
	isAgent := s.Prompt != "" || s.Agent != ""
	isLoop := s.Loop != nil && len(s.Steps) > 0
	isSubWorkflow := s.Workflow != ""
	isGroup := s.Loop == nil && len(s.Steps) > 0

	count := 0
	for _, b := range []bool{isShell, isScript, isUI, isAgent, isLoop, isSubWorkflow, isGroup} {
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
	if s.Mode == ModeUI {
		if err := s.validateUIExclusiveFields(); err != nil {
			return err
		}
	}

	if !hasExactlyOneStepType(s) {
		return fmt.Errorf(`step must have exactly one of: command, script, mode: ui, prompt/mode, loop+steps, workflow, or steps (group)`)
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

func (s *Step) validateUIExclusiveFields() error {
	switch {
	case s.Agent != "":
		return fmt.Errorf(`"agent" is not valid on ui steps`)
	case s.Command != "":
		return fmt.Errorf(`"command" is not valid on ui steps`)
	case s.Prompt != "":
		return fmt.Errorf(`"prompt" is not valid on ui steps`)
	case s.Session != "":
		return fmt.Errorf(`"session" is not valid on ui steps`)
	case s.Workflow != "":
		return fmt.Errorf(`"workflow" is not valid on ui steps`)
	case s.Loop != nil:
		return fmt.Errorf(`"loop" is not valid on ui steps`)
	case len(s.Steps) > 0:
		return fmt.Errorf(`"steps" is not valid on ui steps`)
	}
	return nil
}

// validateAgentField checks that the agent field is used correctly:
// required on session:new agent steps, forbidden on resume/inherit/named and shell steps.
// Named session values (not new/resume/inherit) are accepted; agent: is forbidden on them.
func (s *Step) validateAgentField(isAgent, isShell bool) error {
	if isAgent {
		switch {
		case s.Session == SessionNew:
			if s.Agent == "" {
				return fmt.Errorf(`"agent" is required on agent steps with session "new"`)
			}
		case s.Session == SessionResume || s.Session == SessionInherit:
			if s.Agent != "" {
				return fmt.Errorf(`"agent" cannot be specified on %s steps`, s.Session)
			}
		case IsNamedSession(s.Session):
			if s.Agent != "" {
				return fmt.Errorf(`"agent" cannot be specified on named session steps; it is pinned by the session declaration`)
			}
		default:
			if s.Session != "" {
				return fmt.Errorf(`invalid session strategy %q (must be new, resume, inherit, or a declared session name)`, s.Session)
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
	if s.Capture != "" {
		if isShell && s.Mode == ModeInteractive {
			return fmt.Errorf(`"capture" cannot be combined with "mode: interactive" on shell steps`)
		}
		switch {
		case isShell, s.Script != "", s.Mode == ModeUI:
			// ok
		case !isAgent:
			return fmt.Errorf(`"capture" is only allowed on shell, script, ui, and headless steps`)
		default:
			// When mode is explicit, validate it directly. When mode is unset and
			// a profile is configured (s.Agent), the profile's default_mode may
			// resolve to headless at execution time — defer the check.
			if s.Mode != "" && s.Mode != ModeHeadless {
				return fmt.Errorf(`"capture" is only allowed on shell, script, ui, and headless steps`)
			}
			if s.Mode == "" && s.Agent == "" && s.Session != SessionResume && s.Session != SessionInherit && !IsNamedSession(s.Session) {
				return fmt.Errorf(`"capture" is only allowed on shell, script, ui, and headless steps`)
			}
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
	isScript := s.Script != ""
	isUI := s.Mode == ModeUI

	if isAgent && s.Prompt == "" {
		return fmt.Errorf(`agent steps require "prompt"`)
	}

	if err := s.validateAgentField(isAgent, isShell); err != nil {
		return err
	}

	if err := s.validateCaptureFields(isAgent, isShell); err != nil {
		return err
	}

	if s.Workdir != "" && !isShell && !isAgent && !isScript {
		return fmt.Errorf(`"workdir" is only allowed on shell, script, and agent steps`)
	}

	if err := s.validateAgentAdapterFields(knownCLIs, isAgent, isScript, isUI); err != nil {
		return err
	}

	if s.Loop != nil && len(s.Steps) == 0 {
		return fmt.Errorf(`"loop" requires a non-empty "steps" array`)
	}

	if s.Params != nil && s.Workflow == "" {
		return fmt.Errorf(`"params" is only allowed on sub-workflow steps`)
	}

	if err := s.validateScriptFields(isScript); err != nil {
		return err
	}

	if err := s.validateUIFields(isUI); err != nil {
		return err
	}

	if s.SkipIf != "" && s.SkipIf != "previous_success" {
		if cmd, ok := flowctl.ShellSkipCommand(s.SkipIf); !ok || cmd == "" {
			return fmt.Errorf(`invalid skip_if value: %q`, s.SkipIf)
		}
	}

	if s.BreakIf != "" && s.BreakIf != "success" && s.BreakIf != "failure" {
		return fmt.Errorf(`invalid break_if value: %q`, s.BreakIf)
	}

	if s.Mode != "" && s.Mode != ModeInteractive && s.Mode != ModeHeadless && s.Mode != ModeUI {
		return fmt.Errorf(`invalid mode: %q`, s.Mode)
	}

	return nil
}

func (s *Step) validateAgentAdapterFields(knownCLIs []string, isAgent, isScript, isUI bool) error {
	if err := validateModelField(s.Model, isAgent, isScript, isUI); err != nil {
		return err
	}
	if s.CLI == "" {
		return nil
	}
	if isUI {
		return fmt.Errorf(`"cli" is not valid on ui steps`)
	}
	if isScript {
		return fmt.Errorf(`"cli" is not valid on script steps`)
	}
	if err := validateAgentOnlyField("cli", isAgent); err != nil {
		return err
	}
	return validateCLIName(s.CLI, knownCLIs)
}

func validateModelField(modelValue string, isAgent, isScript, isUI bool) error {
	if modelValue == "" {
		return nil
	}
	if isUI {
		return fmt.Errorf(`"model" is not valid on ui steps`)
	}
	if isScript {
		return fmt.Errorf(`"model" is not valid on script steps`)
	}
	return validateAgentOnlyField("model", isAgent)
}

func (s *Step) validateScriptFields(isScript bool) error {
	if s.ScriptInputs != nil && !isScript {
		return fmt.Errorf(`"script_inputs" is only allowed on script steps`)
	}
	if !isScript {
		if s.CaptureFormat != "" {
			return fmt.Errorf(`"capture_format" requires a script step with "capture"`)
		}
		return nil
	}
	if s.Mode != "" {
		return fmt.Errorf(`"mode" is not valid on script steps`)
	}
	if s.Session != "" {
		return fmt.Errorf(`"session" is not valid on script steps`)
	}
	if err := validateScriptPath(s.Script); err != nil {
		return err
	}
	if s.CaptureFormat == "" {
		return nil
	}
	if s.Capture == "" {
		return fmt.Errorf(`"capture_format" requires a script step with "capture"`)
	}
	if s.CaptureFormat != "text" && s.CaptureFormat != "json" {
		return fmt.Errorf(`invalid capture_format: %q`, s.CaptureFormat)
	}
	return nil
}

func validateScriptPath(script string) error {
	if strings.Contains(script, "{{") || strings.Contains(script, "}}") {
		return fmt.Errorf(`"script" must be a static path`)
	}
	// Normalize backslashes to forward slashes before validation to prevent
	// bypass via paths like "..\outside\evil.sh".
	normalized := strings.ReplaceAll(script, `\`, "/")
	clean := path.Clean(normalized)
	if clean == "." {
		return fmt.Errorf(`"script" path is required`)
	}
	if path.IsAbs(clean) || filepath.IsAbs(script) {
		return fmt.Errorf(`absolute script paths are not allowed`)
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return fmt.Errorf(`script path traversal is not allowed`)
		}
	}
	return nil
}

func (s *Step) validateUIFields(isUI bool) error {
	if s.OutcomeCapture != "" && !isUI {
		return fmt.Errorf(`"outcome_capture" is only allowed on ui steps`)
	}
	if !isUI {
		if s.Title != "" || s.Body != "" || len(s.Actions) > 0 || len(s.Inputs) > 0 {
			return fmt.Errorf(`ui fields are only allowed on ui steps`)
		}
		return nil
	}
	if s.Title == "" {
		return fmt.Errorf(`"title" is required on ui steps`)
	}
	if len(s.Actions) == 0 {
		return fmt.Errorf(`"actions" is required on ui steps`)
	}
	if s.Capture != "" && len(s.Inputs) == 0 {
		return fmt.Errorf(`"capture" requires "inputs" on ui steps`)
	}
	if s.Capture != "" && s.Capture == s.OutcomeCapture {
		return fmt.Errorf(`"capture" and "outcome_capture" must name distinct variables`)
	}

	if err := validateUIActions(s.Actions); err != nil {
		return err
	}
	return validateUIInputs(s.Inputs)
}

func validateUIActions(actions []UIAction) error {
	outcomes := make(map[string]struct{}, len(actions))
	for i, action := range actions {
		if err := validateUIAction(i, action, outcomes); err != nil {
			return err
		}
	}
	return nil
}

func validateUIAction(i int, action UIAction, outcomes map[string]struct{}) error {
	if action.Label == "" || action.Outcome == "" {
		return fmt.Errorf(`actions[%d] requires label and outcome`, i)
	}
	if strings.Contains(action.Outcome, "{{") || strings.Contains(action.Outcome, "}}") {
		return fmt.Errorf(`actions[%d].outcome must be static identifiers`, i)
	}
	if !identifierRe.MatchString(action.Outcome) {
		return fmt.Errorf(`actions[%d].outcome must match ^[a-z][a-z0-9_]*$`, i)
	}
	if _, exists := outcomes[action.Outcome]; exists {
		return fmt.Errorf(`duplicate outcome %q`, action.Outcome)
	}
	outcomes[action.Outcome] = struct{}{}
	return nil
}

func validateUIInputs(inputs []UIInput) error {
	seen := make(map[string]struct{}, len(inputs))
	for i := range inputs {
		if err := validateUIInput(i, &inputs[i], seen); err != nil {
			return err
		}
	}
	return nil
}

func validateUIInput(i int, input *UIInput, seen map[string]struct{}) error {
	if input.Kind != "single_select" && input.Kind != "single-select" {
		return fmt.Errorf(`inputs[%d]: only single_select inputs are supported`, i)
	}
	if input.ID == "" {
		return fmt.Errorf(`inputs[%d].id is required`, i)
	}
	if !identifierRe.MatchString(input.ID) {
		return fmt.Errorf(`inputs[%d].id must match ^[a-z][a-z0-9_]*$`, i)
	}
	if _, exists := seen[input.ID]; exists {
		return fmt.Errorf(`duplicate input id %q`, input.ID)
	}
	seen[input.ID] = struct{}{}
	if input.Prompt == "" {
		return fmt.Errorf(`inputs[%d].prompt is required`, i)
	}
	if len(input.Options) == 0 {
		return fmt.Errorf(`inputs[%d].options is required`, i)
	}
	if input.Default != "" && !isDynamicOptions(input.Options) && !slices.Contains(input.Options, input.Default) {
		return fmt.Errorf(`inputs[%d].default is not among declared options`, i)
	}
	return nil
}

func isDynamicOptions(options []string) bool {
	return len(options) == 1 && strings.HasPrefix(options[0], "{{") && strings.HasSuffix(options[0], "}}")
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
	Sessions    []SessionDecl `yaml:"sessions,omitempty" json:"sessions,omitempty"`
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
