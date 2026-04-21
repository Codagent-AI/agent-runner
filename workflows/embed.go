package builtinworkflows

import (
	"embed"
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

func isNotExist(err error) bool {
	return err == nil || err == fs.ErrNotExist || strings.Contains(err.Error(), "file does not exist")
}
