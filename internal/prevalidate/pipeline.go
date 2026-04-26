// Package prevalidate performs run-start workflow validation before execution.
package prevalidate

import (
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

type Mode int

const (
	Strict Mode = iota
	Lenient
)

type Options struct {
	LoadConfig func() (*config.Config, []string, error)
	LookPath   func(string) (string, error)
	Adapter    func(string) (cli.Adapter, error)
}

type Result struct {
	DeferredWarnings []ValidationError
	ProbeResults     []ProbeResult
}

type ProbeResult struct {
	CLI      string
	Model    string
	Effort   string
	Strength cli.ProbeStrength
}

type ValidationError struct {
	File          string
	LayerFiles    []string
	StepID        string
	ProfileSet    string
	Agent         string
	Field         string
	Value         string
	Allowed       []string
	ProbeStrength cli.ProbeStrength
	Deferred      bool
	Message       string
}

//nolint:gocritic // Value receiver keeps ValidationError usable both as an error and as warning data.
func (e ValidationError) Error() string {
	var b strings.Builder
	if e.Deferred {
		b.WriteString("warning: ")
	}
	if e.Message != "" {
		b.WriteString(e.Message)
	}
	appendField := func(name, value string) {
		if value == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(name)
		b.WriteString("=")
		b.WriteString(value)
	}
	appendField("file", e.File)
	appendField("step", e.StepID)
	appendField("profile", e.ProfileSet)
	appendField("agent", e.Agent)
	appendField("field", e.Field)
	appendField("value", e.Value)
	if len(e.Allowed) > 0 {
		appendField("allowed", strings.Join(e.Allowed, ","))
	}
	if len(e.LayerFiles) > 0 {
		appendField("layers", strings.Join(e.LayerFiles, ","))
	}
	if e.ProbeStrength.String() != "BinaryOnly" || e.Message == "" {
		appendField("probe_strength", e.ProbeStrength.String())
	}
	return b.String()
}

func Pipeline(rootPath string, boundParams map[string]string, mode Mode, opts Options) (Result, error) {
	opts = opts.withDefaults()
	cfg, layers, err := opts.LoadConfig()
	if err != nil {
		return Result{}, ValidationError{LayerFiles: layers, Message: fmt.Sprintf("loading layered config: %v", err)}
	}

	state := &walkState{
		mode:         mode,
		opts:         opts,
		cfg:          cfg,
		layerFiles:   layers,
		completed:    map[string]bool{},
		stack:        map[string]bool{},
		sessionDecls: map[string]string{},
		triples:      map[probeKey]probeSource{},
	}
	if err := state.walkFile(rootPath, boundParams, false, nil); err != nil {
		return state.result, err
	}
	if err := state.probeTriples(); err != nil {
		return state.result, err
	}
	return state.result, nil
}

func (o Options) withDefaults() Options {
	if o.LoadConfig == nil {
		o.LoadConfig = func() (*config.Config, []string, error) {
			cfg, err := config.Load(filepath.Join(".agent-runner", "config.yaml"))
			return cfg, defaultLayerFiles(), err
		}
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Adapter == nil {
		o.Adapter = cli.Get
	}
	return o
}

func defaultLayerFiles() []string {
	return []string{"builtin defaults", filepath.Join("~", ".agent-runner", "config.yaml"), filepath.Join(".agent-runner", "config.yaml")}
}

type walkState struct {
	mode         Mode
	opts         Options
	cfg          *config.Config
	layerFiles   []string
	result       Result
	completed    map[string]bool
	stack        map[string]bool
	sessionDecls map[string]string
	triples      map[probeKey]probeSource
}

type agentOrigin struct {
	profile string
	triple  probeKey
}

type probeKey struct {
	cli    string
	model  string
	effort string
}

type probeSource struct {
	file   string
	stepID string
	agent  string
}

func (s *walkState) walkFile(path string, params map[string]string, isSub bool, parentOrigin *agentOrigin) error {
	sourceID := loader.SourceID(path)
	if s.stack[sourceID] {
		return nil
	}
	visitKey := sourceID + "\x00" + stableParamKey(params)
	if s.completed[visitKey] {
		return nil
	}
	s.stack[sourceID] = true
	defer delete(s.stack, sourceID)

	workflow, err := loader.LoadWorkflow(path, loader.Options{IsSubWorkflow: isSub})
	if err != nil {
		return ValidationError{File: path, Message: err.Error()}
	}
	if err := createEngine(path, &workflow); err != nil {
		return err
	}
	for _, decl := range workflow.Sessions {
		if existing, ok := s.sessionDecls[decl.Name]; ok && existing != decl.Agent {
			return ValidationError{
				File:    path,
				Agent:   decl.Agent,
				Message: fmt.Sprintf("incompatible named session declaration %q: agent %q conflicts with %q", decl.Name, decl.Agent, existing),
			}
		}
		s.sessionDecls[decl.Name] = decl.Agent
	}

	visibleParams := bindParamDefaults(workflow.Params, params)
	paramNames := workflowParamNames(workflow.Params)
	_, err = s.walkSteps(path, workflow.Steps, visibleParams, paramNames, map[string]bool{}, nil, parentOrigin)
	if err != nil {
		return err
	}
	s.completed[visitKey] = true
	return nil
}

func stableParamKey(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var b strings.Builder
	for _, key := range keys {
		value := params[key]
		fmt.Fprintf(&b, "%d:%s=%d:%s;", len(key), key, len(value), value)
	}
	return b.String()
}

func createEngine(path string, workflow *model.Workflow) error {
	if workflow.Engine == nil {
		return nil
	}
	engConfig := map[string]any{"type": workflow.Engine.Type}
	maps.Copy(engConfig, workflow.Engine.Extras)
	if _, err := engine.Create(engConfig); err != nil {
		return ValidationError{File: path, Message: fmt.Sprintf("create engine: %v", err)}
	}
	return nil
}

func bindParamDefaults(params []model.Param, bound map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range bound {
		out[k] = v
	}
	for _, p := range params {
		if _, ok := out[p.Name]; !ok && p.Default != "" {
			out[p.Name] = p.Default
		}
	}
	return out
}

func workflowParamNames(params []model.Param) map[string]bool {
	out := map[string]bool{}
	for _, p := range params {
		out[p.Name] = true
	}
	return out
}

func (s *walkState) walkSteps(path string, steps []model.Step, params map[string]string, paramNames, captured map[string]bool, initialOrigin, parentOrigin *agentOrigin) (*agentOrigin, error) {
	currentOrigin := initialOrigin
	for i := range steps {
		step := &steps[i]
		nextOrigin, err := s.walkStep(path, step, params, paramNames, captured, currentOrigin, parentOrigin)
		if err != nil {
			return currentOrigin, err
		}
		if nextOrigin != nil {
			currentOrigin = nextOrigin
		}
		if step.Capture != "" {
			captured[step.Capture] = true
		}
	}
	return currentOrigin, nil
}

func (s *walkState) walkStep(path string, step *model.Step, params map[string]string, paramNames, captured map[string]bool, currentOrigin, parentOrigin *agentOrigin) (*agentOrigin, error) {
	if err := checkStepReferences(path, step, params, paramNames, captured); err != nil {
		return nil, err
	}
	if err := validateLoopReferences(path, step, params, paramNames, captured); err != nil {
		return nil, err
	}

	nextOrigin := currentOrigin
	if isAgentStep(step) {
		origin, err := s.collectAgent(path, step, currentOrigin, parentOrigin)
		if err != nil {
			return nil, err
		}
		if origin != nil {
			nextOrigin = origin
		}
	}

	if err := s.walkSubWorkflowStep(path, step, params, paramNames, captured, nextOrigin); err != nil {
		return nil, err
	}
	if err := s.walkNestedSteps(path, step, params, paramNames, captured, nextOrigin, parentOrigin); err != nil {
		return nil, err
	}
	return nextOrigin, nil
}

func validateLoopReferences(path string, step *model.Step, params map[string]string, paramNames, captured map[string]bool) error {
	if step.Loop == nil {
		return nil
	}
	if err := checkReferences(path, step.ID, "loop.over", step.Loop.Over, params, paramNames, captured, false); err != nil {
		return err
	}
	return validateLoop(path, step)
}

func (s *walkState) walkSubWorkflowStep(path string, step *model.Step, params map[string]string, paramNames, captured map[string]bool, currentOrigin *agentOrigin) error {
	if step.Workflow == "" {
		return nil
	}
	subPath, subParams, err := s.resolveSubWorkflow(path, step, params, paramNames, captured)
	if err != nil || subPath == "" {
		return err
	}
	return s.walkFile(subPath, subParams, true, currentOrigin)
}

func (s *walkState) walkNestedSteps(path string, step *model.Step, params map[string]string, paramNames, captured map[string]bool, currentOrigin, parentOrigin *agentOrigin) error {
	if len(step.Steps) == 0 {
		return nil
	}
	childParams, childParamNames := childScopeForStep(step, params, paramNames)
	_, err := s.walkSteps(path, step.Steps, childParams, childParamNames, copyBoolMap(captured), currentOrigin, parentOrigin)
	return err
}

func childScopeForStep(step *model.Step, params map[string]string, paramNames map[string]bool) (childParams map[string]string, childParamNames map[string]bool) {
	childParams = copyStringMap(params)
	childParamNames = copyBoolMap(paramNames)
	if step.Loop == nil {
		return childParams, childParamNames
	}
	if step.Loop.As != "" {
		childParams[step.Loop.As] = step.Loop.As
		childParamNames[step.Loop.As] = true
	}
	if step.Loop.AsIndex != "" {
		childParams[step.Loop.AsIndex] = step.Loop.AsIndex
		childParamNames[step.Loop.AsIndex] = true
	}
	return childParams, childParamNames
}

func checkStepReferences(path string, step *model.Step, params map[string]string, paramNames, captured map[string]bool) error {
	fields := map[string]string{
		"prompt":  step.Prompt,
		"command": step.Command,
	}
	for field, value := range fields {
		if err := checkReferences(path, step.ID, field, value, params, paramNames, captured, false); err != nil {
			return err
		}
	}
	for key, value := range step.Params {
		if err := checkReferences(path, step.ID, "params."+key, value, params, paramNames, captured, false); err != nil {
			return err
		}
	}
	return nil
}

func checkReferences(path, stepID, field, value string, params map[string]string, paramNames, captured map[string]bool, allowUnboundParams bool) error {
	for _, ref := range placeholders(value) {
		if _, ok := params[ref]; ok {
			continue
		}
		if paramNames[ref] {
			continue
		}
		if captured[ref] || isBuiltin(ref) {
			continue
		}
		if allowUnboundParams {
			continue
		}
		return ValidationError{
			File:    path,
			StepID:  stepID,
			Field:   field,
			Value:   ref,
			Message: fmt.Sprintf("undefined variable reference {{%s}}", ref),
		}
	}
	return nil
}

func copyBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isAgentStep(step *model.Step) bool {
	return step.Prompt != "" || step.Agent != ""
}

func validateLoop(path string, step *model.Step) error {
	if len(step.Steps) == 0 {
		return ValidationError{File: path, StepID: step.ID, Message: "loop requires at least one body step"}
	}
	if step.Loop.Over != "" {
		if _, err := filepath.Match(step.Loop.Over, ""); err != nil {
			return ValidationError{File: path, StepID: step.ID, Field: "over", Value: step.Loop.Over, Message: "invalid loop glob pattern"}
		}
	}
	if step.Loop.As != "" && !identRe.MatchString(step.Loop.As) {
		return ValidationError{File: path, StepID: step.ID, Field: "as", Value: step.Loop.As, Message: "invalid loop binding name"}
	}
	return nil
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (s *walkState) collectAgent(path string, step *model.Step, currentOrigin, parentOrigin *agentOrigin) (*agentOrigin, error) {
	profileName := ""
	var base *agentOrigin
	switch {
	case step.Session == model.SessionNew:
		profileName = step.Agent
	case model.IsNamedSession(step.Session):
		profileName = s.sessionDecls[string(step.Session)]
		if profileName == "" {
			return nil, ValidationError{File: path, StepID: step.ID, Message: fmt.Sprintf("no declaration found for named session %q", step.Session)}
		}
	case step.Session == model.SessionResume:
		base = currentOrigin
		if base == nil {
			return nil, ValidationError{File: path, StepID: step.ID, Message: "session resume has no prior session-originating step"}
		}
		profileName = base.profile
	case step.Session == model.SessionInherit:
		base = parentOrigin
		if base == nil {
			return nil, nil
		}
		profileName = base.profile
	default:
		profileName = step.Agent
	}

	triple, err := s.resolveTriple(path, step, profileName)
	if err != nil {
		return nil, err
	}
	src := probeSource{file: path, stepID: step.ID, agent: profileName}
	if _, ok := s.triples[triple]; !ok {
		s.triples[triple] = src
	}
	if step.Session == model.SessionNew || model.IsNamedSession(step.Session) {
		return &agentOrigin{profile: profileName, triple: triple}, nil
	}
	return nil, nil
}

func (s *walkState) resolveTriple(path string, step *model.Step, profileName string) (probeKey, error) {
	if s.cfg == nil {
		return probeKey{cli: step.CLI, model: step.Model}, nil
	}
	resolved, err := s.cfg.Resolve(profileName)
	if err != nil {
		return probeKey{}, ValidationError{
			File:       path,
			LayerFiles: s.layerFiles,
			StepID:     step.ID,
			ProfileSet: activeProfileName(s.cfg),
			Agent:      profileName,
			Message:    fmt.Sprintf("resolving profile %q: %v", profileName, err),
		}
	}
	if step.CLI != "" {
		resolved.CLI = step.CLI
	}
	if step.Model != "" {
		resolved.Model = step.Model
	}
	return probeKey{cli: resolved.CLI, model: resolved.Model, effort: resolved.Effort}, nil
}

func activeProfileName(cfg *config.Config) string {
	if cfg.ActiveProfile != "" {
		return cfg.ActiveProfile
	}
	return "default"
}

func (s *walkState) resolveSubWorkflow(parentFile string, step *model.Step, params map[string]string, paramNames, captured map[string]bool) (path string, paramsOut map[string]string, err error) {
	target, ok, err := s.resolveWorkflowField(parentFile, step, params, paramNames)
	if err != nil || !ok {
		return "", nil, err
	}
	resolvedParams := map[string]string{}
	interpolationParams := copyStringMap(params)
	for key := range paramNames {
		if _, ok := interpolationParams[key]; !ok {
			interpolationParams[key] = key
		}
	}
	capturedValues := map[string]string{}
	for key := range captured {
		capturedValues[key] = key
	}
	for k, v := range step.Params {
		interpolated, err := textfmt.Interpolate(v, interpolationParams, capturedValues, builtinNamesMap())
		if err != nil {
			return "", nil, ValidationError{File: parentFile, StepID: step.ID, Field: "params", Value: v, Message: err.Error()}
		}
		resolvedParams[k] = interpolated
	}
	return loader.ResolveRelativeWorkflowPath(parentFile, target), resolvedParams, nil
}

func (s *walkState) resolveWorkflowField(parentFile string, step *model.Step, params map[string]string, paramNames map[string]bool) (target string, ok bool, err error) {
	refs := placeholders(step.Workflow)
	if len(refs) == 0 {
		return step.Workflow, true, nil
	}
	values := map[string]string{}
	for _, ref := range refs {
		if value, ok := params[ref]; ok {
			values[ref] = value
			continue
		}
		if isBuiltin(ref) {
			warning := ValidationError{
				File:     parentFile,
				StepID:   step.ID,
				Field:    "workflow",
				Value:    step.Workflow,
				Deferred: true,
				Message:  fmt.Sprintf("sub-workflow target depends on builtin %q; checked at run time", ref),
			}
			s.result.DeferredWarnings = append(s.result.DeferredWarnings, warning)
			return "", false, nil
		}
		if paramNames[ref] && s.mode == Lenient {
			warning := ValidationError{
				File:     parentFile,
				StepID:   step.ID,
				Field:    "workflow",
				Value:    step.Workflow,
				Deferred: true,
				Message:  fmt.Sprintf("sub-workflow target depends on unbound param %q; checked at run time", ref),
			}
			s.result.DeferredWarnings = append(s.result.DeferredWarnings, warning)
			return "", false, nil
		}
		if paramNames[ref] {
			return "", false, ValidationError{
				File:    parentFile,
				StepID:  step.ID,
				Field:   "workflow",
				Value:   step.Workflow,
				Message: fmt.Sprintf("unresolved workflow parameter %q in sub-workflow target", ref),
			}
		}
		return "", false, ValidationError{
			File:    parentFile,
			StepID:  step.ID,
			Field:   "workflow",
			Value:   step.Workflow,
			Message: fmt.Sprintf("sub-workflow targets cannot depend on captured variables or unbound params: %q", ref),
		}
	}
	resolved, err := textfmt.Interpolate(step.Workflow, values, nil, nil)
	if err != nil {
		return "", false, ValidationError{File: parentFile, StepID: step.ID, Field: "workflow", Value: step.Workflow, Message: err.Error()}
	}
	return resolved, true, nil
}

var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

func placeholders(s string) []string {
	matches := placeholderRe.FindAllStringSubmatch(s, -1)
	var refs []string
	for _, m := range matches {
		if len(m) == 2 && !slices.Contains(refs, m[1]) {
			refs = append(refs, m[1])
		}
	}
	return refs
}

func isBuiltin(name string) bool {
	_, ok := builtinNamesMap()[name]
	return ok
}

func builtinNamesMap() map[string]string {
	return map[string]string{"session_dir": "", "step_id": ""}
}

func (s *walkState) probeTriples() error {
	seenCLI := map[string]error{}
	keys := make([]probeKey, 0, len(s.triples))
	for key := range s.triples {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b probeKey) int {
		return strings.Compare(a.cli+"|"+a.model+"|"+a.effort, b.cli+"|"+b.model+"|"+b.effort)
	})
	for _, key := range keys {
		src := s.triples[key]
		if _, ok := seenCLI[key.cli]; !ok {
			_, err := s.opts.LookPath(key.cli)
			seenCLI[key.cli] = err
			if err != nil {
				return probeError(key, src, cli.BinaryOnly, fmt.Errorf("binary not found: %w", err))
			}
		}
		adapter, err := s.opts.Adapter(key.cli)
		if err != nil {
			return probeError(key, src, cli.BinaryOnly, err)
		}
		strength, err := adapter.ProbeModel(key.model, key.effort)
		s.result.ProbeResults = append(s.result.ProbeResults, ProbeResult{
			CLI: key.cli, Model: key.model, Effort: key.effort, Strength: strength,
		})
		if err != nil {
			return probeError(key, src, strength, err)
		}
	}
	return nil
}

func probeError(key probeKey, src probeSource, strength cli.ProbeStrength, err error) error {
	return ValidationError{
		File:          src.file,
		StepID:        src.stepID,
		Agent:         src.agent,
		Field:         "model/effort",
		Value:         key.model + "/" + key.effort,
		ProbeStrength: strength,
		Message:       err.Error(),
	}
}
