package runretention

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runs"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/codagent/agent-runner/internal/usersettings"
)

func TestPlan(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	p := PolicyFromSettings(usersettings.RunRetention{})
	cases := []struct {
		name string
		all  []Run
		want []string
	}{
		{"finished age", []Run{{ID: "old", Class: Finished, LastActivity: now.Add(-31 * 24 * time.Hour)}}, []string{"old"}},
		{"resumable grace", []Run{{ID: "old", Class: Resumable, LastActivity: now.Add(-45 * 24 * time.Hour)}}, nil},
		{"resumable expiry", []Run{{ID: "old", Class: Resumable, LastActivity: now.Add(-91 * 24 * time.Hour)}}, []string{"old"}},
		{"active group", []Run{{ID: "source", Class: Finished, LastActivity: now.Add(-60 * 24 * time.Hour), AuditRunIDs: []string{"audit"}}, {ID: "audit", Class: Active, LastActivity: now.Add(-60 * 24 * time.Hour), SourceRunID: "source"}}, nil},
		{"recent audit protects source", []Run{{ID: "source", Class: Finished, LastActivity: now.Add(-60 * 24 * time.Hour), AuditRunIDs: []string{"audit"}}, {ID: "audit", Class: Finished, LastActivity: now.Add(-10 * 24 * time.Hour), SourceRunID: "source"}}, nil},
		{"dangling link", []Run{{ID: "source", Class: Finished, LastActivity: now.Add(-60 * 24 * time.Hour), AuditRunIDs: []string{"gone"}}}, []string{"source"}},
		{"orphaned audit", []Run{{ID: "audit", Class: Finished, LastActivity: now.Add(-31 * 24 * time.Hour), SourceRunID: "gone"}}, []string{"audit"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			units := Plan(tc.all, p, now)
			var got []string
			for _, u := range units {
				for _, r := range u.Members {
					got = append(got, r.ID)
				}
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	zero := 0
	noCount := PolicyFromSettings(usersettings.RunRetention{MaxRunsPerProject: &zero})
	noCount.MaxAge = 0
	if got := Plan([]Run{{ID: "one", Class: Finished, LastActivity: now.Add(-time.Hour)}, {ID: "two", Class: Finished, LastActivity: now.Add(-2 * time.Hour)}}, noCount, now); len(got) != 0 {
		t.Fatalf("disabled limits removed runs: %+v", got)
	}
}

func TestSweepIntegration(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	project := filepath.Join(home, ".agent-runner", "projects", "a")
	runsDir := filepath.Join(project, "runs")
	if err := os.MkdirAll(runsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-40 * 24 * time.Hour)
	makeRun := func(id string, completed bool, age time.Time, audit *model.AuditMetadata) string {
		t.Helper()
		dir := filepath.Join(runsDir, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if id != "stateless" {
			if err := stateio.WriteState(&model.RunState{RunID: id, WorkflowFile: "builtin:test-v1.0.yaml", WorkflowName: id, Completed: completed, Audit: audit}, dir); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "audit.log"), []byte("log"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{filepath.Join(dir, "state.json"), filepath.Join(dir, "audit.log"), dir} {
			if _, err := os.Stat(path); err == nil {
				if err := os.Chtimes(path, age, age); err != nil {
					t.Fatal(err)
				}
			}
		}
		return dir
	}
	makeRun("old", true, old, nil)
	makeRun("recent", true, now.Add(-5*24*time.Hour), nil)
	makeRun("resumable", false, now.Add(-45*24*time.Hour), nil)
	makeRun("abandoned", false, now.Add(-91*24*time.Hour), nil)
	makeRun("stateless", false, old, nil)
	external := filepath.Join(home, "outside")
	if err := os.WriteFile(external, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	externalRun := makeRun("external", true, old, nil)
	if err := os.Symlink(external, filepath.Join(externalRun, "outside-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(externalRun, old, old); err != nil {
		t.Fatal(err)
	}
	makeRun("source", true, old, &model.AuditMetadata{Links: []model.AuditLink{{AuditRunID: "audit"}}})
	makeRun("audit", true, old, &model.AuditMetadata{SourceRunID: "source"})
	makeRun("source-protected", true, now.Add(-60*24*time.Hour), &model.AuditMetadata{Links: []model.AuditLink{{AuditRunID: "audit-recent"}}})
	makeRun("audit-recent", false, now.Add(-10*24*time.Hour), &model.AuditMetadata{SourceRunID: "source-protected"})
	sealed := filepath.Join(runsDir, "source", "audit-snapshots", "snapshot")
	if err := os.MkdirAll(sealed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sealed, "sealed"), []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sealed, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(runsDir, "source"), old, old); err != nil {
		t.Fatal(err)
	}
	makeRun("damaged", true, old, nil)
	if err := os.WriteFile(filepath.Join(runsDir, "damaged", "state.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	makeRun("inconsistent", true, old, nil)
	if err := os.WriteFile(filepath.Join(runsDir, "inconsistent", "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateLink := makeRun("state-link", true, old, nil)
	if err := os.Remove(filepath.Join(stateLink, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(stateLink, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stateLink, old, old); err != nil {
		t.Fatal(err)
	}
	makeRun("unknown-lock", true, old, nil)
	if err := os.Mkdir(filepath.Join(runsDir, "unknown-lock", "lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	trash := filepath.Join(runsDir, ".pruning-leftover")
	if err := os.Mkdir(trash, 0o500); err != nil {
		t.Fatal(err)
	}
	beforeList, err := runs.ListForDir(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range beforeList {
		if strings.HasPrefix(item.SessionID, ".") {
			t.Fatalf("trash appeared in run list: %s", item.SessionID)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".agent-runner", "settings.yaml"), []byte("run_retention: {max_runs_per_project: 100}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(home, ".agent-runner", "retention", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(statePath, retentionState{ActivatedAt: now.Add(-8 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	report := Sweep(home, now)
	if report.Notice != "" {
		t.Fatalf("unexpected notice: %s", report.Notice)
	}
	for _, id := range []string{"old", "abandoned", "stateless", "external", "source", "audit"} {
		if _, err := os.Stat(filepath.Join(runsDir, id)); !os.IsNotExist(err) {
			t.Errorf("%s survived: %v", id, err)
		}
	}
	for _, id := range []string{"recent", "resumable", "source-protected", "audit-recent", "damaged", "inconsistent", "state-link", "unknown-lock"} {
		if _, err := os.Stat(filepath.Join(runsDir, id)); err != nil {
			t.Errorf("%s removed: %v", id, err)
		}
	}
	if _, err := os.Stat(trash); !os.IsNotExist(err) {
		t.Errorf("trash survived: %v", err)
	}
	if body, err := os.ReadFile(external); err != nil || string(body) != "keep" {
		t.Errorf("symlink target changed: %v %q", err, body)
	}
	listed, err := runs.ListForDir(project)
	if err != nil || len(listed) != 8 {
		t.Fatalf("listing: %d, %v", len(listed), err)
	}
	if len(report.Warnings) < 2 {
		t.Fatalf("expected protected warnings, got %v", report.Warnings)
	}
}

func TestMalformedSettingsFailClosed(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := filepath.Join(home, ".agent-runner", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".agent-runner", "projects", "p", "runs", "old")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "old", WorkflowFile: "builtin:test-v1.0.yaml", Completed: true}, dir); err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(-40 * 24 * time.Hour)
	for _, p := range []string{filepath.Join(dir, "state.json"), dir} {
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	statePath := filepath.Join(home, ".agent-runner", "retention", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(statePath, retentionState{ActivatedAt: now.Add(-8 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"run_retention: [", "7\n", "run_retention: 7\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		r := Sweep(home, time.Now())
		if len(r.Warnings) == 0 || r.Notice != "" {
			t.Fatalf("body %q: %+v", body, r)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("malformed settings removed run: %v", err)
		}
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err != nil {
		r := Sweep(home, time.Now())
		if len(r.Warnings) == 0 || r.Notice != "" {
			t.Fatalf("unreadable settings: %+v", r)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("unreadable settings removed run: %v", err)
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("autonomous_backend: bogus\nrun_retention: {enabled: false}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := Sweep(home, time.Now())
	if len(r.Warnings) != 0 || r.Notice != "" {
		t.Fatalf("opt out: %+v", r)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("opt out removed run: %v", err)
	}
}

func TestSweepScopeAndCount(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	base := filepath.Join(home, ".agent-runner")
	root := filepath.Join(base, "projects")
	second := filepath.Join(root, "second", "runs")
	outside := filepath.Join(home, "outside")
	for _, dir := range []string{second, outside, filepath.Join(base, "onboarding", "runs")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 103; i++ {
		id := fmt.Sprintf("run-%03d", i)
		dir := filepath.Join(second, id)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := stateio.WriteState(&model.RunState{RunID: id, WorkflowFile: "builtin:test-v1.0.yaml", Completed: true}, dir); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-time.Duration(i+1) * time.Minute)
		for _, p := range []string{dir, filepath.Join(dir, "state.json")} {
			if err := os.Chtimes(p, stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(second, "linked-run")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "linked-runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-runs", "runs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "onboarding", "runs", "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(base, "retention", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(statePath, retentionState{ActivatedAt: now.Add(-8 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	r := Sweep(home, now)
	if len(r.Removed) != 3 {
		t.Fatalf("removed %d, want 3: %+v", len(r.Removed), r)
	}
	for i := 100; i < 103; i++ {
		if _, err := os.Stat(filepath.Join(second, fmt.Sprintf("run-%03d", i))); !os.IsNotExist(err) {
			t.Errorf("oldest %d survived", i)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Errorf("outside modified: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "onboarding", "runs", "old")); err != nil {
		t.Errorf("onboarding modified: %v", err)
	}
	if len(r.Warnings) < 3 {
		t.Errorf("missing symlink warnings: %v", r.Warnings)
	}
}

func TestSweepAcrossProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture")
	}
	if os.Getenv("RUN_RETENTION_HELPER") == "1" {
		home := os.Getenv("HOME")
		report := Sweep(home, time.Now())
		if err := json.NewEncoder(os.Stdout).Encode(report.Removed); err != nil {
			t.Fatal(err)
		}
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Now()
	dir := filepath.Join(home, ".agent-runner", "projects", "p", "runs", "old")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "old", WorkflowFile: "builtin:test-v1.0.yaml", Completed: true}, dir); err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(-40 * 24 * time.Hour)
	for _, p := range []string{filepath.Join(dir, "state.json"), dir} {
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	statePath := filepath.Join(home, ".agent-runner", "retention", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(statePath, retentionState{ActivatedAt: now.Add(-8 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var cmds []*exec.Cmd
	for i := 0; i < 2; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=TestSweepAcrossProcesses$")
		cmd.Env = append(os.Environ(), "RUN_RETENTION_HELPER=1")
		cmds = append(cmds, cmd)
	}
	var outputs [2]bytes.Buffer
	for i, cmd := range cmds {
		cmd.Stdout = &outputs[i]
		cmd.Stderr = &outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper %d: %v %s", i, err, outputs[i].String())
		}
	}
	removed := 0
	for _, out := range outputs {
		var ids []string
		line, _, _ := bytes.Cut(out.Bytes(), []byte("\n"))
		if err := json.Unmarshal(line, &ids); err != nil {
			t.Fatalf("decode helper %q: %v", out.String(), err)
		}
		removed += len(ids)
	}
	if removed != 1 {
		t.Fatalf("removed by helpers = %d, want 1", removed)
	}
	if third := Sweep(home, time.Now()); len(third.Removed) != 0 {
		t.Fatalf("daily throttle failed: %+v", third)
	}
	// A crash leaves a started timestamp ahead of completion. The hourly
	// retry gate applies independently of the 24-hour completion gate.
	started := time.Now().Add(-30 * time.Minute)
	if err := stateio.WriteJSONAtomic(statePath, retentionState{ActivatedAt: now.Add(-8 * 24 * time.Hour), LastCompletedAt: now.Add(-25 * time.Hour), LastStartedAt: started}); err != nil {
		t.Fatal(err)
	}
	if dueState, _ := readRetentionState(statePath); due(dueState, time.Now()) {
		t.Fatal("retried incomplete sweep before one hour")
	}
	if dueState, _ := readRetentionState(statePath); !due(dueState, time.Now().Add(2*time.Hour)) {
		t.Fatal("did not retry incomplete sweep after one hour")
	}
}

func TestSweepRelocatedRoot(t *testing.T) {
	home := t.TempDir()
	storage := t.TempDir()
	if err := os.Symlink(storage, filepath.Join(home, ".agent-runner")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	dir := filepath.Join(storage, "projects", "p", "runs", "old")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "old", WorkflowFile: "builtin:test-v1.0.yaml", Completed: true}, dir); err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(-40 * 24 * time.Hour)
	for _, p := range []string{filepath.Join(dir, "state.json"), dir} {
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(storage, "retention", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(path, retentionState{ActivatedAt: now.Add(-8 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	r := Sweep(home, now)
	if len(r.Removed) != 1 {
		t.Fatalf("relocated sweep: %+v", r)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("run survived: %v", err)
	}
}

func TestSweepGracePeriod(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	dir := filepath.Join(home, ".agent-runner", "projects", "p", "runs", "old")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "old", WorkflowFile: "builtin:test-v1.0.yaml", Completed: true}, dir); err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(-40 * 24 * time.Hour)
	for _, p := range []string{filepath.Join(dir, "state.json"), dir} {
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(home, ".agent-runner", "retention", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(path, retentionState{ActivatedAt: now.Add(-3 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	r := Sweep(home, now)
	if r.Notice != "" || len(r.Removed) != 0 {
		t.Fatalf("grace sweep removed run: %+v", r)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("run removed during grace: %v", err)
	}
}

func TestFinishBoundedWithPartialWarnings(t *testing.T) {
	background.Lock()
	previousStarted, previousDone, previousReport := background.started, background.done, background.report
	background.started = true
	background.done = make(chan struct{})
	background.report = Report{Warnings: []string{"partial failure"}}
	background.Unlock()
	defer func() {
		background.Lock()
		background.started, background.done, background.report = previousStarted, previousDone, previousReport
		background.Unlock()
	}()
	var output bytes.Buffer
	started := time.Now()
	Finish(&output, 20*time.Millisecond)
	if time.Since(started) > 200*time.Millisecond {
		t.Fatal("Finish exceeded bounded wait")
	}
	if !strings.Contains(output.String(), "agent-runner: warning: run retention: partial failure") {
		t.Fatalf("missing partial warning: %q", output.String())
	}
}

func TestNoticeDisabledLimits(t *testing.T) {
	if got := describeAge("finished runs", 0); !strings.Contains(got, "disabled") {
		t.Fatal(got)
	}
	if got := describeCount(0); !strings.Contains(got, "disabled") {
		t.Fatal(got)
	}
}
