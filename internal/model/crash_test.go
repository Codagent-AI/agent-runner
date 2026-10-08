package model

import (
	"encoding/json"
	"testing"
)

func TestCrashLedgerPathsAndPruning(t *testing.T) {
	old := CrashRecord{StepID: "agent", Path: []NestingSegment{{StepID: "loop", Iteration: intPtr(0)}, {StepID: "agent"}}, ExecutionSessionID: "old"}
	current := CrashRecord{StepID: "agent", Path: []NestingSegment{{StepID: "loop", Iteration: intPtr(0)}, {StepID: "agent"}}, ExecutionSessionID: "new"}
	other := CrashRecord{StepID: "agent", Path: []NestingSegment{{StepID: "loop", Iteration: intPtr(1)}, {StepID: "agent"}}, ExecutionSessionID: "new"}
	ledger := NewCrashLedger()
	ledger.Add(&old)
	ledger.Add(&current)
	ledger.Add(&other)
	if !ledger.ObservedUnder([]NestingSegment{{StepID: "loop"}}) || !ledger.ObservedUnder([]NestingSegment{{StepID: "loop", Iteration: intPtr(0)}}) {
		t.Fatal("expected crash under loop and iteration")
	}
	ledger.PruneReexecuted([]NestingSegment{{StepID: "loop", Iteration: intPtr(0)}, {StepID: "agent"}}, "new")
	if !ledger.ObservedUnder([]NestingSegment{{StepID: "loop", Iteration: intPtr(0)}}) || !ledger.ObservedUnder([]NestingSegment{{StepID: "loop", Iteration: intPtr(1)}}) || len(ledger.Records()) != 2 {
		t.Fatal("pruning removed same-session or other-iteration crashes")
	}
}

func TestCrashLedgerStructuredPathMatchesOptionalQualifiers(t *testing.T) {
	iter, attempt := 2, 1
	ledger := NewCrashLedger()
	record := CrashRecord{Path: []NestingSegment{{StepID: "loop", Iteration: &iter}, {StepID: "child", SubWorkflowName: "verify"}, {StepID: "check", RepairAttempt: &attempt}, {StepID: "repair"}}}
	ledger.Add(&record)
	for _, path := range [][]NestingSegment{
		{{StepID: "loop"}},
		{{StepID: "loop", Iteration: &iter}, {StepID: "child"}},
		{{StepID: "loop", Iteration: &iter}, {StepID: "child", SubWorkflowName: "verify"}, {StepID: "check", RepairAttempt: &attempt}},
	} {
		if !ledger.ObservedUnder(path) {
			t.Fatalf("not observed under %v", path)
		}
	}
	otherIter, otherAttempt := 3, 2
	for _, path := range [][]NestingSegment{
		{{StepID: "loop", Iteration: &otherIter}},
		{{StepID: "loop"}, {StepID: "child", SubWorkflowName: "other"}},
		{{StepID: "loop"}, {StepID: "child"}, {StepID: "check", RepairAttempt: &otherAttempt}},
	} {
		if ledger.ObservedUnder(path) {
			t.Fatalf("unexpected observation under %v", path)
		}
	}
}

func TestCrashStateRoundTripAndLegacyDefaults(t *testing.T) {
	original := RunState{FailureKind: FailureInfrastructure, CrashObserved: true, Crashes: []CrashRecord{{StepID: "agent", Prefix: "[agent]"}}, CurrentStep: CurrentStep{Nested: &NestedStepState{StepID: "agent", PreviousStep: &PreviousStepRecord{Outcome: "failed", FailureKind: FailureInfrastructure, CrashObserved: true}, Repair: &RepairFrame{CheckID: "check", LastAttemptCrashed: true, LastCrashPrefix: "[check, repair]"}}}}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored RunState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.FailureKind != FailureInfrastructure || !restored.CrashObserved || len(restored.Crashes) != 1 || restored.CurrentStep.Nested.PreviousStep.FailureKind != FailureInfrastructure || !restored.CurrentStep.Nested.Repair.LastAttemptCrashed {
		t.Fatalf("restored = %+v", restored)
	}
	var legacy RunState
	if err := json.Unmarshal([]byte(`{"currentStep":{"stepId":"agent"}}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.FailureKind != "" || legacy.CrashObserved || len(legacy.Crashes) != 0 || legacy.CurrentStep.Nested.PreviousStep != nil {
		t.Fatalf("legacy = %+v", legacy)
	}
}

func TestPreviousStepBuiltinsRestore(t *testing.T) {
	ctx := NewRootContext(&RootContextOptions{})
	if vars := ctx.BuiltinVarsForStep("next"); vars["last_step_failure_kind"] != "" || vars["last_step_crash_observed"] != "false" {
		t.Fatalf("defaults = %v", vars)
	}
	ctx.RestorePreviousStep(&PreviousStepRecord{Outcome: "failed", FailureKind: FailureInfrastructure, CrashObserved: true})
	if vars := ctx.BuiltinVarsForStep("next"); vars["last_step_failure_kind"] != "infrastructure" || vars["last_step_crash_observed"] != "true" {
		t.Fatalf("restored = %v", vars)
	}
	if ctx.PreviousStep == nil || ctx.PreviousStep.Outcome != "failed" {
		t.Fatalf("previous step = %v", ctx.PreviousStep)
	}
}
