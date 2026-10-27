package builtinworkflows

import (
	"bytes"
	"fmt"
	"io/fs"
	osexec "os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestValidatorBookkeepingDecisionTable(t *testing.T) {
	dir := t.TempDir()
	writeBookkeepingHelper(t, dir)
	configured := t.TempDir()
	writeValidatorConfig(t, configured)

	for _, tc := range []struct {
		name, value string
		config      bool
		want        string
	}{
		{"empty with config", "", true, "run"},
		{"empty without config", "", false, "skip"},
		{"auto with config", "auto", true, "run"},
		{"auto without config", "auto", false, "skip"},
		{"auto reference with config", "builtin:core/validation-auto-v1.0.yaml", true, "run"},
		{"auto reference without config", "builtin:core/validation-auto-v1.0.yaml", false, "skip"},
		{"agent-validator without config", "agent-validator", false, "run"},
		{"agent-validator reference", "builtin:core/validation-agent-validator-v1.0.yaml", false, "run"},
		{"none with config", "none", true, "skip"},
		{"none reference with config", "builtin:core/validation-none-v1.0.yaml", true, "skip"},
		{"custom path with config", "/tmp/custom.yaml", true, "skip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			if tc.config {
				cwd = configured
			}
			cmd := osexec.Command("sh", filepath.Join(dir, "validator-bookkeeping.sh"), tc.value)
			cmd.Dir = cwd
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if got := strings.TrimSpace(string(out)); got != tc.want {
				t.Errorf("decision = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPayloadValidationWorkflowReadsOptionalString(t *testing.T) {
	dir := t.TempDir()
	writeBookkeepingHelper(t, dir)
	for _, tc := range []struct{ name, payload, want string }{
		{"present", `{"validation_workflow":"none"}`, "none"},
		{"absent", `{"change_name":"x"}`, ""},
		{"null", `{"validation_workflow":null}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := osexec.Command("sh", "-c", `. "$1"; payload_validation_workflow "$2"`, "sh", filepath.Join(dir, "validator-bookkeeping.sh"), tc.payload)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if got := strings.TrimSpace(string(out)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidatorBookkeepingCopiesAreIdentical(t *testing.T) {
	want, err := ReadAsset("core/validator-bookkeeping.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, ns := range []string{"openspec", "spec-driven"} {
		got, err := ReadAsset(ns + "/validator-bookkeeping.sh")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s/validator-bookkeeping.sh differs from core/validator-bookkeeping.sh", ns)
		}
	}
}

type paramDecl struct {
	Name     string            `yaml:"name"`
	Default  string            `yaml:"default"`
	Required *bool             `yaml:"required"`
	Aliases  map[string]string `yaml:"aliases"`
}

type workflowDoc struct {
	Params []paramDecl `yaml:"params"`
}

func readWorkflowDoc(t *testing.T, path string) (doc workflowDoc, body string) {
	t.Helper()
	raw, err := fs.ReadFile(FS, path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc, string(raw)
}

// The selection param is declared identically wherever a top-level workflow
// takes it, and the retired skip_validator flag is gone from current versions.
func TestTopLevelWorkflowsDeclareValidationWorkflowIdentically(t *testing.T) {
	top := []string{
		"core/implement-change-v3.0.yaml",
		"openspec/change-v3.0.yaml",
		"openspec/simple-change-v2.0.yaml",
		"spec-driven/change-v3.0.yaml",
		"spec-driven/simple-change-v2.0.yaml",
		"core/complete-simple-change-v2.0.yaml",
	}
	var first *paramDecl
	for _, path := range top {
		doc, body := readWorkflowDoc(t, path)
		var found *paramDecl
		for i := range doc.Params {
			if doc.Params[i].Name == "validation_workflow" {
				found = &doc.Params[i]
			}
			if doc.Params[i].Name == "skip_validator" {
				t.Errorf("%s still declares skip_validator", path)
			}
		}
		if found == nil {
			t.Errorf("%s does not declare validation_workflow", path)
			continue
		}
		if strings.Contains(body, "skip_validator") {
			t.Errorf("%s still mentions skip_validator", path)
		}
		if found.Default != "auto" {
			t.Errorf("%s default = %q, want auto", path, found.Default)
		}
		if first == nil {
			first = found
			continue
		}
		if !mapsEqual(first.Aliases, found.Aliases) {
			t.Errorf("%s aliases = %v, differ from %v", path, found.Aliases, first.Aliases)
		}
	}
}

// Every embedded file that declares validation_workflow declares it the same
// way, and every current top-level workflow is among them.
func TestEveryEmbeddedValidationWorkflowParamIsIdentical(t *testing.T) {
	var first *paramDecl
	declaring := map[string]bool{}
	bound := map[string]bool{}
	for _, ns := range []string{"core", "openspec", "spec-driven", "onboarding"} {
		entries, err := fs.ReadDir(FS, ns)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			a := entry.Name()
			if !strings.HasSuffix(a, ".yaml") {
				continue
			}
			doc, _ := readWorkflowDoc(t, ns+"/"+a)
			for i := range doc.Params {
				p := doc.Params[i]
				if p.Name != "validation_workflow" {
					continue
				}
				if len(p.Aliases) == 0 {
					// Only the three bookkeeping helpers take the already-bound value.
					bound[ns+"/"+a] = true
					continue
				}
				declaring[ns+"/"+a] = true
				if p.Default != "auto" {
					t.Errorf("%s/%s default = %q, want auto", ns, a, p.Default)
				}
				if first == nil {
					first = &p
				} else if !mapsEqual(first.Aliases, p.Aliases) {
					t.Errorf("%s/%s aliases differ: %v", ns, a, p.Aliases)
				}
			}
		}
	}
	wantBound := map[string]bool{
		"core/commit-change-plan-v1.0.yaml": true, "core/commit-definition-v1.0.yaml": true, "openspec/archive-change-v1.0.yaml": true,
	}
	if diff := cmp.Diff(wantBound, bound); diff != "" {
		t.Errorf("declarations without aliases (-want +got):\n%s", diff)
	}
	for _, want := range []string{
		"openspec/change-v3.0.yaml", "spec-driven/change-v3.0.yaml", "openspec/simple-change-v2.0.yaml",
		"spec-driven/simple-change-v2.0.yaml", "openspec/scaffold-v1.0.yaml", "spec-driven/scaffold-v1.0.yaml",
		"core/implement-change-v3.0.yaml", "core/verify-change-v2.0.yaml", "core/accept-change-v2.0.yaml",
		"core/finalize-pr-v2.0.yaml", "core/complete-simple-change-v2.0.yaml",
	} {
		if !declaring[want] {
			t.Errorf("%s does not declare validation_workflow with aliases", want)
		}
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestImplementTaskV2HasNoValidatorSteps(t *testing.T) {
	doc, body := readWorkflowDoc(t, "core/implement-task-v2.0.yaml")
	for _, p := range doc.Params {
		if p.Name == "task_compliance" || p.Name == "skip_validator" || p.Name == "validation_workflow" {
			t.Errorf("implement-task-v2.0 declares %s", p.Name)
		}
	}
	for _, banned := range []string{"run-validator", "validator-retry", "fix-violations", "check-task-compliance"} {
		if strings.Contains(body, banned) {
			t.Errorf("implement-task-v2.0 mentions %s", banned)
		}
	}
}

// Current workflows reach Agent Validator only through a validation workflow.
// The walk starts at every current top-level workflow, follows each static
// sub-workflow reference (a {{validation_workflow}} target is the allowed way
// in), and checks every reached file and every script a reached step runs.
func TestCurrentWorkflowsInvokeValidatorOnlyInValidationWorkflows(t *testing.T) {
	roots := []string{
		"openspec/change-v3.0.yaml", "openspec/simple-change-v2.0.yaml", "openspec/scaffold-v1.0.yaml",
		"spec-driven/change-v3.0.yaml", "spec-driven/simple-change-v2.0.yaml", "spec-driven/scaffold-v1.0.yaml",
		"core/implement-change-v3.0.yaml",
	}
	// Steps that set Agent Validator up on request, and the gated bookkeeping
	// scripts, may name it.
	setupSteps := map[string]bool{"validator-choice": true, "validator-init": true, "validator-setup": true, "validator-skipped-guidance": true}
	gatedScripts := map[string]bool{
		"create-change.sh": true, "commit-change-plan.sh": true, "advance-validator-baseline.sh": true,
		"guard-clean-tree.sh": true, "validator-bookkeeping.sh": true,
	}
	idPattern := regexp.MustCompile(`^\s*- id: (\S+)`)
	scriptPattern := regexp.MustCompile(`^\s*script: (\S+)`)
	refPattern := regexp.MustCompile(`^\s*workflow: "?([^"\s]+)"?`)
	invokePattern := regexp.MustCompile(`agent-validator (run|check|review)\b`)

	seen := map[string]bool{}
	var walk func(path string)
	walk = func(path string) {
		if seen[path] {
			return
		}
		seen[path] = true
		_, body := readWorkflowDoc(t, path)
		step := ""
		for n, line := range strings.Split(body, "\n") {
			if m := idPattern.FindStringSubmatch(line); m != nil {
				step = m[1]
			}
			where := fmt.Sprintf("%s:%d", path, n+1)
			if m := scriptPattern.FindStringSubmatch(line); m != nil {
				name := m[1]
				if name == "run-validator.sh" {
					t.Errorf("%s runs the validator script directly", where)
				}
				if !gatedScripts[name] && scriptMentionsValidator(t, path, name) {
					t.Errorf("%s runs %s, which invokes Agent Validator outside the gated set", where, name)
				}
			}
			if m := refPattern.FindStringSubmatch(line); m != nil {
				target := m[1]
				switch {
				case strings.Contains(target, "{{"):
				case strings.Contains(target, "run-validator"):
					t.Errorf("%s composes the validator helper %s", where, target)
				default:
					walk(pathpkg.Join(pathpkg.Dir(path), target))
				}
			}
			lower := strings.ToLower(line)
			if setupSteps[step] || !invokePattern.MatchString(line) {
				continue
			}
			if strings.Contains(lower, "do not") || strings.Contains(lower, "never") {
				continue
			}
			t.Errorf("%s runs Agent Validator directly: %s", where, strings.TrimSpace(line))
		}
	}
	for _, root := range roots {
		walk(root)
	}
	if len(seen) < 10 {
		t.Errorf("walk reached only %d files: %v", len(seen), seen)
	}
}

// scriptMentionsValidator reports whether the named script, in the workflow's
// namespace or core, invokes the validator CLI.
func scriptMentionsValidator(t *testing.T, workflowPath, script string) bool {
	t.Helper()
	for _, dir := range []string{pathpkg.Dir(workflowPath), "core"} {
		body, err := fs.ReadFile(FS, pathpkg.Join(dir, script))
		if err == nil {
			return regexp.MustCompile(`agent-validator\s+(run|check|review|detect|skip)\b`).Match(body)
		}
	}
	t.Fatalf("script %s not found for %s", script, workflowPath)
	return false
}

func TestValidationBuiltinsAcceptOnlyNamedRepairSessions(t *testing.T) {
	for _, name := range []string{"auto", "agent-validator", "none"} {
		doc, _ := readWorkflowDoc(t, "core/validation-"+name+"-v1.0.yaml")
		var seen bool
		for _, p := range doc.Params {
			if p.Name == "repair_session" {
				seen = true
				if p.Default != "lead-agent" {
					t.Errorf("validation-%s repair_session default = %q", name, p.Default)
				}
			}
		}
		if !seen {
			t.Errorf("validation-%s lacks repair_session", name)
		}
	}
}
