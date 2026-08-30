package builtinworkflows

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/codagent/agent-runner/internal/model"
	"gopkg.in/yaml.v3"
)

func TestOnboardingWorkflowsResolveAndAssetsList(t *testing.T) {
	onboarding, err := Resolve("onboarding:onboarding")
	if err != nil {
		t.Fatalf("Resolve(onboarding:onboarding) returned error: %v", err)
	}
	if onboarding != "builtin:onboarding/onboarding-v1.0.yaml" {
		t.Fatalf("onboarding ref = %q", onboarding)
	}
	demo, err := Resolve("onboarding:step-types-demo")
	if err != nil {
		t.Fatalf("Resolve(onboarding:step-types-demo) returned error: %v", err)
	}
	if demo != "builtin:onboarding/step-types-demo-v1.0.yaml" {
		t.Fatalf("demo ref = %q", demo)
	}
	guided, err := Resolve("onboarding:guided-workflow")
	if err != nil {
		t.Fatalf("Resolve(onboarding:guided-workflow) returned error: %v", err)
	}
	if guided != "builtin:onboarding/guided-workflow-v1.0.yaml" {
		t.Fatalf("guided ref = %q", guided)
	}
	validator, err := Resolve("onboarding:validator")
	if err != nil {
		t.Fatalf("Resolve(onboarding:validator) returned error: %v", err)
	}
	if validator != "builtin:onboarding/validator-v1.0.yaml" {
		t.Fatalf("validator ref = %q", validator)
	}
	advanced, err := Resolve("onboarding:advanced")
	if err != nil {
		t.Fatalf("Resolve(onboarding:advanced) returned error: %v", err)
	}
	if advanced != "builtin:onboarding/advanced-v1.0.yaml" {
		t.Fatalf("advanced ref = %q", advanced)
	}
	help, err := Resolve("onboarding:help")
	if err != nil {
		t.Fatalf("Resolve(onboarding:help) returned error: %v", err)
	}
	if help != "builtin:onboarding/help-v1.0.yaml" {
		t.Fatalf("help ref = %q", help)
	}

	assets, err := ListAssets("onboarding")
	if err != nil {
		t.Fatalf("ListAssets(onboarding) returned error: %v", err)
	}
	for _, removed := range []string{"onboarding:welcome", "onboarding:setup-agent-profile"} {
		if ref, err := Resolve(removed); err == nil {
			t.Fatalf("Resolve(%s) = %q, want not found", removed, ref)
		}
	}
	for _, want := range []string{"docs/agent-runner-basics.md"} {
		if !slices.Contains(assets, want) {
			t.Fatalf("asset %q not found in %v", want, assets)
		}
		body, err := ReadAsset("onboarding/" + want)
		if err != nil {
			t.Fatalf("ReadAsset(%s) returned error: %v", want, err)
		}
		if len(body) == 0 {
			t.Fatalf("asset %s is empty", want)
		}
	}
	for _, removed := range []string{
		"check-collisions.sh",
		"count-list.sh",
		"detect-adapters.sh",
		"echo-value.sh",
		"format-list.sh",
		"models-for-cli.sh",
		"write-profile.sh",
	} {
		if slices.Contains(assets, removed) {
			t.Fatalf("removed setup asset %q still embedded in onboarding namespace assets %v", removed, assets)
		}
	}
}

func TestResolveSelectsLatestVersionWhileExactReadsRemainAvailable(t *testing.T) {
	ref, err := Resolve("openspec:change")
	if err != nil {
		t.Fatalf("Resolve(openspec:change): %v", err)
	}
	if ref != "builtin:openspec/change-v2.0.yaml" {
		t.Fatalf("resolved ref = %q, want latest v2.0", ref)
	}

	if _, err := ReadFile("builtin:openspec/change-v1.0.yaml"); err != nil {
		t.Fatalf("ReadFile exact older version: %v", err)
	}
	latest, err := ReadFile(ref)
	if err != nil {
		t.Fatalf("ReadFile latest version: %v", err)
	}
	if strings.Contains(string(latest), "openspec:change2") {
		t.Fatalf("latest openspec:change still contains legacy openspec:change2 user-facing text")
	}
	if legacy, err := Resolve("openspec:change2"); err == nil {
		t.Fatalf("Resolve(openspec:change2) = %q, want not found", legacy)
	}

	refs, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, want := range []string{
		"builtin:openspec/change-v1.0.yaml",
		"builtin:openspec/change-v2.0.yaml",
	} {
		if !slices.Contains(refs, want) {
			t.Fatalf("List() missing %q from %v", want, refs)
		}
	}
}

func TestOpenSpecAndSpecDrivenChangeGenerationsAreEmbedded(t *testing.T) {
	refs, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, logicalName := range []string{"change", "plan-change", "implement-change", "simple-change"} {
		for _, namespace := range []string{"openspec", "spec-driven"} {
			for _, version := range []string{"v1.0", "v2.0"} {
				want := "builtin:" + namespace + "/" + logicalName + "-" + version + ".yaml"
				if !slices.Contains(refs, want) {
					t.Errorf("List() missing %q", want)
				}
			}
		}
	}

	ref, err := Resolve("spec-driven:change")
	if err != nil {
		t.Fatalf("Resolve(spec-driven:change): %v", err)
	}
	if ref != "builtin:spec-driven/change-v2.0.yaml" {
		t.Fatalf("Resolve(spec-driven:change) = %q, want spec-driven v2.0", ref)
	}

	ref, err = Resolve("spec-driven:simple-change")
	if err != nil {
		t.Fatalf("Resolve(spec-driven:simple-change): %v", err)
	}
	if ref != "builtin:spec-driven/simple-change-v2.0.yaml" {
		t.Fatalf("Resolve(spec-driven:simple-change) = %q, want spec-driven v2.0", ref)
	}
}

func TestV2ChangeWorkflowsShareCoreLifecyclePhases(t *testing.T) {
	for _, logicalName := range []string{
		"define-change",
		"plan-change",
		"review-tasks",
		"implement-change",
		"accept-change",
		"validate-feature-branch",
	} {
		ref := "builtin:core/" + logicalName + "-v1.0.yaml"
		body, err := ReadFile(ref)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", ref, err)
		}
		for _, forbidden := range []string{"openspec/changes/", "OpenSpec change", "openspec validate"} {
			if strings.Contains(string(body), forbidden) {
				t.Errorf("%s contains provider-specific text %q", ref, forbidden)
			}
		}
	}

	tests := []struct {
		ref         string
		wantArchive bool
	}{
		{ref: "builtin:openspec/change-v2.0.yaml", wantArchive: true},
		{ref: "builtin:spec-driven/change-v2.0.yaml", wantArchive: false},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			body, err := ReadFile(tt.ref)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", tt.ref, err)
			}
			var workflow struct {
				Params []struct {
					Name     string `yaml:"name"`
					Required *bool  `yaml:"required"`
				} `yaml:"params"`
				Steps []struct {
					ID       string            `yaml:"id"`
					Workflow string            `yaml:"workflow"`
					Params   map[string]string `yaml:"params"`
				} `yaml:"steps"`
			}
			if err := yaml.Unmarshal(body, &workflow); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.ref, err)
			}
			if len(workflow.Params) == 0 || workflow.Params[0].Name != "change_name" ||
				workflow.Params[0].Required == nil || !*workflow.Params[0].Required {
				t.Fatalf("%s must require change_name, got %#v", tt.ref, workflow.Params)
			}

			var hasArchive bool
			for _, step := range workflow.Steps {
				switch step.ID {
				case "validate-feature-branch":
					if step.Workflow != "../core/validate-feature-branch-v1.0.yaml" {
						t.Errorf("%s branch guard workflow = %q", tt.ref, step.Workflow)
					}
				case "accept":
					if step.Workflow != "../core/accept-change-v1.0.yaml" {
						t.Errorf("%s accept workflow = %q", tt.ref, step.Workflow)
					}
					if step.Params["change_dir"] == "" {
						t.Errorf("%s accept step does not pass change_dir", tt.ref)
					}
				case "archive":
					hasArchive = true
				}
			}
			if hasArchive != tt.wantArchive {
				t.Fatalf("%s archive presence = %t, want %t", tt.ref, hasArchive, tt.wantArchive)
			}
		})
	}
}

func TestV2NamespaceAdaptersConfigureSharedCorePhases(t *testing.T) {
	tests := []struct {
		ref            string
		wantWorkflow   string
		wantChangeDir  string
		wantChangeKind string
	}{
		{
			ref:            "builtin:openspec/plan-change-v2.0.yaml",
			wantWorkflow:   "../core/plan-change-v1.0.yaml",
			wantChangeDir:  "{{workspace_dir}}/openspec/changes/{{change_name}}",
			wantChangeKind: "openspec",
		},
		{
			ref:            "builtin:spec-driven/plan-change-v2.0.yaml",
			wantWorkflow:   "../core/plan-change-v1.0.yaml",
			wantChangeDir:  "{{canonical_change_dir}}",
			wantChangeKind: "spec-driven",
		},
		{
			ref:           "builtin:openspec/implement-change-v2.0.yaml",
			wantWorkflow:  "../core/implement-change-v1.0.yaml",
			wantChangeDir: "{{workspace_dir}}/openspec/changes/{{change_name}}",
		},
		{
			ref:           "builtin:spec-driven/implement-change-v2.0.yaml",
			wantWorkflow:  "../core/implement-change-v1.0.yaml",
			wantChangeDir: "{{canonical_change_dir}}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			body, err := ReadFile(tt.ref)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", tt.ref, err)
			}
			var workflow struct {
				Steps []struct {
					Workflow string            `yaml:"workflow"`
					Params   map[string]string `yaml:"params"`
				} `yaml:"steps"`
			}
			if err := yaml.Unmarshal(body, &workflow); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.ref, err)
			}
			var step struct {
				Workflow string            `yaml:"workflow"`
				Params   map[string]string `yaml:"params"`
			}
			found := false
			for _, candidate := range workflow.Steps {
				if candidate.Workflow == tt.wantWorkflow {
					step, found = candidate, true
					break
				}
			}
			if !found {
				t.Fatalf("%s has no shared-core adapter step %q", tt.ref, tt.wantWorkflow)
			}
			if step.Workflow != tt.wantWorkflow {
				t.Fatalf("%s workflow = %q, want %q", tt.ref, step.Workflow, tt.wantWorkflow)
			}
			if step.Params["change_dir"] != tt.wantChangeDir {
				t.Fatalf("%s change_dir = %q, want %q", tt.ref, step.Params["change_dir"], tt.wantChangeDir)
			}
			if tt.wantChangeKind != "" && step.Params["change_kind"] != tt.wantChangeKind {
				t.Fatalf("%s change_kind = %q, want %q", tt.ref, step.Params["change_kind"], tt.wantChangeKind)
			}
		})
	}
}

func TestCorePlanChangeUsesDeterministicValidation(t *testing.T) {
	body, err := ReadFile("builtin:core/plan-change-v1.0.yaml")
	if err != nil {
		t.Fatalf("ReadFile(core plan-change): %v", err)
	}

	var workflow struct {
		Params []struct {
			Name string `yaml:"name"`
		} `yaml:"params"`
		Steps []struct {
			ID           string            `yaml:"id"`
			Script       string            `yaml:"script"`
			Prompt       string            `yaml:"prompt"`
			ScriptInputs map[string]string `yaml:"script_inputs"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatalf("unmarshal core plan-change: %v", err)
	}

	if strings.Contains(string(body), "PLANNING_READY") ||
		strings.Contains(string(body), "definition_validation_checklist") ||
		strings.Contains(string(body), "planning-status-gate.sh") {
		t.Fatalf("core plan-change still contains agent-owned validation protocol:\n%s", body)
	}

	steps := make(map[string]struct {
		script       string
		prompt       string
		scriptInputs map[string]string
	}, len(workflow.Steps))
	for _, step := range workflow.Steps {
		steps[step.ID] = struct {
			script       string
			prompt       string
			scriptInputs map[string]string
		}{script: step.Script, prompt: step.Prompt, scriptInputs: step.ScriptInputs}
	}
	for _, id := range []string{"check-definition", "check-plan"} {
		step, ok := steps[id]
		if !ok {
			t.Fatalf("core plan-change missing %s", id)
		}
		if step.script != "validate-planning-artifacts.sh" {
			t.Errorf("%s script = %q, want validate-planning-artifacts.sh", id, step.script)
		}
		if step.prompt != "" {
			t.Errorf("%s unexpectedly invokes an agent prompt", id)
		}
	}
	if got := steps["check-definition"].scriptInputs["require_tasks"]; got != "false" {
		t.Errorf("check-definition require_tasks = %q, want false", got)
	}
	if got := steps["check-plan"].scriptInputs["require_tasks"]; got != "true" {
		t.Errorf("check-plan require_tasks = %q, want true", got)
	}
}

func TestCoreImplementChangePreflightsValidatedPlanBeforeAgentWork(t *testing.T) {
	body, err := ReadFile("builtin:core/implement-change-v1.0.yaml")
	if err != nil {
		t.Fatalf("ReadFile(core implement-change): %v", err)
	}

	var workflow struct {
		Params []struct {
			Name string `yaml:"name"`
		} `yaml:"params"`
		Steps []struct {
			ID           string            `yaml:"id"`
			Script       string            `yaml:"script"`
			ScriptInputs map[string]string `yaml:"script_inputs"`
			Prompt       string            `yaml:"prompt"`
			Workflow     string            `yaml:"workflow"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatalf("unmarshal core implement-change: %v", err)
	}

	if !slices.ContainsFunc(workflow.Params, func(param struct {
		Name string `yaml:"name"`
	}) bool {
		return param.Name == "change_kind"
	}) {
		t.Fatal("core implement-change does not require change_kind for deterministic planning-artifact validation")
	}

	var checkPlanIndex, repositoryGroupIndex = -1, -1
	for index, step := range workflow.Steps {
		switch step.ID {
		case "check-plan":
			checkPlanIndex = index
			if step.Script != "validate-planning-artifacts.sh" {
				t.Errorf("check-plan script = %q, want validate-planning-artifacts.sh", step.Script)
			}
			wantInputs := map[string]string{
				"change_name":   "{{change_name}}",
				"change_dir":    "{{change_dir}}",
				"change_kind":   "{{change_kind}}",
				"require_tasks": "true",
			}
			for name, want := range wantInputs {
				if got := step.ScriptInputs[name]; got != want {
					t.Errorf("check-plan script input %s = %q, want %q", name, got, want)
				}
			}
		case "implement-task-groups":
			repositoryGroupIndex = index
		}
	}
	if checkPlanIndex < 0 {
		t.Fatal("core implement-change has no check-plan preflight")
	}
	if repositoryGroupIndex < 0 {
		t.Fatal("core implement-change has no repository task group")
	}
	if checkPlanIndex >= repositoryGroupIndex {
		t.Fatalf("check-plan index = %d, want before repository group index %d", checkPlanIndex, repositoryGroupIndex)
	}
	if checkPlanIndex != repositoryGroupIndex-1 {
		t.Fatalf("check-plan index = %d, want immediately before repository group index %d", checkPlanIndex, repositoryGroupIndex)
	}
	for _, step := range workflow.Steps[:checkPlanIndex+1] {
		if step.Prompt != "" || step.Workflow != "" {
			t.Fatalf("preflight step %q can invoke an agent or sub-workflow before plan validation", step.ID)
		}
	}
}

func TestV2ImplementChangeCallersProvideChangeKind(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{ref: "builtin:openspec/change-v2.0.yaml", want: "openspec"},
		{ref: "builtin:openspec/implement-change-v2.0.yaml", want: "openspec"},
		{ref: "builtin:spec-driven/change-v2.0.yaml", want: "spec-driven"},
		{ref: "builtin:spec-driven/implement-change-v2.0.yaml", want: "spec-driven"},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			body, err := ReadFile(tt.ref)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", tt.ref, err)
			}
			var workflow struct {
				Steps []struct {
					Workflow string            `yaml:"workflow"`
					Params   map[string]string `yaml:"params"`
				} `yaml:"steps"`
			}
			if err := yaml.Unmarshal(body, &workflow); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.ref, err)
			}
			for _, step := range workflow.Steps {
				if step.Workflow != "../core/implement-change-v1.0.yaml" {
					continue
				}
				if got := step.Params["change_kind"]; got != tt.want {
					t.Fatalf("implement change_kind = %q, want %q", got, tt.want)
				}
				return
			}
			t.Fatal("no shared core implement-change step")
		})
	}
}

func TestBuiltInPlanningUsesTaskGroupResolverAndScopedRepositoryLifecycle(t *testing.T) {
	plan, err := ReadFile("builtin:core/plan-change-v1.0.yaml")
	if err != nil {
		t.Fatalf("ReadFile(core plan-change): %v", err)
	}
	var planWorkflow struct {
		Scope string `yaml:"scope"`
		Steps []struct {
			ID      string `yaml:"id"`
			Script  string `yaml:"script"`
			Capture string `yaml:"capture"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(plan, &planWorkflow); err != nil {
		t.Fatalf("unmarshal core plan-change: %v", err)
	}
	if planWorkflow.Scope != "workspace" {
		t.Fatalf("core plan-change scope = %q, want workspace", planWorkflow.Scope)
	}
	var selected bool
	for _, step := range planWorkflow.Steps {
		if step.ID == "resolve-affected-repositories" && step.Script == "resolve-task-group.sh" && step.Capture == "affected_repositories" {
			selected = true
		}
	}
	if !selected {
		t.Fatal("core plan-change must capture affected_repositories through resolve-task-group.sh")
	}

	implementation, err := ReadFile("builtin:core/implement-change-v1.0.yaml")
	if err != nil {
		t.Fatalf("ReadFile(core implement-change): %v", err)
	}
	var implementWorkflow struct {
		Scope  string `yaml:"scope"`
		Params []struct {
			Name string `yaml:"name"`
		} `yaml:"params"`
		Steps []struct {
			ID       string `yaml:"id"`
			Scope    string `yaml:"scope"`
			Script   string `yaml:"script"`
			Workflow string `yaml:"workflow"`
			Steps    []struct {
				ID       string `yaml:"id"`
				Script   string `yaml:"script"`
				Capture  string `yaml:"capture"`
				Workflow string `yaml:"workflow"`
			} `yaml:"steps"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(implementation, &implementWorkflow); err != nil {
		t.Fatalf("unmarshal core implement-change: %v", err)
	}
	if implementWorkflow.Scope != "workspace" {
		t.Fatalf("core implement-change scope = %q, want workspace", implementWorkflow.Scope)
	}
	if !slices.ContainsFunc(implementWorkflow.Params, func(param struct {
		Name string `yaml:"name"`
	}) bool {
		return param.Name == "repositories"
	}) {
		t.Fatal("core implement-change must accept selected repositories")
	}
	var repositoryGroup bool
	for _, step := range implementWorkflow.Steps {
		if step.ID != "implement-task-groups" {
			continue
		}
		if step.Scope == "repositories" && len(step.Steps) == 1 && step.Steps[0].Workflow == "implement-repository-task-group-v1.0.yaml" {
			repositoryGroup = true
		}
	}
	if !repositoryGroup {
		t.Fatal("core implement-change must invoke its repository-scoped task group")
	}

	asset, err := ReadAsset("core/resolve-task-group.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/resolve-task-group.sh): %v", err)
	}
	for _, want := range []string{"internal task-groups", "--output repositories", "--output task-pattern"} {
		if !strings.Contains(string(asset), want) {
			t.Errorf("task-group resolver does not delegate %q", want)
		}
	}
}

func TestEveryShippedWorkflowExplicitlyClassifiesScope(t *testing.T) {
	contextNeutral := map[string]bool{
		"core/run-validator-v1.0.yaml":           true,
		"core/validate-feature-branch-v1.0.yaml": true,
		"core/implement-task-v1.0.yaml":          true,
		"core/finalize-pr-v1.0.yaml":             true,
		"core/remediate-repository-v1.0.yaml":    true,
	}
	err := fs.WalkDir(FS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".yaml") || strings.HasPrefix(filepath.Base(path), "_") {
			return nil
		}
		body, err := FS.ReadFile(path)
		if err != nil {
			return err
		}
		var workflow model.Workflow
		if err := yaml.Unmarshal(body, &workflow); err != nil {
			t.Errorf("unmarshal %s: %v", path, err)
			return nil
		}
		if contextNeutral[path] {
			if workflow.Scope != model.ScopeLegacy {
				t.Errorf("context-neutral %s scope = %q, want omitted", path, workflow.Scope)
			}
		} else if workflow.Scope != model.ScopeWorkspace && workflow.Scope != model.ScopeRepositories {
			t.Errorf("orchestration workflow %s has no explicit scope", path)
		}
		if err := workflow.Validate(nil); err != nil {
			t.Errorf("validate %s: %v", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded workflows: %v", err)
	}
}

func TestRepositoryFinalizationSkipsWorkspaceCheckoutAndAcceptanceRequiresCompletion(t *testing.T) {
	finalizers, err := ReadFile("builtin:core/finalize-repositories-v1.0.yaml")
	if err != nil {
		t.Fatalf("ReadFile(finalize repositories): %v", err)
	}
	if !strings.Contains(string(finalizers), `test "{{repository_dir}}" = "{{workspace_dir}}"`) {
		t.Fatal("repository finalization must skip the workspace checkout to avoid duplicate implicit-repository PR finalization")
	}

	implementation, err := ReadFile("builtin:core/implement-change-v1.0.yaml")
	if err != nil {
		t.Fatalf("ReadFile(implement change): %v", err)
	}
	if !strings.Contains(string(implementation), "ACCEPTANCE_COMPLETE") || !strings.Contains(string(implementation), "acceptance-preparation-status.txt") {
		t.Fatal("implementation acceptance handoff gate must require successful acceptance preparation")
	}
}

func TestAcceptanceAndSimpleChangeRemediateBeforeReacceptanceOrFinalization(t *testing.T) {
	tests := []struct {
		ref  string
		want []string
	}{
		{ref: "builtin:core/accept-change-v1.0.yaml", want: []string{"review-and-refine", "validate-remediation-ledger", "remediate-repositories", "recommend-reacceptance-testing", "run-reacceptance-testing"}},
		{ref: "builtin:core/complete-simple-change-v1.0.yaml", want: []string{"implement-task-groups", "test", "review", "validate-remediation-ledger", "remediate-repositories", "finalize-repositories"}},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			body, err := ReadFile(tt.ref)
			if err != nil {
				t.Fatal(err)
			}
			var workflow model.Workflow
			if err := yaml.Unmarshal(body, &workflow); err != nil {
				t.Fatal(err)
			}
			positions := make(map[string]int, len(workflow.Steps))
			for index := range workflow.Steps {
				positions[workflow.Steps[index].ID] = index
			}
			for index := 1; index < len(tt.want); index++ {
				if positions[tt.want[index-1]] >= positions[tt.want[index]] {
					t.Fatalf("step order %v does not preserve %s before %s", positions, tt.want[index-1], tt.want[index])
				}
			}
			for _, boundary := range []string{"implement-task-groups", "remediate-repositories"} {
				if position, ok := positions[boundary]; ok && workflow.Steps[position].Scope != model.ScopeRepositories {
					t.Fatalf("%s scope = %q, want repositories", boundary, workflow.Steps[position].Scope)
				}
			}
			ledgerStep := workflow.Steps[positions["validate-remediation-ledger"]]
			if ledgerStep.Script != "validate-remediation-ledger.sh" {
				t.Fatalf("validate-remediation-ledger script = %q, want shared validator", ledgerStep.Script)
			}
			wantInputs := map[string]string{
				"ledger":       "{{session_dir}}/output/acceptance-remediation.json",
				"repositories": "{{repositories}}",
			}
			for name, want := range wantInputs {
				if got := ledgerStep.ScriptInputs[name]; got != want {
					t.Errorf("validate-remediation-ledger input %s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestCoreValidateRemediationLedgerScript(t *testing.T) {
	script, err := ReadAsset("core/validate-remediation-ledger.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/validate-remediation-ledger.sh): %v", err)
	}
	scriptPath := filepath.Join(t.TempDir(), "validate-remediation-ledger.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write validator script: %v", err)
	}

	run := func(ledger, repositories string) (string, error) {
		t.Helper()
		payload := `{"ledger":` + strconv.Quote(ledger) + `,"repositories":` + strconv.Quote(repositories) + `}`
		cmd := exec.Command("sh", scriptPath)
		cmd.Stdin = strings.NewReader(payload)
		output, runErr := cmd.CombinedOutput()
		return string(output), runErr
	}

	ledger := filepath.Join(t.TempDir(), "acceptance-remediation.json")
	if output, err := run(ledger, "backend,frontend"); err != nil {
		t.Fatalf("missing ledger validation failed: %v\n%s", err, output)
	}
	contents, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatalf("read initialized ledger: %v", err)
	}
	if got, want := strings.TrimSpace(string(contents)), `{"workspace":[],"repositories":{}}`; got != want {
		t.Fatalf("initialized ledger = %q, want %q", got, want)
	}

	if err := os.WriteFile(ledger, []byte(`{"workspace":[],"repositories":{"docs":[]}}`), 0o600); err != nil {
		t.Fatalf("write invalid ledger: %v", err)
	}
	output, err := run(ledger, "backend,frontend")
	if err == nil {
		t.Fatal("validator accepted an unselected repository")
	}
	if !strings.Contains(output, "names unselected repositories: docs") {
		t.Fatalf("unexpected validation failure: %s", output)
	}
}

func TestCoreValidatePlanningArtifactsScript(t *testing.T) {
	script, err := ReadAsset("core/validate-planning-artifacts.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/validate-planning-artifacts.sh): %v", err)
	}

	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "validate-planning-artifacts.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write validator script: %v", err)
	}
	fakeBin := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("create fake bin: %v", err)
	}
	fakeOpenSpec := filepath.Join(fakeBin, "openspec")
	if err := os.WriteFile(fakeOpenSpec, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fake openspec: %v", err)
	}
	taskGroupLog := filepath.Join(tempDir, "task-groups.log")
	fakeAgentRunner := filepath.Join(fakeBin, "agent-runner")
	if err := os.WriteFile(fakeAgentRunner, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TASK_GROUP_LOG\"\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = --change-dir ]; then\n    shift\n    change_dir=$1\n    break\n  fi\n  shift\ndone\ntest -s \"$change_dir/tasks/one.md\"\n"), 0o700); err != nil {
		t.Fatalf("write fake agent-runner: %v", err)
	}

	projectDir := filepath.Join(tempDir, "project")
	changeDir := filepath.Join(projectDir, "openspec", "changes", "demo")
	for _, dir := range []string{filepath.Join(changeDir, "specs", "widgets"), filepath.Join(changeDir, "tasks")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	for name, content := range map[string]string{
		"proposal.md": "# Proposal\n",
		"design.md":   "# Design\n",
		"test-plan.md": `## Coverage Strategy

Use the lowest reliable layer.

## Integration Tests

### INT-001: Widget boundary
- Covers: Widget behavior
- Boundary: CLI and service
- Setup: Controlled fixture
- Action: Invoke the CLI
- Assertions: Stable output
- Execution: Integration suite

## End-to-End Tests

### E2E-001: Widget journey
- Covers: Widget behavior
- Surface: CLI
- Setup: Isolated workspace
- Journey: Create a widget
- Assertions: Widget is reported
- Execution: End-to-end suite

## Agent Acceptance Tests

### AT-001: Use a widget
- Classification: Required
- Covers: Widget behavior
- Actor and surface: User through the CLI
- Setup: Isolated workspace
- Steps: Create and inspect a widget
- Expected: The widget is visible
- Evidence: Captured terminal output
- Effects and cleanup: Remove the workspace
- Permitted substitutes: None

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | AT | HT |
| --- | --- | --- | --- | --- |
| Widget behavior | INT-001 | E2E-001 | AT-001 | — |
`,
		"specs/widgets/spec.md": "## ADDED Requirements\n### Requirement: Widget\nThe system SHALL work.\n#### Scenario: Works\n- **WHEN** used\n- **THEN** it works\n",
	} {
		path := filepath.Join(changeDir, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	run := func(requireTasks bool, changeDirInput string) error {
		t.Helper()
		cmd := exec.Command("sh", scriptPath)
		cmd.Dir = projectDir
		cmd.Env = append(os.Environ(), "PATH="+fakeBin+":"+os.Getenv("PATH"), "TASK_GROUP_LOG="+taskGroupLog, "AGENT_RUNNER_EXECUTABLE="+fakeAgentRunner)
		cmd.Stdin = strings.NewReader(`{"change_name":"demo","change_dir":"` + changeDirInput + `","change_kind":"openspec","require_tasks":"` + strconv.FormatBool(requireTasks) + `"}`)
		return cmd.Run()
	}

	if err := run(false, "openspec/changes/demo"); err != nil {
		t.Fatalf("definition validation failed: %v", err)
	}

	validTestPlan, err := os.ReadFile(filepath.Join(changeDir, "test-plan.md"))
	if err != nil {
		t.Fatalf("read valid test plan: %v", err)
	}
	for name, content := range map[string]string{
		"missing required section": strings.ReplaceAll(string(validTestPlan), "## Coverage Map", "## Traceability"),
		"dangling coverage id":     strings.ReplaceAll(string(validTestPlan), "AT-001 | —", "AT-999 | —"),
		"unmapped obligation id":   strings.ReplaceAll(string(validTestPlan), "E2E-001 | AT-001", "— | AT-001"),
		"duplicate obligation id":  string(validTestPlan) + "\n### AT-001: Duplicate\n",
		"missing integration field": strings.ReplaceAll(
			string(validTestPlan),
			"- Boundary: CLI and service\n",
			"",
		),
		"missing acceptance field":       strings.ReplaceAll(string(validTestPlan), "- Evidence: Captured terminal output\n", ""),
		"empty conditional trigger":      strings.ReplaceAll(string(validTestPlan), "- Classification: Required", "- Classification: Conditional:"),
		"missing human-only disposition": strings.ReplaceAll(string(validTestPlan), "\nNone.\n\n## Coverage Map", "\nNot applicable.\n\n## Coverage Map"),
	} {
		t.Run(name, func(t *testing.T) {
			testPlanPath := filepath.Join(changeDir, "test-plan.md")
			if err := os.WriteFile(testPlanPath, []byte(content), 0o600); err != nil {
				t.Fatalf("write invalid test plan: %v", err)
			}
			if err := run(false, "openspec/changes/demo"); err == nil {
				t.Fatal("definition validation passed malformed test plan")
			}
			if err := os.WriteFile(testPlanPath, validTestPlan, 0o600); err != nil {
				t.Fatalf("restore valid test plan: %v", err)
			}
		})
	}

	t.Run("missing test plan", func(t *testing.T) {
		testPlanPath := filepath.Join(changeDir, "test-plan.md")
		if err := os.Remove(testPlanPath); err != nil {
			t.Fatalf("remove test plan: %v", err)
		}
		if err := run(false, "openspec/changes/demo"); err == nil {
			t.Fatal("definition validation passed without test-plan.md")
		}
		if err := os.WriteFile(testPlanPath, validTestPlan, 0o600); err != nil {
			t.Fatalf("restore valid test plan: %v", err)
		}
	})

	if err := run(true, "openspec/changes/demo"); err == nil {
		t.Fatal("task-plan validation passed without a task file")
	}

	if err := os.WriteFile(filepath.Join(changeDir, "tasks", "one.md"), []byte("# Task one\n"), 0o600); err != nil {
		t.Fatalf("write task file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(changeDir, "tasks.md"), []byte("- [Task one](tasks/one.md)\n"), 0o600); err != nil {
		t.Fatalf("write task index: %v", err)
	}
	if err := run(true, "openspec/changes/demo"); err != nil {
		t.Fatalf("task-plan validation failed: %v", err)
	}
	if err := run(true, changeDir); err != nil {
		t.Fatalf("task-plan validation rejected canonical OpenSpec change path: %v", err)
	}
	called, err := os.ReadFile(taskGroupLog)
	if err != nil {
		t.Fatalf("read task-group delegation log: %v", err)
	}
	if got := string(called); !strings.Contains(got, "internal task-groups") || !strings.Contains(got, "--plan-kind full") {
		t.Fatalf("task validation did not delegate to task-groups: %q", got)
	}
}

func TestV2SimpleChangeWorkflowsShareCorePhases(t *testing.T) {
	for _, ref := range []string{
		"builtin:core/complete-simple-change-v1.0.yaml",
	} {
		body, err := ReadFile(ref)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", ref, err)
		}
		for _, forbidden := range []string{"openspec/changes/", "specs/changes/", "OpenSpec change", "openspec validate"} {
			if strings.Contains(string(body), forbidden) {
				t.Errorf("%s contains provider-specific text %q", ref, forbidden)
			}
		}
	}

	tests := []struct {
		ref                    string
		wantChangeDir          string
		wantValidationText     string
		wantOpenSpecValidation bool
		wantArchive            bool
		discoverChangeDir      bool
	}{
		{
			ref:                    "builtin:openspec/simple-change-v2.0.yaml",
			wantChangeDir:          "{{workspace_dir}}/openspec/changes/{{change_name}}",
			wantValidationText:     "openspec validate",
			wantOpenSpecValidation: true,
			wantArchive:            true,
		},
		{
			ref:                "builtin:spec-driven/simple-change-v2.0.yaml",
			wantChangeDir:      "{{canonical_change_dir}}",
			wantValidationText: "simple-change-validation-checklist.md",
			discoverChangeDir:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			body, err := ReadFile(tt.ref)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", tt.ref, err)
			}
			var workflow struct {
				Params []struct {
					Name     string `yaml:"name"`
					Required *bool  `yaml:"required"`
				} `yaml:"params"`
				Steps []struct {
					ID       string            `yaml:"id"`
					Workflow string            `yaml:"workflow"`
					Script   string            `yaml:"script"`
					Params   map[string]string `yaml:"params"`
				} `yaml:"steps"`
			}
			if err := yaml.Unmarshal(body, &workflow); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.ref, err)
			}
			if len(workflow.Params) == 0 || workflow.Params[0].Name != "change_name" ||
				workflow.Params[0].Required == nil || !*workflow.Params[0].Required {
				t.Fatalf("%s must require change_name, got %#v", tt.ref, workflow.Params)
			}

			steps := make(map[string]struct {
				workflow string
				script   string
				params   map[string]string
			}, len(workflow.Steps))
			for _, step := range workflow.Steps {
				steps[step.ID] = struct {
					workflow string
					script   string
					params   map[string]string
				}{workflow: step.Workflow, script: step.Script, params: step.Params}
			}

			if got := steps["validate-feature-branch"].workflow; got != "../core/validate-feature-branch-v1.0.yaml" {
				t.Errorf("branch guard workflow = %q", got)
			}
			if got := steps["plan"].workflow; got != "" {
				t.Errorf("plan unexpectedly delegates to %q", got)
			}
			if got := steps["review-plan"].workflow; got != "" {
				t.Errorf("review unexpectedly delegates to %q", got)
			}
			if got := steps["check-planning-artifacts"].workflow; got != "../core/check-planning-artifacts-v1.0.yaml" {
				t.Errorf("artifact check workflow = %q", got)
			}
			if got := steps["check-planning-artifacts"].params["change_dir"]; got != tt.wantChangeDir {
				t.Errorf("artifact check change_dir = %q, want %q", got, tt.wantChangeDir)
			}
			if !strings.Contains(string(body), tt.wantValidationText) {
				t.Errorf("workflow does not contain validation text %q", tt.wantValidationText)
			}
			if tt.discoverChangeDir {
				if _, ok := steps["commit-plan"]; ok {
					t.Error("dynamic plan should not commit artifacts that may live outside the project repository")
				}
			} else {
				if got := steps["commit-plan"].workflow; got != "../core/commit-change-plan-v1.0.yaml" {
					t.Errorf("commit workflow = %q", got)
				}
			}
			if got := steps["complete"].workflow; got != "../core/complete-simple-change-v1.0.yaml" {
				t.Errorf("complete workflow = %q", got)
			}
			if got := steps["complete"].params["change_dir"]; got != tt.wantChangeDir {
				t.Errorf("complete change_dir = %q, want %q", got, tt.wantChangeDir)
			}
			_, hasValidation := steps["validate-openspec"]
			if hasValidation != tt.wantOpenSpecValidation {
				t.Errorf("OpenSpec validation presence = %t, want %t", hasValidation, tt.wantOpenSpecValidation)
			}
			_, hasArchive := steps["archive"]
			if hasArchive != tt.wantArchive {
				t.Errorf("archive presence = %t, want %t", hasArchive, tt.wantArchive)
			}
		})
	}
}

func TestCoreReviewTasksWorkflowAndOpenSpecGateRemainEmbedded(t *testing.T) {
	ref, err := Resolve("core:review-tasks")
	if err != nil {
		t.Fatalf("Resolve(core:review-tasks): %v", err)
	}
	if ref != "builtin:core/review-tasks-v1.0.yaml" {
		t.Fatalf("resolved ref = %q, want core review-tasks v1.0", ref)
	}

	assets, err := ListAssets("openspec")
	if err != nil {
		t.Fatalf("ListAssets(openspec): %v", err)
	}
	if !slices.Contains(assets, "task-review-loop-gate.sh") {
		t.Fatalf("ListAssets(openspec) missing task-review-loop-gate.sh: %v", assets)
	}
	if body, err := ReadAsset("openspec/task-review-loop-gate.sh"); err != nil {
		t.Fatalf("ReadAsset(task-review-loop-gate.sh): %v", err)
	} else if len(body) == 0 {
		t.Fatal("task-review-loop-gate.sh is empty")
	}

	info, err := os.Stat("openspec/task-review-loop-gate.sh")
	if err != nil {
		t.Fatalf("stat task-review-loop-gate.sh: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("task-review-loop-gate.sh mode = %v, want executable", info.Mode().Perm())
	}
}

func TestOpenSpecTaskReviewLoopGateHandlesLargeReportWithoutJQ(t *testing.T) {
	script, err := ReadAsset("openspec/task-review-loop-gate.sh")
	if err != nil {
		t.Fatalf("ReadAsset(task-review-loop-gate.sh): %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "task-review-loop-gate.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write task review gate: %v", err)
	}

	binDir := t.TempDir()
	for _, name := range []string{"cat", "python3", "sed", "tail"} {
		target, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s not available", name)
		}
		if name == "python3" {
			out, err := exec.Command(target, "-c", "import sys; print(sys.executable)").Output()
			if err != nil {
				t.Skipf("resolve python3 executable: %v", err)
			}
			target = strings.TrimSpace(string(out))
		}
		if err := os.Symlink(target, filepath.Join(binDir, name)); err != nil {
			t.Fatalf("symlink %s: %v", name, err)
		}
	}

	report := strings.Repeat("x", 3<<20) + "\nREVIEW_RESOLVED\n"
	cmd := exec.Command("sh", scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+binDir)
	cmd.Stdin = strings.NewReader(`{"report":` + strconv.Quote(report) + `}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("large report failed without jq: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Task review loop: resolved") {
		t.Fatalf("output = %q, want resolved status", out)
	}
}

func TestResolveFSRejectsInvalidAndDuplicateLogicalGroups(t *testing.T) {
	t.Run("unversioned sibling invalidates versioned group", func(t *testing.T) {
		fsys := fstest.MapFS{
			"team/deploy.yaml":      {Data: []byte("name: deploy\n")},
			"team/deploy-v1.0.yaml": {Data: []byte("name: deploy\n")},
		}

		_, err := resolveFS(fsys, "team:deploy")
		if err == nil {
			t.Fatal("resolveFS succeeded, want invalid filename error")
		}
		for _, want := range []string{"team/deploy.yaml", "deploy-v1.0.yaml"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want text %q", err, want)
			}
		}
	})

	t.Run("yaml and yml duplicate version invalidates group", func(t *testing.T) {
		fsys := fstest.MapFS{
			"team/deploy-v1.0.yaml": {Data: []byte("name: deploy\n")},
			"team/deploy-v1.0.yml":  {Data: []byte("name: deploy\n")},
		}

		_, err := resolveFS(fsys, "team:deploy")
		if err == nil || !strings.Contains(err.Error(), "duplicate version v1.0") {
			t.Fatalf("resolveFS error = %v, want duplicate version", err)
		}
	})
}

func TestDefinitionEnumerationIncludesYMLButExcludesMetadataAndTopLevelFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"top-v1.0.yaml":          {Data: []byte("name: top\n")},
		"core/_group.yaml":       {Data: []byte("display_name: Core\n")},
		"core/deploy-v1.0.yml":   {Data: []byte("name: deploy\n")},
		"core/deploy-v2.0.yaml":  {Data: []byte("name: deploy\n")},
		"core/data.json":         {Data: []byte("{}")},
		"core/nested/readme.txt": {Data: []byte("docs")},
	}

	ref, err := resolveFS(fsys, "core:deploy")
	if err != nil {
		t.Fatalf("resolveFS: %v", err)
	}
	if ref != "builtin:core/deploy-v2.0.yaml" {
		t.Fatalf("resolveFS ref = %q, want latest YAML definition", ref)
	}

	refs, err := listFS(fsys)
	if err != nil {
		t.Fatalf("listFS: %v", err)
	}
	want := []string{"builtin:core/deploy-v1.0.yml", "builtin:core/deploy-v2.0.yaml"}
	if !slices.Equal(refs, want) {
		t.Fatalf("listFS refs = %v, want %v", refs, want)
	}

	if _, err := ReadAsset("core/deploy-v1.0.yml"); err == nil || !strings.Contains(err.Error(), "is a workflow") {
		t.Fatalf("ReadAsset(YML definition) error = %v, want workflow rejection", err)
	}
}

func TestBuiltinReferenceAndAssetPathsStayConfined(t *testing.T) {
	for _, ref := range []string{"builtin:../core/debug-v1.0.yaml", "builtin:/core/debug-v1.0.yaml"} {
		if _, err := RefPath(ref); err == nil {
			t.Fatalf("RefPath(%q) succeeded, want confinement error", ref)
		}
	}
	for _, asset := range []string{"../core/debug/prompt.md", "/core/debug/prompt.md"} {
		if _, err := ReadAsset(asset); err == nil {
			t.Fatalf("ReadAsset(%q) succeeded, want confinement error", asset)
		}
	}
}

func TestUnderscorePrefixedYAMLIsNotWorkflow(t *testing.T) {
	ref, err := Resolve("core:_group")
	if err == nil {
		t.Fatalf("Resolve(core:_group) = %q, want not found", ref)
	}

	refs, err := List()
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	for _, ref := range refs {
		if strings.HasSuffix(ref, "/_group.yaml") {
			t.Fatalf("List() exposed metadata file as workflow: %v", refs)
		}
	}
}

// TestIsMetadataBasename matches the underscore-prefix convention used by
// List() and discovery, so any workflow name whose basename starts with `_`
// is rejected regardless of its directory depth.
func TestIsMetadataBasename(t *testing.T) {
	for _, in := range []string{"_group", "sub/_group", "nested/dir/_meta"} {
		if !isMetadataBasename(in) {
			t.Errorf("isMetadataBasename(%q) = false, want true", in)
		}
	}
	for _, in := range []string{"group", "sub/group", "nested/dir/group"} {
		if isMetadataBasename(in) {
			t.Errorf("isMetadataBasename(%q) = true, want false", in)
		}
	}
}

func TestNamespaceGroupMetadataEmbedded(t *testing.T) {
	// Go's //go:embed default skips files whose basename starts with `_` or `.`.
	// The pattern in embed.go must use the `all:` prefix so _group.yaml files
	// are actually present in FS; otherwise discovery silently falls back to
	// default display name and empty description.
	for _, ns := range []string{"spec-driven", "openspec", "onboarding", "core"} {
		data, err := FS.ReadFile(ns + "/_group.yaml")
		if err != nil {
			t.Errorf("FS.ReadFile(%s/_group.yaml) = %v, want embedded metadata", ns, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("%s/_group.yaml is embedded but empty", ns)
		}
	}
}

func TestOpenSpecPlanningWorkflowsUseSharedCreateScript(t *testing.T) {
	for _, ref := range []string{"builtin:openspec/plan-change-v1.0.yaml", "builtin:openspec/simple-change-v1.0.yaml"} {
		t.Run(ref, func(t *testing.T) {
			data, err := ReadFile(ref)
			if err != nil {
				t.Fatalf("read %s: %v", ref, err)
			}

			var workflow struct {
				Steps []struct {
					ID           string            `yaml:"id"`
					Command      string            `yaml:"command"`
					Script       string            `yaml:"script"`
					ScriptInputs map[string]string `yaml:"script_inputs"`
				} `yaml:"steps"`
			}
			if err := yaml.Unmarshal(data, &workflow); err != nil {
				t.Fatalf("parse %s: %v", ref, err)
			}
			if len(workflow.Steps) == 0 {
				t.Fatalf("%s has no steps", ref)
			}
			create := workflow.Steps[0]
			if create.ID != "create" {
				t.Fatalf("first step id = %q, want create", create.ID)
			}
			if create.Script != "create-change.sh" {
				t.Fatalf("create script = %q, want create-change.sh", create.Script)
			}
			if create.Command != "" {
				t.Fatalf("create should not duplicate shell command, got %q", create.Command)
			}
			if got := create.ScriptInputs["change_name"]; got != "{{change_name}}" {
				t.Fatalf("script input change_name = %q, want {{change_name}}", got)
			}
		})
	}
}

func TestBuiltInWorkflowAgentReferencesUseCanonicalRoles(t *testing.T) {
	err := fs.WalkDir(FS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		body, err := FS.ReadFile(path)
		if err != nil {
			return err
		}
		for _, legacy := range []string{"agent: planner", "agent: reviewer"} {
			if strings.Contains(string(body), legacy) {
				t.Errorf("%s still contains legacy role reference %q", path, legacy)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded workflows: %v", err)
	}
}

func TestSharedAcceptanceCallsUseTesterAndPreserveControls(t *testing.T) {
	tests := []struct {
		ref      string
		stepID   string
		required []string
	}{
		{
			ref:    "builtin:core/implement-change-v1.0.yaml",
			stepID: "prepare-acceptance",
			required: []string{
				"`session: acceptance-tester`",
				"verification scope: `full` for the first pass",
				"test plan was structurally validated before implementation",
				"acceptance-assumptions.md",
				"acceptance-impact-scope.md",
				"acceptance-flow-evidence.md",
				"Use at most three acceptance-tester calls",
				"acceptance-handoff.md",
			},
		},
		{
			ref:    "builtin:core/accept-change-v1.0.yaml",
			stepID: "run-reacceptance-testing",
			required: []string{
				"`session: acceptance-tester`",
				"directly dependent flows agreed with the user",
				"acceptance-flow-evidence.md",
				"Use at most three tester calls",
				"REACCEPTANCE_COMPLETE",
				"REACCEPTANCE_FAILED",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			body, err := ReadFile(tt.ref)
			if err != nil {
				t.Fatalf("ReadFile(%s) error = %v", tt.ref, err)
			}
			var workflow struct {
				Sessions []struct {
					Name  string `yaml:"name"`
					Agent string `yaml:"agent"`
				} `yaml:"sessions"`
				Steps []struct {
					ID      string `yaml:"id"`
					Prompt  string `yaml:"prompt"`
					Session string `yaml:"session"`
					Mode    string `yaml:"mode"`
				} `yaml:"steps"`
			}
			if err := yaml.Unmarshal(body, &workflow); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.ref, err)
			}
			var testerAgent string
			for _, session := range workflow.Sessions {
				if session.Name == "acceptance-tester" {
					testerAgent = session.Agent
					break
				}
			}
			if testerAgent != "tester" {
				t.Fatalf("acceptance-tester agent = %q, want tester", testerAgent)
			}
			for _, step := range workflow.Steps {
				if step.ID != tt.stepID {
					continue
				}
				if step.Session != "lead-agent" || step.Mode != "autonomous" {
					t.Fatalf("%s shape = session:%q mode:%q, want lead-agent/autonomous", tt.stepID, step.Session, step.Mode)
				}
				for _, required := range tt.required {
					if !strings.Contains(step.Prompt, required) {
						t.Errorf("%s prompt missing preserved control %q", tt.stepID, required)
					}
				}
				return
			}
			t.Fatalf("%s has no step %q", tt.ref, tt.stepID)
		})
	}
}

func TestCoreReviewProposalUsesCrosscheckAgentProfile(t *testing.T) {
	data, err := ReadFile("builtin:core/review-proposal-v1.0.yaml")
	if err != nil {
		t.Fatalf("ReadFile(core/review-proposal): %v", err)
	}

	var workflow struct {
		Sessions []struct {
			Name  string `yaml:"name"`
			Agent string `yaml:"agent"`
		} `yaml:"sessions"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("unmarshal workflow: %v", err)
	}

	for _, session := range workflow.Sessions {
		if session.Name != "crosscheck-agent" {
			continue
		}
		if session.Agent != "crosscheck" {
			t.Fatalf("crosscheck-agent profile = %q, want crosscheck", session.Agent)
		}
		return
	}
	t.Fatal("review-proposal workflow has no crosscheck-agent session")
}

func TestCoreFinalizePRUsesCIStatusGate(t *testing.T) {
	data, err := ReadFile("builtin:core/finalize-pr-v1.0.yaml")
	if err != nil {
		t.Fatalf("ReadFile(core/finalize-pr): %v", err)
	}

	var workflow struct {
		Steps []struct {
			ID    string `yaml:"id"`
			Steps []struct {
				ID                string            `yaml:"id"`
				Script            string            `yaml:"script"`
				ScriptInputs      map[string]string `yaml:"script_inputs"`
				Capture           string            `yaml:"capture"`
				ContinueOnFailure bool              `yaml:"continue_on_failure"`
				BreakIf           string            `yaml:"break_if"`
				SkipIf            string            `yaml:"skip_if"`
			} `yaml:"steps"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("unmarshal workflow: %v", err)
	}

	var loopSteps []struct {
		ID                string            `yaml:"id"`
		Script            string            `yaml:"script"`
		ScriptInputs      map[string]string `yaml:"script_inputs"`
		Capture           string            `yaml:"capture"`
		ContinueOnFailure bool              `yaml:"continue_on_failure"`
		BreakIf           string            `yaml:"break_if"`
		SkipIf            string            `yaml:"skip_if"`
	}
	for _, step := range workflow.Steps {
		if step.ID == "ci-fix-loop" {
			loopSteps = step.Steps
			break
		}
	}
	if len(loopSteps) != 4 {
		t.Fatalf("ci-fix-loop body has %d steps, want 4", len(loopSteps))
	}

	waitCI := loopSteps[0]
	if waitCI.ID != "wait-ci" {
		t.Fatalf("first loop step = %q, want wait-ci", waitCI.ID)
	}
	if waitCI.BreakIf != "" {
		t.Fatalf("wait-ci break_if = %q, want empty because agent exit success is not CI success", waitCI.BreakIf)
	}
	if waitCI.Capture != "ci_report" {
		t.Fatalf("wait-ci capture = %q, want ci_report", waitCI.Capture)
	}

	gate := loopSteps[1]
	if gate.ID != "ci-status-gate" {
		t.Fatalf("second loop step = %q, want ci-status-gate", gate.ID)
	}
	if gate.Script != "ci-status-gate.sh" {
		t.Fatalf("gate script = %q, want ci-status-gate.sh", gate.Script)
	}
	if got := gate.ScriptInputs["report"]; got != "{{ci_report}}" {
		t.Fatalf("gate report input = %q, want {{ci_report}}", got)
	}
	if !gate.ContinueOnFailure {
		t.Fatal("gate should continue_on_failure so fix-pr can run after failed CI")
	}
	if gate.BreakIf != "success" {
		t.Fatalf("gate break_if = %q, want success", gate.BreakIf)
	}

	fixNeeded := loopSteps[2]
	if fixNeeded.ID != "ci-fix-needed-gate" {
		t.Fatalf("third loop step = %q, want ci-fix-needed-gate", fixNeeded.ID)
	}
	if fixNeeded.Script != "ci-fix-needed-gate.sh" {
		t.Fatalf("fix-needed script = %q, want ci-fix-needed-gate.sh", fixNeeded.Script)
	}
	if got := fixNeeded.ScriptInputs["report"]; got != "{{ci_report}}" {
		t.Fatalf("fix-needed report input = %q, want {{ci_report}}", got)
	}
	if !fixNeeded.ContinueOnFailure {
		t.Fatal("fix-needed gate should continue_on_failure so fix-pr can run after failed CI")
	}

	fixPR := loopSteps[3]
	if fixPR.ID != "fix-pr" {
		t.Fatalf("fourth loop step = %q, want fix-pr", fixPR.ID)
	}
	if fixPR.SkipIf != "previous_success" {
		t.Fatalf("fix-pr skip_if = %q, want previous_success", fixPR.SkipIf)
	}
}

func TestCoreCommitChangePlanRestrictsCommitToChangeDirectory(t *testing.T) {
	script, err := ReadAsset("core/commit-change-plan.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/commit-change-plan.sh): %v", err)
	}

	commit := `git commit -m "[commit-plan] chore: add change documents for $change_name" -- "$change_dir"`
	if !strings.Contains(string(script), commit) {
		t.Fatal("commit-change-plan.sh must restrict git commit to change_dir")
	}
}

func TestCoreCommitChangePlanRejectsRepositoryRoot(t *testing.T) {
	script, err := ReadAsset("core/commit-change-plan.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/commit-change-plan.sh): %v", err)
	}

	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "commit-change-plan.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	binDir := filepath.Join(tempDir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	if err := os.Symlink(pythonPath, filepath.Join(binDir, "python3")); err != nil {
		t.Fatalf("symlink python3: %v", err)
	}
	gitMarker := filepath.Join(tempDir, "git-called")
	fakeGit := "#!/bin/sh\n: > " + strconv.Quote(gitMarker) + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(fakeGit), 0o700); err != nil {
		t.Fatalf("write fake git: %v", err)
	}

	cmd := exec.Command("sh", scriptPath)
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "PATH="+binDir+":/usr/bin:/bin")
	cmd.Stdin = strings.NewReader(`{"change_name":"demo","change_dir":"."}`)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("script accepted repository root")
	}
	if !strings.Contains(string(out), "change_dir must be a confined relative path") {
		t.Fatalf("output = %q, want confined-path error", out)
	}
	if _, err := os.Stat(gitMarker); !os.IsNotExist(err) {
		t.Fatalf("git was invoked for repository-root path: %v", err)
	}
}

func TestCreateChangeScriptsExplainMissingAgentValidator(t *testing.T) {
	for _, asset := range []string{"openspec/create-change.sh", "spec-driven/create-change.sh"} {
		t.Run(asset, func(t *testing.T) {
			script, err := ReadAsset(asset)
			if err != nil {
				t.Fatalf("ReadAsset(%s): %v", asset, err)
			}

			tempDir := t.TempDir()
			scriptPath := filepath.Join(tempDir, "create-change.sh")
			if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
				t.Fatalf("write script: %v", err)
			}
			if strings.HasPrefix(asset, "openspec/") {
				validator, err := ReadAsset("openspec/validate-change-name.sh")
				if err != nil {
					t.Fatalf("ReadAsset(openspec/validate-change-name.sh): %v", err)
				}
				if err := os.WriteFile(filepath.Join(tempDir, "validate-change-name.sh"), validator, 0o700); err != nil {
					t.Fatalf("write name validator: %v", err)
				}
			}
			binDir := filepath.Join(tempDir, "bin")
			if err := os.Mkdir(binDir, 0o755); err != nil {
				t.Fatalf("create bin dir: %v", err)
			}
			pythonPath, err := exec.LookPath("python3")
			if err != nil {
				t.Skip("python3 not available")
			}
			if err := os.Symlink(pythonPath, filepath.Join(binDir, "python3")); err != nil {
				t.Fatalf("symlink python3: %v", err)
			}

			cmd := exec.Command("sh", scriptPath)
			cmd.Dir = tempDir
			cmd.Env = append(os.Environ(), "PATH="+binDir+":/usr/bin:/bin")
			cmd.Stdin = strings.NewReader(`{"change_name":"demo","change_dir":"changes/demo"}`)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatal("script succeeded without agent-validator")
			}
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("run script: %v", err)
			}
			if exitErr.ExitCode() != 127 {
				t.Fatalf("exit code = %d, want 127; output: %s", exitErr.ExitCode(), out)
			}
			if !strings.Contains(string(out), "agent-validator is not installed") {
				t.Fatalf("output = %q, want missing-validator explanation", out)
			}
		})
	}
}

func TestCoreCIStatusGateScript(t *testing.T) {
	script, err := ReadAsset("core/ci-status-gate.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/ci-status-gate.sh): %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "ci-status-gate.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tests := []struct {
		name     string
		report   string
		wantCode int
	}{
		{name: "passed marker exits success", report: "CI is green.\n\nCI_PASSED\n", wantCode: 0},
		{name: "failed marker exits fixable failure", report: "CI failed.\n\nCI_FAILED\n", wantCode: 1},
		{name: "comments marker exits fixable failure", report: "Review feedback remains.\n\nCI_COMMENTS\n", wantCode: 1},
		{name: "pending marker exits failure to keep polling", report: "Checks are running.\n\nCI_PENDING\n", wantCode: 1},
		{name: "markdown heading without marker fails closed", report: "## CI Status: passed\n", wantCode: 1},
		{name: "marker must be final non-empty line", report: "CI_PASSED\nAdditional prose\n", wantCode: 1},
		{name: "unknown exits failure to keep polling", report: "wait-ci did not produce a status\n", wantCode: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("sh", scriptPath)
			cmd.Stdin = strings.NewReader(`{"report":` + strconv.Quote(tt.report) + `}`)
			err := cmd.Run()
			gotCode := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("run script: %v", err)
				}
				gotCode = exitErr.ExitCode()
			}
			if gotCode != tt.wantCode {
				t.Fatalf("exit code = %d, want %d", gotCode, tt.wantCode)
			}
		})
	}
}

func TestCoreDebugWorkflowIsSinglePromptStepAndDoesNotUseResumeHandoff(t *testing.T) {
	data, err := ReadFile("builtin:core/debug-v1.0.yaml")
	if err != nil {
		t.Fatalf("ReadFile(core/debug): %v", err)
	}

	var workflow struct {
		Steps []struct {
			Agent   string `yaml:"agent"`
			ID      string `yaml:"id"`
			Mode    string `yaml:"mode"`
			Prompt  string `yaml:"prompt"`
			Session string `yaml:"session"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("unmarshal debug workflow: %v", err)
	}
	if len(workflow.Steps) != 1 {
		t.Fatalf("debug workflow should be a single prompt step, got %#v", workflow.Steps)
	}
	step := workflow.Steps[0]
	if step.ID != "debug" || step.Mode != "interactive" || step.Agent != "lead" || step.Session != "new" {
		t.Fatalf("debug step shape = %#v", step)
	}
	if !strings.Contains(step.Prompt, "{{session_dir}}/bundled/core/debug/prompt.md") {
		t.Fatalf("debug step does not point at bundled prompt file:\n%s", step.Prompt)
	}
	for _, step := range workflow.Steps {
		if step.ID == "handle-resume" {
			t.Fatalf("debug workflow still has handle-resume step: %#v", workflow.Steps)
		}
		if strings.Contains(step.Prompt, "resume-target") || strings.Contains(step.Prompt, "resume handoff") {
			t.Fatalf("step %s prompt still mentions resume handoff:\n%s", step.ID, step.Prompt)
		}
	}

	prompt, err := ReadAsset("core/debug/prompt.md")
	if err != nil {
		t.Fatalf("ReadAsset(core/debug/prompt.md): %v", err)
	}
	promptText := string(prompt)
	for _, forbidden := range []string{"resume-target", "resume-handoff", "emit a resume handoff"} {
		if strings.Contains(promptText, forbidden) {
			t.Fatalf("debug prompt still mentions %q", forbidden)
		}
	}
	if !strings.Contains(promptText, "agent-runner --resume <run-id>") {
		t.Fatalf("debug prompt does not tell user the manual resume command")
	}
	if !strings.Contains(promptText, "This workflow is a single interactive step") {
		t.Fatalf("debug prompt does not describe the single-step flow")
	}
	if !strings.Contains(promptText, "agent-runner --version") {
		t.Fatalf("debug prompt does not require agent-runner version in issue reports")
	}
	if !strings.Contains(promptText, "uname -a") {
		t.Fatalf("debug prompt does not require OS details in issue reports")
	}
	if !strings.Contains(promptText, "Session directory and project directory") {
		t.Fatalf("debug prompt does not require run directory details in issue reports")
	}
}

func TestCoreCIFixNeededGateScript(t *testing.T) {
	script, err := ReadAsset("core/ci-fix-needed-gate.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/ci-fix-needed-gate.sh): %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "ci-fix-needed-gate.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tests := []struct {
		name     string
		report   string
		wantCode int
	}{
		{name: "failed marker exits failure so fix-pr runs", report: "CI_FAILED\n", wantCode: 1},
		{name: "comments marker exits failure so fix-pr runs", report: "CI_COMMENTS\n", wantCode: 1},
		{name: "passed marker exits success so fix-pr skips", report: "CI_PASSED\n", wantCode: 0},
		{name: "pending marker exits success so fix-pr skips", report: "CI_PENDING\n", wantCode: 0},
		{name: "markdown heading without marker skips fixer", report: "## CI Status: failed\n", wantCode: 0},
		{name: "marker must be final non-empty line", report: "CI_FAILED\nAdditional prose\n", wantCode: 0},
		{name: "unknown exits success so fix-pr skips", report: "wait-ci did not produce a status\n", wantCode: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("sh", scriptPath)
			cmd.Stdin = strings.NewReader(`{"report":` + strconv.Quote(tt.report) + `}`)
			err := cmd.Run()
			gotCode := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("run script: %v", err)
				}
				gotCode = exitErr.ExitCode()
			}
			if gotCode != tt.wantCode {
				t.Fatalf("exit code = %d, want %d", gotCode, tt.wantCode)
			}
		})
	}

	t.Run("python fallback parses escaped JSON report without jq", func(t *testing.T) {
		pythonPath, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("python3 not available")
		}
		binDir := t.TempDir()
		if err := os.Symlink(pythonPath, filepath.Join(binDir, "python3")); err != nil {
			t.Fatalf("symlink python3: %v", err)
		}

		cmd := exec.Command("sh", scriptPath)
		cmd.Env = append(os.Environ(), "PATH="+binDir+":/usr/bin:/bin")
		cmd.Stdin = strings.NewReader(`{"report":"intro \"quoted\"\nCI_COMMENTS\n"}`)
		err = cmd.Run()
		if err == nil {
			t.Fatal("fallback script exit code = 0, want fix-needed failure")
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run script: %v", err)
		}
		if exitErr.ExitCode() != 1 {
			t.Fatalf("exit code = %d, want 1", exitErr.ExitCode())
		}
	})
}
