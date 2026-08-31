// Package intakeroute validates and persists run-owned intake route sidecars.
package intakeroute

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

const (
	// MaxRequestBytes bounds agent-written route requests.
	MaxRequestBytes = 64 << 10
	// MaxHandoffBytes bounds the handoff text carried into a launched run's
	// first agent prompt.
	MaxHandoffBytes = 8 << 10

	// maxListedWorkflows bounds how many routable names a resolution failure
	// names, so a large user catalog cannot flood the agent's context.
	maxListedWorkflows = 40

	sidecarName = "intake-route.json"
	requestName = "route-request.json"
	catalogName = "route-catalog.md"
)

// SidecarPath returns the route sidecar for a run directory.
func SidecarPath(runDir string) string { return filepath.Join(runDir, sidecarName) }

// RequestPathFor returns the runner-owned route request path for a run directory.
func RequestPathFor(runDir string) string { return filepath.Join(runDir, requestName) }

// CatalogPathFor returns the runner-owned workflow catalog path for a run
// directory. The catalog is what the intake agent reads to recommend a route.
func CatalogPathFor(runDir string) string { return filepath.Join(runDir, catalogName) }

// RenderCatalog describes every workflow the agent may route to, so the route
// is chosen from the same catalog the validator will accept rather than from
// whatever the agent inferred about the repository.
func RenderCatalog(catalog Catalog, intakeWorkflow string) string {
	var out strings.Builder
	out.WriteString("# Workflows you can route to\n\n")
	out.WriteString("Put the canonical name in the route request's `workflow` field. Supply every required\n")
	out.WriteString("parameter and no parameter that is not listed here.\n")
	for _, entry := range routableEntries(catalog, intakeWorkflow) {
		out.WriteString("\n## " + entry.CanonicalName + "\n")
		if description := strings.TrimSpace(entry.Description); description != "" {
			out.WriteString(description + "\n")
		}
		required, optional := partitionParams(entry.Params)
		out.WriteString("Required parameters: " + joinOrNone(required) + "\n")
		out.WriteString("Optional parameters: " + joinOrNone(optional) + "\n")
	}
	return out.String()
}

// WriteCatalog publishes the catalog for a run and returns its path. It is
// written before the agent starts and never read back by the runner: the
// validator resolves the request against the catalog directly, so a stale or
// tampered file cannot widen what the agent may route to.
func WriteCatalog(opts *ValidateOptions) (string, error) {
	if opts == nil || opts.RunDir == "" {
		return "", errors.New("route catalog requires a run directory")
	}
	temporary, err := os.CreateTemp(opts.RunDir, ".route-catalog-*")
	if err != nil {
		return "", fmt.Errorf("create route catalog: %w", err)
	}
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return "", fmt.Errorf("secure route catalog: %w", err)
	}
	if _, err := temporary.WriteString(RenderCatalog(opts.Catalog, opts.IntakeWorkflow)); err != nil {
		return "", fmt.Errorf("write route catalog: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("write route catalog: %w", err)
	}
	// Rename replaces whatever occupies the path without following it, so an
	// agent that swapped the catalog for a symbolic link between attempts
	// cannot redirect this write outside the run directory.
	path := CatalogPathFor(opts.RunDir)
	if err := os.Rename(temporary.Name(), path); err != nil {
		return "", fmt.Errorf("publish route catalog: %w", err)
	}
	return path, nil
}

func partitionParams(params []model.Param) (required, optional []string) {
	for _, param := range params {
		if param.IsRequired() {
			required = append(required, param.Name)
			continue
		}
		optional = append(optional, param.Name)
	}
	return required, optional
}

func joinOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

var (
	// ErrWorkflowNotFound lets catalog implementations distinguish a missing target.
	ErrWorkflowNotFound = errors.New("workflow not found")
	// ErrFrozen reports that a published route can no longer be replaced.
	ErrFrozen = errors.New("intake route is frozen")
	// ErrNoRoute reports an attempt to freeze a run without a route.
	ErrNoRoute = errors.New("no staged intake route")
)

// State is the lifecycle state of a sealed route.
type State string

const (
	Staged State = "staged"
	Frozen State = "frozen"
)

// ViolationCode identifies the validation rule a submitted route violated.
type ViolationCode string

const (
	ViolationConfiguration      ViolationCode = "configuration"
	ViolationRunDirectory       ViolationCode = "run_directory"
	ViolationRequest            ViolationCode = "request"
	ViolationDecode             ViolationCode = "decode"
	ViolationWorkflowResolution ViolationCode = "workflow_resolution"
	ViolationSelfRoute          ViolationCode = "self_route"
	ViolationParameter          ViolationCode = "parameter"
	ViolationHandoff            ViolationCode = "handoff"
)

// ValidationError is an actionable, transport-independent route validation
// failure. Code is stable for clients; the optional context fields identify the
// relevant request value while Message remains suitable for immediate display.
type ValidationError struct {
	Code      ViolationCode
	Message   string
	Workflow  string
	Parameter string
	Path      string
	Err       error
}

func (e *ValidationError) Error() string { return e.Message }

// Unwrap exposes the underlying filesystem, decoding, or catalog error.
func (e *ValidationError) Unwrap() error { return e.Err }

// ViolationCode returns the stable code without requiring callers to parse the
// human-facing Error string.
func (e *ValidationError) ViolationCode() string { return string(e.Code) }

// Request is the strict, agent-written route request.
type Request struct {
	Workflow string            `json:"workflow"`
	Params   map[string]string `json:"params,omitempty"`
	Handoff  string            `json:"handoff"`
}

// Sealed is the run-owned record persisted in intake-route.json.
type Sealed struct {
	State       State             `json:"state"`
	ParentRunID string            `json:"parent_run_id"`
	Workflow    string            `json:"workflow"`
	SourceRef   string            `json:"source_ref"`
	Params      map[string]string `json:"params"`
	Handoff     string            `json:"handoff"`
	StagedAt    string            `json:"staged_at"`
	FrozenAt    string            `json:"frozen_at,omitempty"`
}

// Catalog resolves canonical workflow names through the normal discovery result.
// Callers should construct it from discovery.Enumerate rather than interpreting
// workflow names themselves.
type Catalog interface {
	ResolveWorkflow(string) (discovery.WorkflowEntry, error)
}

// RoutableCatalog is an optional Catalog capability. When a catalog implements
// it, a resolution failure names the workflows the agent may route to instead,
// so one rejected attempt teaches the catalog rather than inviting a guess.
type RoutableCatalog interface {
	RoutableWorkflows() []discovery.WorkflowEntry
}

// NewCatalog returns a catalog backed by entries produced by discovery.Enumerate.
func NewCatalog(entries []discovery.WorkflowEntry) Catalog {
	catalog := &entryCatalog{byName: make(map[string]discovery.WorkflowEntry, len(entries))}
	for _, entry := range entries {
		catalog.byName[entry.CanonicalName] = entry
	}
	return catalog
}

type entryCatalog struct {
	byName map[string]discovery.WorkflowEntry
}

func (c *entryCatalog) ResolveWorkflow(name string) (discovery.WorkflowEntry, error) {
	entry, ok := c.byName[name]
	if !ok || entry.ParseError != "" || entry.SourcePath == "" {
		return discovery.WorkflowEntry{}, ErrWorkflowNotFound
	}
	return entry, nil
}

// RoutableWorkflows lists the resolvable, non-hidden entries by canonical name.
// Hidden workflows are omitted because they are sub-workflow building blocks
// rather than routes a user would start, which is also why the new tab keeps
// them behind its show-hidden toggle.
func (c *entryCatalog) RoutableWorkflows() []discovery.WorkflowEntry {
	entries := make([]discovery.WorkflowEntry, 0, len(c.byName))
	for name, entry := range c.byName {
		if entry.Hidden {
			continue
		}
		if _, err := c.ResolveWorkflow(name); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].CanonicalName < entries[j].CanonicalName })
	return entries
}

// ValidateOptions supplies the transport-independent validation dependencies.
// Request takes precedence when supplied; otherwise RequestPath, or the default
// route-request.json beneath RunDir, is read.
type ValidateOptions struct {
	RunDir         string
	ParentRunID    string
	IntakeWorkflow string
	RequestPath    string
	Request        []byte
	Catalog        Catalog
	Now            func() time.Time
}

// Prepared is a validated, unpublished route. It must be staged or discarded.
type Prepared struct {
	sealed    Sealed
	discarded bool
	published bool
}

// Sealed returns a copy of the metadata that will be published.
func (p *Prepared) Sealed() Sealed {
	if p == nil {
		return Sealed{}
	}
	sealed := p.sealed
	sealed.Params = cloneParams(p.sealed.Params)
	return sealed
}

// Discard marks an unpublished route unavailable. It is safe to call repeatedly.
func (p *Prepared) Discard() error {
	if p == nil || p.discarded || p.published {
		return nil
	}
	p.discarded = true
	return nil
}

// Validate resolves and validates a route request, including the prompt handoff.
func Validate(opts *ValidateOptions) (*Prepared, error) {
	if opts == nil {
		return nil, validationFailure(ViolationConfiguration, errors.New("route validation options are required"), "", "", "")
	}
	runDir, err := canonicalRunDir(opts.RunDir)
	if err != nil {
		return nil, validationFailure(ViolationRunDirectory, err, "", "", opts.RunDir)
	}
	requestBytes, err := readRequest(opts, runDir)
	if err != nil {
		return nil, validationFailure(ViolationRequest, err, "", "", opts.RequestPath)
	}
	request, err := decodeRequest(requestBytes)
	if err != nil {
		return nil, validationFailure(ViolationDecode, err, "", "", "")
	}
	if opts.Catalog == nil {
		return nil, validationFailure(ViolationConfiguration, errors.New("route workflow catalog is required"), request.Workflow, "", "")
	}
	entry, err := opts.Catalog.ResolveWorkflow(request.Workflow)
	if err != nil {
		if errors.Is(err, ErrWorkflowNotFound) {
			return nil, validationFailure(ViolationWorkflowResolution, workflowNotFoundError(opts, request.Workflow), request.Workflow, "", "")
		}
		return nil, validationFailure(ViolationWorkflowResolution, fmt.Errorf("resolve workflow %q: %w", request.Workflow, err), request.Workflow, "", "")
	}
	if entry.CanonicalName == "" || entry.SourcePath == "" || entry.ParseError != "" {
		return nil, validationFailure(ViolationWorkflowResolution, workflowNotFoundError(opts, request.Workflow), request.Workflow, "", "")
	}
	if request.Workflow == opts.IntakeWorkflow {
		return nil, validationFailure(ViolationSelfRoute, errors.New("intake cannot route to itself"), request.Workflow, "", "")
	}
	if err := validateParams(&entry, request.Params); err != nil {
		return nil, validationFailure(ViolationParameter, err, entry.CanonicalName, "", "")
	}
	if strings.TrimSpace(request.Handoff) == "" {
		return nil, validationFailure(ViolationHandoff, errors.New("route handoff is required"), entry.CanonicalName, "", "")
	}
	if len(request.Handoff) > MaxHandoffBytes {
		return nil, validationFailure(ViolationHandoff, fmt.Errorf("route handoff exceeds %d KiB", MaxHandoffBytes>>10), entry.CanonicalName, "", "")
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	return &Prepared{
		sealed: Sealed{
			State:       Staged,
			ParentRunID: opts.ParentRunID,
			Workflow:    entry.CanonicalName,
			SourceRef:   entry.SourcePath,
			Params:      cloneParams(request.Params),
			Handoff:     request.Handoff,
			StagedAt:    now().UTC().Format(time.RFC3339Nano),
		},
	}, nil
}

// Store owns the sidecar for one run directory.
type Store struct{ path string }

// NewStore creates a sidecar store rooted in runDir.
func NewStore(runDir string) *Store { return &Store{path: SidecarPath(runDir)} }

// Load reads the persisted route record.
func (s *Store) Load() (*Sealed, error) {
	if s == nil || s.path == "" {
		return nil, errors.New("intake route store is required")
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var sealed Sealed
	if err := json.Unmarshal(data, &sealed); err != nil {
		return nil, fmt.Errorf("decode intake route sidecar: %w", err)
	}
	sealed.Params = cloneParams(sealed.Params)
	return &sealed, nil
}

// LoadStrict reads a sealed route from an explicit sidecar path. It rejects
// unknown fields and trailing JSON so process-boundary consumers never act on
// a partially written or malformed launch plan.
func LoadStrict(path string) (*Sealed, error) {
	file, err := os.Open(path) // #nosec G304 -- path is supplied by the internal launcher and validated by its caller.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var sealed Sealed
	if err := decodeSingleJSON(file, &sealed, "intake route sidecar"); err != nil {
		return nil, err
	}
	sealed.Params = cloneParams(sealed.Params)
	return &sealed, nil
}

// decodeSingleJSON decodes exactly one JSON value into target, rejecting
// unknown fields and any trailing value. Both the sidecar and the agent-written
// request need identical strictness, so they share this rather than each
// spelling out the decode-then-assert-EOF dance.
func decodeSingleJSON(reader io.Reader, target any, subject string) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", subject, err)
	}
	switch err := decoder.Decode(&struct{}{}); {
	case err == nil:
		return fmt.Errorf("decode %s: multiple JSON values", subject)
	case !errors.Is(err, io.EOF):
		return fmt.Errorf("decode %s: %w", subject, err)
	}
	return nil
}

// ValidateLaunchSealed verifies the persisted data that the process-boundary
// launcher relies on before it creates a child run.
func ValidateLaunchSealed(sealed *Sealed) error {
	if sealed == nil {
		return errors.New("sealed intake route is required")
	}
	if sealed.State != Frozen {
		return fmt.Errorf("intake route must be frozen (got %q)", sealed.State)
	}
	if strings.TrimSpace(sealed.ParentRunID) == "" {
		return errors.New("intake route parent run ID is required")
	}
	if strings.TrimSpace(sealed.Workflow) == "" {
		return errors.New("intake route workflow is required")
	}
	if strings.TrimSpace(sealed.SourceRef) == "" {
		return errors.New("intake route source reference is required")
	}
	if sealed.Params == nil {
		return errors.New("intake route parameters are required")
	}
	if strings.TrimSpace(sealed.Handoff) == "" {
		return errors.New("intake route handoff is required")
	}
	if len(sealed.Handoff) > MaxHandoffBytes {
		return fmt.Errorf("intake route handoff exceeds %d KiB", MaxHandoffBytes>>10)
	}
	if strings.TrimSpace(sealed.StagedAt) == "" || strings.TrimSpace(sealed.FrozenAt) == "" {
		return errors.New("intake route staging and freeze timestamps are required")
	}
	return nil
}

// Stage publishes a prepared route. It does no catalog resolution or reads of
// agent-writable input, making it safe for a caller to hold its ordering lock.
func (s *Store) Stage(prepared *Prepared) (err error) {
	if s == nil || s.path == "" {
		return errors.New("intake route store is required")
	}
	if prepared == nil {
		return errors.New("prepared intake route is no longer available")
	}
	if prepared.published {
		current, loadErr := s.Load()
		if loadErr != nil {
			return fmt.Errorf("load published intake route: %w", loadErr)
		}
		if current.State == Frozen {
			return ErrFrozen
		}
		if current.State == Staged && current.StagedAt == prepared.sealed.StagedAt {
			return nil
		}
		return errors.New("prepared intake route was published by a different store")
	}
	if prepared.discarded {
		return errors.New("prepared intake route is no longer available")
	}
	if current, loadErr := s.Load(); loadErr == nil {
		if current.State == Frozen {
			return ErrFrozen
		}
	} else if !os.IsNotExist(loadErr) {
		return fmt.Errorf("load current intake route: %w", loadErr)
	}
	prepared.sealed.Params = cloneParams(prepared.sealed.Params)
	if err := stateio.WriteJSONAtomic(s.path, &prepared.sealed); err != nil {
		return fmt.Errorf("write intake route sidecar: %w", err)
	}
	prepared.discarded = true
	prepared.published = true
	return nil
}

// Freeze atomically records the staged-to-frozen lifecycle transition.
func (s *Store) Freeze() error {
	sealed, err := s.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNoRoute
		}
		return fmt.Errorf("load intake route: %w", err)
	}
	if sealed.State == Frozen {
		return nil
	}
	if sealed.State != Staged {
		return fmt.Errorf("cannot freeze intake route in state %q", sealed.State)
	}
	sealed.State = Frozen
	sealed.FrozenAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := stateio.WriteJSONAtomic(s.path, sealed); err != nil {
		return fmt.Errorf("freeze intake route: %w", err)
	}
	return nil
}

func canonicalRunDir(runDir string) (string, error) {
	if runDir == "" {
		return "", errors.New("run directory is required")
	}
	abs, err := filepath.Abs(runDir)
	if err != nil {
		return "", fmt.Errorf("resolve run directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve run directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat run directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("run directory is not a directory: %s", resolved)
	}
	return resolved, nil
}

func readRequest(opts *ValidateOptions, runDir string) ([]byte, error) {
	if opts.Request != nil {
		if len(opts.Request) > MaxRequestBytes {
			return nil, errors.New("route request exceeds 64 KiB")
		}
		return opts.Request, nil
	}
	path := opts.RequestPath
	if path == "" {
		path = RequestPathFor(runDir)
	}
	file, err := os.Open(path) // #nosec G304 -- agent input is intentionally validated here.
	if err != nil {
		return nil, fmt.Errorf("open route request: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, MaxRequestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read route request: %w", err)
	}
	if len(data) > MaxRequestBytes {
		return nil, errors.New("route request exceeds 64 KiB")
	}
	return data, nil
}

func decodeRequest(data []byte) (Request, error) {
	var request Request
	if err := decodeSingleJSON(bytes.NewReader(data), &request, "route request"); err != nil {
		return Request{}, err
	}
	if request.Workflow == "" {
		return Request{}, errors.New("route workflow is required")
	}
	return request, nil
}

// workflowNotFoundError names the unresolved workflow and, when the catalog can
// enumerate itself, the alternatives. The submitting agent sees this inline, so
// listing the catalog turns a failed guess into a correctable choice.
func workflowNotFoundError(opts *ValidateOptions, requested string) error {
	entries := routableEntries(opts.Catalog, opts.IntakeWorkflow)
	if len(entries) == 0 {
		return fmt.Errorf("workflow %q not found", requested)
	}
	truncated := ""
	if len(entries) > maxListedWorkflows {
		truncated = fmt.Sprintf("\n  (+%d more)", len(entries)-maxListedWorkflows)
		entries = entries[:maxListedWorkflows]
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		line := "  " + entry.CanonicalName
		if description := strings.TrimSpace(entry.Description); description != "" {
			line += " - " + description
		}
		lines = append(lines, line)
	}
	return fmt.Errorf("workflow %q not found; routable workflows:\n%s%s", requested, strings.Join(lines, "\n"), truncated)
}

// routableEntries returns the catalog's routable entries minus intake itself,
// which is rejected as a self-route. A catalog that cannot enumerate itself
// yields nothing, leaving callers to fall back to names alone.
func routableEntries(catalog Catalog, intakeWorkflow string) []discovery.WorkflowEntry {
	lister, ok := catalog.(RoutableCatalog)
	if !ok {
		return nil
	}
	routable := lister.RoutableWorkflows()
	entries := make([]discovery.WorkflowEntry, 0, len(routable))
	for _, entry := range routable {
		if entry.CanonicalName == intakeWorkflow {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func validateParams(entry *discovery.WorkflowEntry, supplied map[string]string) error {
	declared := make(map[string]bool, len(entry.Params))
	for _, param := range entry.Params {
		declared[param.Name] = true
		if param.IsRequired() {
			if _, ok := supplied[param.Name]; !ok {
				return fmt.Errorf("missing required parameter %q", param.Name)
			}
		}
	}
	for name := range supplied {
		if !declared[name] {
			return fmt.Errorf("unexpected parameter %q", name)
		}
	}
	return nil
}

func cloneParams(params map[string]string) map[string]string {
	cloned := make(map[string]string, len(params))
	for key, value := range params {
		cloned[key] = value
	}
	return cloned
}

func validationFailure(code ViolationCode, cause error, workflow, parameter, path string) *ValidationError {
	return &ValidationError{
		Code:      code,
		Message:   cause.Error(),
		Workflow:  workflow,
		Parameter: parameter,
		Path:      path,
		Err:       cause,
	}
}
