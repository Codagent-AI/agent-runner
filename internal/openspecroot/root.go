// Package openspecroot resolves the OpenSpec project independently of the code workspace.
package openspecroot

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Input struct {
	SpecExternal string `json:"spec_external"`
	ChangeName   string `json:"change_name"`
	SpecRoot     string `json:"spec_root"`
	Operation    string `json:"operation"`
}

func canonical(wd, path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(wd, path)
	}
	return filepath.EvalSymlinks(path)
}

// RepositoryKey identifies the main worktree, including when invoked in a linked worktree.
func RepositoryKey(wd string) string {
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = wd
	out, err := cmd.Output()
	if err != nil {
		return wd
	}
	common := strings.TrimSpace(string(out))
	key := filepath.Dir(common)
	if filepath.Base(common) != ".git" {
		cmd = exec.Command("git", "rev-parse", "--show-toplevel")
		cmd.Dir = wd
		out, err = cmd.Output()
		if err != nil {
			return wd
		}
		key = strings.TrimSpace(string(out))
	}
	if path, err := canonical(wd, key); err == nil {
		return path
	}
	return wd
}

// Resolve returns string-valued captures suitable for durable workflow state.
func Resolve(wd string, input Input, roots map[string]string) (map[string]string, error) {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(input.ChangeName) {
		return nil, fmt.Errorf("invalid change_name %q", input.ChangeName)
	}
	if input.Operation != "create" && input.Operation != "continue" && input.Operation != "guard" {
		return nil, fmt.Errorf("invalid operation %q", input.Operation)
	}
	if input.Operation == "guard" && input.SpecExternal == "true" && input.SpecRoot == "" {
		return nil, fmt.Errorf("recorded OpenSpec root context is missing for external archive")
	}
	wd, err := canonical(wd, wd)
	if err != nil {
		return nil, err
	}
	root, source := selectRoot(wd, input.SpecRoot, roots)
	resolved, err := canonical(wd, root)
	if err != nil {
		if input.Operation == "guard" && source == "setting" {
			return nil, fmt.Errorf("recorded OpenSpec root context is missing (setting: %s): %w", root, err)
		}
		return nil, fmt.Errorf("OpenSpec root %q from %s does not exist or cannot be resolved: %w", root, source, err)
	}
	external := resolved != wd
	if external {
		if input.Operation == "guard" && input.SpecRoot == "" {
			return nil, fmt.Errorf("recorded OpenSpec root context is missing (setting: %s)", resolved)
		}

		for _, p := range []string{resolved, filepath.Join(resolved, "openspec")} {
			info, e := os.Stat(p)
			if e != nil || !info.IsDir() {
				return nil, fmt.Errorf("OpenSpec root %q from %s is not an OpenSpec project: missing directory %s", resolved, source, p)
			}
		}
	}
	change := filepath.Join("openspec", "changes", input.ChangeName)
	result := map[string]string{"code_root": wd, "spec_root": resolved, "external": "false", "source": source, "spec_root_input": root, "change_dir": change, "change_dir_absolute": filepath.Join(resolved, change), "commit_plan": "true", "context_instruction": "", "location_instruction": fmt.Sprintf("Keep every OpenSpec definition and planning artifact under the repository-local `%s/` directory.", change), "validate_instruction": fmt.Sprintf("When an approved artifact changed, run `openspec validate --type change %q`.", input.ChangeName)}
	result["accept_validate_instruction"] = fmt.Sprintf("If a specification changed, run `openspec validate --type change %q`.", input.ChangeName)
	result["simple_validate_instruction"] = fmt.Sprintf("Validate OpenSpec change %q with `openspec validate --type change %q`.", input.ChangeName, input.ChangeName)
	result["validation_failure_instruction"] = fmt.Sprintf("`openspec validate --type change %q` failed for OpenSpec change %q.", input.ChangeName, input.ChangeName)
	if !external {
		return result, nil
	}
	change = filepath.Join(resolved, change)
	if err := validateChange(resolved, change, input); err != nil {
		return nil, err
	}
	result["external"], result["commit_plan"], result["change_dir"] = "true", "false", change
	result["location_instruction"] = fmt.Sprintf("Keep every OpenSpec definition and planning artifact under %#q in the OpenSpec project at %#q.", change+"/", resolved)
	result["validate_instruction"] = fmt.Sprintf("When an approved artifact changed, run `openspec validate --type change %q` from %#q.", input.ChangeName, resolved)
	result["accept_validate_instruction"] = fmt.Sprintf("If a specification changed, run `openspec validate --type change %q` from %#q.", input.ChangeName, resolved)
	result["simple_validate_instruction"] = fmt.Sprintf("Validate OpenSpec change %q with `openspec validate --type change %q` run from %#q.", input.ChangeName, input.ChangeName, resolved)
	result["validation_failure_instruction"] = fmt.Sprintf("`openspec validate --type change %q` run from %#q failed for OpenSpec change %q.", input.ChangeName, resolved, input.ChangeName)
	result["context_instruction"] = externalContext(wd, resolved, change)
	return result, nil
}

func validateChange(resolved, change string, input Input) error {
	if input.Operation == "create" {
		if _, e := os.Stat(change); !os.IsNotExist(e) {
			return fmt.Errorf("change %q in %s already exists at %s", input.ChangeName, resolved, change)
		}
		matches, e := filepath.Glob(filepath.Join(resolved, "openspec", "changes", "archive", "[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]-"+input.ChangeName))
		if e != nil {
			return e
		}
		if len(matches) > 0 {
			return fmt.Errorf("change %q in %s already archived at %s", input.ChangeName, resolved, matches[0])
		}
	}
	if input.Operation == "continue" {
		info, e := os.Stat(change)
		if e != nil || !info.IsDir() {
			return fmt.Errorf("change %q in %s requires active directory %s", input.ChangeName, resolved, change)
		}
	}

	return nil
}

func selectRoot(wd, param string, roots map[string]string) (root, source string) {
	key := RepositoryKey(wd)
	root, source = param, "param"
	if root == "" {
		keys := make([]string, 0, len(roots))
		for k := range roots {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if normalized, e := canonical(wd, k); e == nil && normalized == key {
				root = roots[k]
				break
			}
		}
		source = "setting"
		if root == "" {
			root, source = wd, "default"
		}
	}
	return root, source
}

func externalContext(wd, resolved, change string) string {
	context := fmt.Sprintf("The code root is %#q; the OpenSpec spec root is %#q. Definition, planning, task and acceptance artifacts belong under %#q. Code changes, code validation and commits belong in the code root. Never stage, commit or push spec-root files. Never write the spec-root path or paths under it into commit messages, the PR title or body, or any file in the code repository.", wd, resolved, change+"/")
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "openspec/config.yaml"} {
		p := filepath.Join(resolved, name)
		if info, e := os.Stat(p); e == nil && !info.IsDir() {
			context += fmt.Sprintf(" Read and follow %#q as the spec project's working rules.", p)
		}
	}
	return context
}
