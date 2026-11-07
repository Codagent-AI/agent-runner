package builtinworkflows

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionEvidenceFixtures(t *testing.T) {
	for _, tc := range []struct {
		name, row                string
		evidenceOK, passed, only bool
	}{
		{"verified", "| R-1 | a \\| b | `ok.log` | **Verified** |", true, true, false},
		{"failed", "| R-1 | a | broken | failed |", true, false, false},
		{"planned edit", "| R-1 | changed | `ok.log` | verified |", false, false, false},
		{"unverified", "| R-1 | a | needs credentials | not verified |", true, false, true},
		{"no reason", "| R-1 | a | | not verified |", false, false, false},
		{"no citation", "| R-1 | a | ok.log | verified |", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCompletionFixture(t)
			f.write("checklist.md", "| ID | Behavior | Evidence | Status |\n|---|---|---|---|\n"+tc.row+"\n")
			if tc.name == "planned edit" {
				f.write("plan.md", "| ID | Behavior | Evidence | Status |\n|---|---|---|---|\n| R-1 | original | | |\n")
				f.input["plan_file"] = filepath.Join(f.dir, "plan.md")
			}
			f.commit("feat: implement")
			f.write("complete.md", "done")
			out, err := f.run("check-evidence.sh")
			if (err == nil) != tc.evidenceOK {
				t.Fatalf("evidence: %v %s", err, out)
			}
			out, err = f.run("check-implementation-complete.sh")
			if (err == nil) != tc.passed {
				t.Fatalf("completion: %v %s", err, out)
			}
			var report struct {
				Passed bool `json:"passed"`
				Only   bool `json:"only_unverified_with_reason"`
			}
			b, e := os.ReadFile(filepath.Join(f.dir, "completion-check.json"))
			if e != nil {
				t.Fatal(e)
			}
			if e = json.Unmarshal(b, &report); e != nil {
				t.Fatal(e)
			}
			if report.Passed != tc.passed || report.Only != tc.only {
				t.Fatalf("report=%s", b)
			}
			if !tc.passed {
				b, _ = os.ReadFile(filepath.Join(f.dir, "completion-problems.txt"))
				if !strings.Contains(string(b), "R-1") {
					t.Fatalf("missing row: %s", b)
				}
			}
		})
	}
}

type completionFixture struct {
	t            *testing.T
	dir, scripts string
	input        map[string]string
}

func newCompletionFixture(t *testing.T) *completionFixture {
	t.Helper()
	dir := t.TempDir()
	scripts := t.TempDir()
	for _, name := range []string{"check-evidence.sh", "check-implementation-complete.sh", "check-implementation-progress.sh", "check-completion-verdict.sh"} {
		b, e := ReadAsset("core/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(scripts, name), b, 0o700); e != nil {
			t.Fatal(e)
		}
	}
	f := &completionFixture{t: t, dir: dir, scripts: scripts, input: map[string]string{}}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.com"}, {"config", "commit.gpgsign", "false"}} {
		f.git(args...)
	}
	f.write("checklist.md", "| ID | Behavior | Evidence | Status |\n|---|---|---|---|\n| R-1 | works | `ok.log` | verified |\n")
	f.commit("docs: plan")
	f.input["start_head"] = f.git("rev-parse", "HEAD")
	for key, name := range map[string]string{"checklist_file": "checklist.md", "evidence_dir": "evidence", "problems_file": "evidence-problems.txt", "completion_file": "complete.md", "report_dir": ".", "verdict_file": "completion-verdict.json", "check_file": "completion-check.json", "ledger_file": "completion-loop.jsonl", "blocked_file": "blocked.md", "remaining_file": "remaining.md", "snapshot_file": "continue-snapshot.json"} {
		f.input[key] = filepath.Join(dir, name)
	}
	f.input["max_continue"] = "3"
	f.input["max_fix_record"] = "2"
	if e := os.Mkdir(filepath.Join(dir, "evidence"), 0o700); e != nil {
		t.Fatal(e)
	}
	f.write("evidence/ok.log", "PASS")
	return f
}
func (f *completionFixture) write(name, body string) {
	f.t.Helper()
	if e := os.WriteFile(filepath.Join(f.dir, name), []byte(body), 0o600); e != nil {
		f.t.Fatal(e)
	}
}
func (f *completionFixture) git(args ...string) string {
	f.t.Helper()
	c := exec.Command("git", args...)
	c.Dir = f.dir
	b, e := c.CombinedOutput()
	if e != nil {
		f.t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func (f *completionFixture) commit(subject string) {
	f.git("add", "checklist.md")
	f.git("commit", "-q", "--allow-empty", "-m", subject)
}
func (f *completionFixture) run(name string) (string, error) {
	f.t.Helper()
	b, _ := json.Marshal(f.input)
	c := exec.Command("sh", filepath.Join(f.scripts, name))
	c.Dir = f.dir
	c.Stdin = strings.NewReader(string(b))
	out, e := c.CombinedOutput()
	return string(out), e
}
func (f *completionFixture) mustRun(name string) string {
	f.t.Helper()
	out, e := f.run(name)
	if e != nil {
		f.t.Fatalf("%s: %v %s", name, e, out)
	}
	return out
}

func TestCompletionConditions(t *testing.T) {
	f := newCompletionFixture(t)
	f.commit("[judge-completion] docs: records")
	out, e := f.run("check-implementation-complete.sh")
	if e == nil || !strings.Contains(out, "completion record") || !strings.Contains(out, "implementation commit") {
		t.Fatalf("%v %s", e, out)
	}
	f.write("complete.md", "done")
	f.commit("feat: work")
	f.mustRun("check-implementation-complete.sh")
	if _, e = os.Stat(f.input["problems_file"]); !os.IsNotExist(e) {
		t.Fatalf("evidence problems remain: %v", e)
	}
	f.write("plan.md", "| ID | Behavior | Evidence | Status |\n|---|---|---|---|\n| R-1 | original | | |\n")
	f.input["plan_file"] = filepath.Join(f.dir, "plan.md")
	out, e = f.run("check-implementation-complete.sh")
	if e == nil || !strings.Contains(out, "R-1") {
		t.Fatalf("%v %s", e, out)
	}
}

func TestCompletionVerdictValidation(t *testing.T) {
	for _, tc := range []struct{ name, verdict string }{{"missing", ""}, {"malformed", "{"}, {"unknown", `{"verdict":"yes"}`}, {"empty", `{"verdict":"continue","remaining":[]}`}, {"wrong type", `{"verdict":"continue","remaining":[1]}`}, {"complete", `{"verdict":"complete"}`}, {"blocked", `{"verdict":"blocked"}`}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCompletionFixture(t)
			f.mustRun("check-implementation-progress.sh")
			if tc.verdict != "" {
				f.write("completion-verdict.json", tc.verdict)
			}
			out, e := f.run("check-completion-verdict.sh")
			if e == nil || !strings.HasPrefix(out, "completion judge returned no valid verdict:") {
				t.Fatalf("%v %s", e, out)
			}
		})
	}
}

func TestCompletionLedgerAndStall(t *testing.T) {
	f := newCompletionFixture(t)
	f.mustRun("check-implementation-progress.sh")
	f.mustRun("check-implementation-progress.sh")
	f.write("completion-verdict.json", `{"verdict":"continue","remaining":["finish R-1"]}`)
	first := f.mustRun("check-completion-verdict.sh")
	second := f.mustRun("check-completion-verdict.sh")
	if first != second {
		t.Fatalf("capture changed: %s %s", first, second)
	}
	var capture map[string]string
	if e := json.Unmarshal([]byte(first), &capture); e != nil || capture["remaining"] != "- finish R-1" {
		t.Fatalf("capture %s: %v", first, e)
	}
	b, _ := os.ReadFile(f.input["ledger_file"])
	if strings.Count(string(b), "\n") != 1 {
		t.Fatalf("ledger %s", b)
	}
	out, e := f.run("check-implementation-progress.sh")
	if e == nil || !strings.HasPrefix(out, "implementation stalled: continue pass 1") {
		t.Fatalf("%v %s", e, out)
	}
	f.commit("feat: progress")
	f.mustRun("check-implementation-progress.sh")
	if _, e = os.Stat(f.input["snapshot_file"]); !os.IsNotExist(e) {
		t.Fatal("snapshot not removed")
	}
}

func TestCompletionVerdictBounds(t *testing.T) {
	for _, tc := range []struct {
		verdict string
		max     int
		message string
	}{{"continue", 3, "implementation incomplete: 1 items remain after 3 continue passes"}, {"fix-record", 2, "implementation record could not be fixed after 2 fix-record verdicts"}} {
		t.Run(tc.verdict, func(t *testing.T) {
			f := newCompletionFixture(t)
			for n := 0; n <= tc.max; n++ {
				f.mustRun("check-implementation-progress.sh")
				f.write("completion-verdict.json", `{"verdict":"`+tc.verdict+`","remaining":["finish R-1"]}`)
				out, e := f.run("check-completion-verdict.sh")
				if n == tc.max {
					if e == nil || !strings.HasPrefix(out, tc.message) {
						t.Fatalf("%v %s", e, out)
					}
				} else if e != nil {
					t.Fatalf("%v %s", e, out)
				}
				if tc.verdict == "continue" {
					f.commit("feat: progress")
				}
			}
		})
	}
}

func TestCompletionProgressChecklistOnlyAndSnapshotIdempotence(t *testing.T) {
	f := newCompletionFixture(t)
	f.mustRun("check-implementation-progress.sh")
	f.write("completion-verdict.json", `{"verdict":"continue","remaining":["finish"]}`)
	f.mustRun("check-completion-verdict.sh")
	original, e := os.ReadFile(f.input["snapshot_file"])
	if e != nil {
		t.Fatal(e)
	}
	f.commit("feat: progress before interrupt")
	f.mustRun("check-completion-verdict.sh")
	after, e := os.ReadFile(f.input["snapshot_file"])
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(original, after) {
		t.Fatal("resumed verdict refreshed snapshot")
	}
	f.mustRun("check-implementation-progress.sh")
	f.write("completion-verdict.json", `{"verdict":"continue","remaining":["finish"]}`)
	f.mustRun("check-completion-verdict.sh")
	f.write("checklist.md", "changed checklist only")
	f.mustRun("check-implementation-progress.sh")
}

func TestCompletionAcceptedExceptionalVerdicts(t *testing.T) {
	for _, word := range []string{"complete", "blocked"} {
		t.Run(word, func(t *testing.T) {
			f := newCompletionFixture(t)
			f.mustRun("check-implementation-progress.sh")
			f.write("completion-check.json", `{"only_unverified_with_reason":true}`)
			f.write("blocked.md", "decision needed")
			f.write("completion-verdict.json", `{"verdict":"`+word+`","remaining":[]}`)
			out := f.mustRun("check-completion-verdict.sh")
			var capture map[string]string
			if e := json.Unmarshal([]byte(out), &capture); e != nil || capture["verdict"] != word || capture["remaining"] != "" {
				t.Fatalf("capture %s %v", out, e)
			}
		})
	}
}
