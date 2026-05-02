package builtinworkflows

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

const RefPrefix = "builtin:"

// FS contains the builtin workflows embedded at build time from the repository's
// workflows/ directory.
//
//go:embed *
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
	ns, workflowName, ok := strings.Cut(name, ":")
	if !ok || ns == "" || workflowName == "" {
		return "", fmt.Errorf("workflow %q not found", name)
	}
	for _, ext := range []string{".yaml", ".yml"} {
		candidate := path.Join(ns, workflowName+ext)
		info, err := fs.Stat(FS, candidate)
		if err == nil {
			if info.IsDir() {
				continue
			}
			return Ref(candidate), nil
		}
		if !isNotExist(err) {
			return "", fmt.Errorf("workflow %q not found", name)
		}
	}
	return "", fmt.Errorf("workflow %q not found", name)
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
	if namespace == "" || namespace == "." || strings.Contains(namespace, "/") || strings.Contains(namespace, `\`) {
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
	if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return nil, fmt.Errorf("invalid builtin asset path: %s", assetPath)
	}
	ext := path.Ext(clean)
	if ext == ".yaml" || ext == ".yml" {
		return nil, fmt.Errorf("builtin asset path is a workflow: %s", assetPath)
	}
	return FS.ReadFile(clean)
}

func List() ([]string, error) {
	var refs []string
	err := fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := path.Ext(p)
		if ext == ".yaml" || ext == ".yml" {
			refs = append(refs, Ref(p))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return refs, nil
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
