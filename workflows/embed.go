package builtinworkflows

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/codagent/agent-runner/internal/workflowcatalog"
)

const RefPrefix = "builtin:"

const (
	// IntakeCanonicalName is the catalog name of the built-in intake workflow.
	IntakeCanonicalName = "core:intake"
	// IntakeStepID is the intake workflow's route-eligible step.
	IntakeStepID = "plan"

	intakeRelPath = "core/intake-v1.0.yaml"
)

// IntakeRef returns the builtin reference of the intake workflow. Route
// eligibility, the submit_route reservation, and the interactive-terminal gate
// are three independent checks keyed off this one fact, so they resolve it from
// here rather than each spelling out the path. A version bump then moves all of
// them together instead of silently disabling one.
func IntakeRef() string { return Ref(intakeRelPath) }

// IsIntakeRef reports whether workflowFile is the built-in intake workflow.
func IsIntakeRef(workflowFile string) bool { return workflowFile == IntakeRef() }

// FS contains the builtin workflows embedded at build time from the repository's
// workflows/ directory. The `all:` prefix is required so that `_group.yaml`
// namespace metadata files are embedded — Go's default embed behaviour excludes
// any file whose basename begins with `_` or `.`.
//
//go:embed all:*
var FS embed.FS

func IsRef(workflowFile string) bool {
	return strings.HasPrefix(workflowFile, RefPrefix)
}

func Ref(relPath string) string {
	return RefPrefix + path.Clean(relPath)
}

func RefPath(workflowFile string) (string, error) {
	if !IsRef(workflowFile) {
		return "", fmt.Errorf("not a builtin workflow reference: %s", workflowFile)
	}
	relPath := path.Clean(strings.TrimPrefix(workflowFile, RefPrefix))
	if relPath == "." || strings.HasPrefix(relPath, "../") || path.IsAbs(relPath) {
		return "", fmt.Errorf("invalid builtin workflow reference: %s", workflowFile)
	}
	return relPath, nil
}

func Resolve(name string) (string, error) {
	return resolveFS(FS, name)
}

func resolveFS(fsys fs.FS, name string) (string, error) {
	ns, workflowName, ok := strings.Cut(name, ":")
	if !ok || ns == "" || workflowName == "" {
		return "", fmt.Errorf("workflow %q not found", name)
	}
	if isMetadataBasename(workflowName) {
		return "", fmt.Errorf("workflow %q not found", name)
	}

	paths, err := definitionPaths(fsys)
	if err != nil {
		return "", err
	}
	group, found := workflowcatalog.Build(paths).Lookup(path.Join(ns, workflowName))
	if !found {
		return "", fmt.Errorf("workflow %q not found", name)
	}
	if group.Err != nil {
		return "", group.Err
	}
	if group.Selected == nil {
		return "", fmt.Errorf("workflow %q not found", name)
	}
	return Ref(group.Selected.Path), nil
}

// isMetadataBasename reports whether the basename of name starts with `_`,
// matching the convention used by List() to skip namespace metadata files.
func isMetadataBasename(name string) bool {
	return strings.HasPrefix(path.Base(name), "_")
}

func ReadFile(workflowFile string) ([]byte, error) {
	relPath, err := RefPath(workflowFile)
	if err != nil {
		return nil, err
	}
	data, err := FS.ReadFile(relPath)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func ListAssets(namespace string) ([]string, error) {
	if namespace == "" || namespace == "." || namespace == ".." || strings.Contains(namespace, "/") || strings.Contains(namespace, `\`) {
		return nil, fmt.Errorf("invalid builtin workflow namespace: %s", namespace)
	}
	var assets []string
	err := fs.WalkDir(FS, namespace, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("workflow namespace %q not found", namespace)
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := path.Ext(p)
		if ext == ".yaml" || ext == ".yml" {
			return nil
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, namespace), "/")
		assets = append(assets, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return assets, nil
}

func ReadAsset(assetPath string) ([]byte, error) {
	clean := path.Clean(assetPath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return nil, fmt.Errorf("invalid builtin asset path: %s", assetPath)
	}
	ext := path.Ext(clean)
	if ext == ".yaml" || ext == ".yml" {
		return nil, fmt.Errorf("builtin asset path is a workflow: %s", assetPath)
	}
	return FS.ReadFile(clean)
}

func List() ([]string, error) {
	return listFS(FS)
}

func listFS(fsys fs.FS) ([]string, error) {
	paths, err := definitionPaths(fsys)
	if err != nil {
		return nil, err
	}
	refs := make([]string, len(paths))
	for index, candidatePath := range paths {
		refs[index] = Ref(candidatePath)
	}
	return refs, nil
}

func definitionPaths(fsys fs.FS) ([]string, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isMetadataBasename(p) || !strings.Contains(p, "/") {
			return nil
		}
		ext := path.Ext(p)
		if ext == ".yaml" || ext == ".yml" {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}
