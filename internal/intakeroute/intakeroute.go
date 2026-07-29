// Package intakeroute validates and persists run-owned intake route sidecars.
package intakeroute

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/stateio"
)

const (
	// MaxRequestBytes bounds agent-written route requests.
	MaxRequestBytes = 64 << 10
	// MaxHandoffBytes bounds the handoff snapshot retained in a run.
	MaxHandoffBytes = 1 << 20

	sidecarName = "intake-route.json"
)

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
	HandoffPath string            `json:"handoff_path"`
	StagedAt    string            `json:"staged_at"`
	FrozenAt    string            `json:"frozen_at,omitempty"`
}

// Catalog resolves canonical workflow names through the normal discovery result.
// Callers should construct it from discovery.Enumerate rather than interpreting
// workflow names themselves.
type Catalog interface {
	ResolveWorkflow(string) (discovery.WorkflowEntry, error)
}

// CatalogFunc adapts a resolver function into a Catalog.
type CatalogFunc func(string) (discovery.WorkflowEntry, error)

func (f CatalogFunc) ResolveWorkflow(name string) (discovery.WorkflowEntry, error) { return f(name) }

// NewCatalog returns a catalog backed by entries produced by discovery.Enumerate.
func NewCatalog(entries []discovery.WorkflowEntry) Catalog {
	byName := make(map[string]discovery.WorkflowEntry, len(entries))
	for _, entry := range entries {
		byName[entry.CanonicalName] = entry
	}
	return CatalogFunc(func(name string) (discovery.WorkflowEntry, error) {
		entry, ok := byName[name]
		if !ok || entry.ParseError != "" || entry.SourcePath == "" {
			return discovery.WorkflowEntry{}, ErrWorkflowNotFound
		}
		return entry, nil
	})
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

// Prepared is a validated route whose handoff has been copied to a temporary,
// unpublished run-owned path. It must be staged or discarded.
type Prepared struct {
	sealed        Sealed
	temporaryPath string
	discarded     bool
	published     bool
}

// Sealed returns a copy of the metadata that will be published. HandoffPath is
// intentionally empty until Stage assigns the final run-owned snapshot path.
func (p *Prepared) Sealed() Sealed {
	if p == nil {
		return Sealed{}
	}
	sealed := p.sealed
	sealed.Params = cloneParams(p.sealed.Params)
	return sealed
}

// Discard removes the unpublished snapshot. It is safe to call repeatedly and
// does nothing after successful publication.
func (p *Prepared) Discard() error {
	if p == nil || p.discarded || p.temporaryPath == "" {
		return nil
	}
	err := os.Remove(p.temporaryPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("discard temporary handoff snapshot: %w", err)
	}
	p.discarded = true
	p.temporaryPath = ""
	return nil
}

// Validate resolves and validates a route request, then copies the handoff from
// the same validated open handle into an unpublished temporary snapshot.
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
			return nil, validationFailure(ViolationWorkflowResolution, fmt.Errorf("workflow %q not found", request.Workflow), request.Workflow, "", "")
		}
		return nil, validationFailure(ViolationWorkflowResolution, fmt.Errorf("resolve workflow %q: %w", request.Workflow, err), request.Workflow, "", "")
	}
	if entry.CanonicalName == "" || entry.SourcePath == "" || entry.ParseError != "" {
		return nil, validationFailure(ViolationWorkflowResolution, fmt.Errorf("workflow %q not found", request.Workflow), request.Workflow, "", "")
	}
	if request.Workflow == opts.IntakeWorkflow {
		return nil, validationFailure(ViolationSelfRoute, errors.New("intake cannot route to itself"), request.Workflow, "", "")
	}
	if err := validateParams(&entry, request.Params); err != nil {
		return nil, validationFailure(ViolationParameter, err, entry.CanonicalName, "", "")
	}
	temporaryPath, err := snapshotHandoff(runDir, request.Handoff)
	if err != nil {
		return nil, validationFailure(ViolationHandoff, err, entry.CanonicalName, "", request.Handoff)
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
			StagedAt:    now().UTC().Format(time.RFC3339Nano),
		},
		temporaryPath: temporaryPath,
	}, nil
}

// Store owns the sidecar for one run directory.
type Store struct{ path string }

// NewStore creates a sidecar store rooted in runDir.
func NewStore(runDir string) *Store { return &Store{path: filepath.Join(runDir, sidecarName)} }

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
		if current.State == Staged && current.HandoffPath == prepared.sealed.HandoffPath {
			return nil
		}
		return errors.New("prepared intake route was published by a different store")
	}
	if prepared.discarded || prepared.temporaryPath == "" {
		return errors.New("prepared intake route is no longer available")
	}
	defer func() {
		if err != nil {
			_ = prepared.Discard()
		}
	}()
	var previous *Sealed
	if current, loadErr := s.Load(); loadErr == nil {
		if current.State == Frozen {
			return ErrFrozen
		}
		previous = current
	} else if !os.IsNotExist(loadErr) {
		return fmt.Errorf("load current intake route: %w", loadErr)
	}

	finalPath, err := snapshotPath(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	if err := os.Rename(prepared.temporaryPath, finalPath); err != nil {
		return fmt.Errorf("publish handoff snapshot: %w", err)
	}
	prepared.temporaryPath = ""
	prepared.sealed.HandoffPath = finalPath
	prepared.sealed.Params = cloneParams(prepared.sealed.Params)
	if err := stateio.WriteJSONAtomic(s.path, &prepared.sealed); err != nil {
		_ = os.Remove(finalPath)
		return fmt.Errorf("write intake route sidecar: %w", err)
	}
	prepared.discarded = true
	prepared.published = true
	if previous != nil && previous.HandoffPath != finalPath {
		removeOwnedSnapshot(filepath.Dir(s.path), previous.HandoffPath)
	}
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
		path = filepath.Join(runDir, "route-request.json")
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
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("decode route request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Request{}, errors.New("decode route request: multiple JSON values")
	}
	if request.Workflow == "" {
		return Request{}, errors.New("route workflow is required")
	}
	if request.Handoff == "" {
		return Request{}, errors.New("route handoff is required")
	}
	return request, nil
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

func snapshotHandoff(runDir, handoff string) (string, error) {
	path := handoff
	if !filepath.IsAbs(path) {
		path = filepath.Join(runDir, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve handoff: %w", err)
	}
	// The run directory was canonicalized by Validate. Canonicalize the
	// candidate before the lexical containment check as well (notably /var is
	// a symlink to /private/var on macOS).
	if resolvedPath, resolveErr := filepath.EvalSymlinks(absPath); resolveErr == nil {
		absPath = resolvedPath
	}
	if !within(runDir, filepath.Clean(absPath)) {
		return "", errors.New("handoff must live inside the run directory")
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("open handoff: %w", err)
	}
	if !within(runDir, resolved) {
		return "", errors.New("handoff must live inside the run directory")
	}
	input, err := os.Open(resolved) // #nosec G304 -- validated route handoff within the run directory.
	if err != nil {
		return "", fmt.Errorf("open handoff: %w", err)
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil {
		return "", fmt.Errorf("stat handoff: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("handoff must be a regular file")
	}
	if info.Size() == 0 {
		return "", errors.New("handoff is empty")
	}
	if info.Size() > MaxHandoffBytes {
		return "", errors.New("handoff exceeds 1 MiB")
	}
	temporary, err := os.CreateTemp(runDir, ".intake-route-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary handoff snapshot: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("secure temporary handoff snapshot: %w", err)
	}
	written, err := io.Copy(temporary, io.LimitReader(input, MaxHandoffBytes+1))
	if err != nil || written > MaxHandoffBytes {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		if written > MaxHandoffBytes {
			return "", errors.New("handoff exceeds 1 MiB")
		}
		return "", fmt.Errorf("copy handoff snapshot: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("close temporary handoff snapshot: %w", err)
	}
	return temporaryPath, nil
}

func snapshotPath(runDir string) (string, error) {
	temporary, err := os.CreateTemp(runDir, "intake-handoff-*.md")
	if err != nil {
		return "", fmt.Errorf("reserve handoff snapshot path: %w", err)
	}
	path := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("reserve handoff snapshot path: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("reserve handoff snapshot path: %w", err)
	}
	return path, nil
}

func within(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
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

func removeOwnedSnapshot(runDir, snapshot string) {
	runDir, err := filepath.Abs(runDir)
	if err != nil {
		return
	}
	snapshot, err = filepath.Abs(snapshot)
	if err != nil || filepath.Dir(snapshot) != filepath.Clean(runDir) {
		return
	}
	base := filepath.Base(snapshot)
	if !strings.HasPrefix(base, "intake-handoff-") || !strings.HasSuffix(base, ".md") {
		return
	}
	_ = os.Remove(snapshot)
}
