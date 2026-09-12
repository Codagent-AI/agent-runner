package builtinworkflows

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeScript(t *testing.T, relAsset string) string {
	t.Helper()
	script, err := ReadAsset(relAsset)
	if err != nil {
		t.Fatalf("ReadAsset(%s): %v", relAsset, err)
	}
	scriptPath := filepath.Join(t.TempDir(), filepath.Base(relAsset))
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}

func runScript(t *testing.T, scriptPath, stdin string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", scriptPath)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestRecordTriageScript(t *testing.T) {
	scriptPath := writeScript(t, "core/record-triage.sh")

	t.Run("declines with reasons and writes needs-input outcome", func(t *testing.T) {
		outcomePath := filepath.Join(t.TempDir(), "fix-outcome.json")
		payload := map[string]string{
			"decision":     `{"fixable": false, "reasons": ["several viable solutions require a human choice"], "plan": ""}`,
			"outcome_path": outcomePath,
		}
		body, _ := json.Marshal(payload)

		out, err := runScript(t, scriptPath, string(body))
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}
		if strings.TrimSpace(out) != "false" {
			t.Fatalf("stdout = %q, want %q", out, "false")
		}

		data, err := os.ReadFile(outcomePath)
		if err != nil {
			t.Fatalf("read outcome file: %v", err)
		}
		var written struct {
			Contract string   `json:"contract"`
			Outcome  string   `json:"outcome"`
			Reasons  []string `json:"reasons"`
		}
		if err := json.Unmarshal(data, &written); err != nil {
			t.Fatalf("unmarshal outcome: %v\n%s", err, data)
		}
		if written.Contract != "factory-fix/1" {
			t.Fatalf("contract = %q, want factory-fix/1", written.Contract)
		}
		if written.Outcome != "needs-input" {
			t.Fatalf("outcome = %q, want needs-input", written.Outcome)
		}
		if len(written.Reasons) != 1 || written.Reasons[0] != "several viable solutions require a human choice" {
			t.Fatalf("reasons = %v, want the supplied reason", written.Reasons)
		}
	})

	t.Run("fixable decision writes no outcome file and reports true", func(t *testing.T) {
		outcomePath := filepath.Join(t.TempDir(), "fix-outcome.json")
		payload := map[string]string{
			"decision":     `{"fixable": true, "reasons": [], "plan": "add a regression test and fix the off-by-one error"}`,
			"outcome_path": outcomePath,
		}
		body, _ := json.Marshal(payload)

		out, err := runScript(t, scriptPath, string(body))
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}
		if strings.TrimSpace(out) != "true" {
			t.Fatalf("stdout = %q, want %q", out, "true")
		}
		if _, err := os.Stat(outcomePath); !os.IsNotExist(err) {
			t.Fatalf("expected no outcome file to be written for a fixable decision, err=%v", err)
		}
	})

	t.Run("rejects a triage decision missing the fixable field", func(t *testing.T) {
		outcomePath := filepath.Join(t.TempDir(), "fix-outcome.json")
		payload := map[string]string{
			"decision":     `{"reasons": []}`,
			"outcome_path": outcomePath,
		}
		body, _ := json.Marshal(payload)

		out, err := runScript(t, scriptPath, string(body))
		if err == nil {
			t.Fatalf("expected failure for a decision missing 'fixable':\n%s", out)
		}
	})

	t.Run("rejects malformed JSON input", func(t *testing.T) {
		out, err := runScript(t, scriptPath, `not json`)
		if err == nil {
			t.Fatalf("expected failure for malformed input:\n%s", out)
		}
	})
}

func TestRecordOutcomeScript(t *testing.T) {
	scriptPath := writeScript(t, "core/record-outcome.sh")

	t.Run("pull-request outcome when validator and CI both pass", func(t *testing.T) {
		outcomePath := filepath.Join(t.TempDir(), "fix-outcome.json")
		payload := map[string]string{
			"outcome_path":     outcomePath,
			"validator_status": "passed",
			"ci_status":        "passed",
			"branch_name":      "factory/fix-212-1a2b3c4d",
			"pr_details":       `{"url": "https://github.com/o/r/pull/214", "number": 214, "headRefOid": "deadbeef"}`,
		}
		body, _ := json.Marshal(payload)

		out, err := runScript(t, scriptPath, string(body))
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}

		data, err := os.ReadFile(outcomePath)
		if err != nil {
			t.Fatalf("read outcome file: %v", err)
		}
		var written struct {
			Contract string `json:"contract"`
			Outcome  string `json:"outcome"`
			PR       *struct {
				URL     string `json:"url"`
				Number  int    `json:"number"`
				Branch  string `json:"branch"`
				HeadSHA string `json:"head_sha"`
			} `json:"pr"`
			Validator struct {
				Status string `json:"status"`
			} `json:"validator"`
			CI struct {
				Status string `json:"status"`
			} `json:"ci"`
		}
		if err := json.Unmarshal(data, &written); err != nil {
			t.Fatalf("unmarshal outcome: %v\n%s", err, data)
		}
		if written.Outcome != "pull-request" {
			t.Fatalf("outcome = %q, want pull-request", written.Outcome)
		}
		if written.PR == nil || written.PR.URL != "https://github.com/o/r/pull/214" || written.PR.Number != 214 {
			t.Fatalf("pr = %+v, want populated PR reference", written.PR)
		}
		if written.PR.Branch != "factory/fix-212-1a2b3c4d" {
			t.Fatalf("pr.branch = %q, want the fix branch", written.PR.Branch)
		}
		if written.PR.HeadSHA != "deadbeef" {
			t.Fatalf("pr.head_sha = %q, want deadbeef", written.PR.HeadSHA)
		}
		if written.Validator.Status != "passed" {
			t.Fatalf("validator.status = %q, want passed", written.Validator.Status)
		}
		if written.CI.Status != "passed" {
			t.Fatalf("ci.status = %q, want passed", written.CI.Status)
		}
	})

	t.Run("failed outcome when validator never passes, no PR reference", func(t *testing.T) {
		outcomePath := filepath.Join(t.TempDir(), "fix-outcome.json")
		payload := map[string]string{
			"outcome_path":     outcomePath,
			"validator_status": "failed",
			"ci_status":        "",
			"branch_name":      "factory/fix-212-1a2b3c4d",
			"pr_details":       "{}",
		}
		body, _ := json.Marshal(payload)

		out, err := runScript(t, scriptPath, string(body))
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}

		data, err := os.ReadFile(outcomePath)
		if err != nil {
			t.Fatalf("read outcome file: %v", err)
		}
		var written struct {
			Outcome string          `json:"outcome"`
			PR      json.RawMessage `json:"pr"`
			Reasons []string        `json:"reasons"`
		}
		if err := json.Unmarshal(data, &written); err != nil {
			t.Fatalf("unmarshal outcome: %v\n%s", err, data)
		}
		if written.Outcome != "failed" {
			t.Fatalf("outcome = %q, want failed", written.Outcome)
		}
		if written.PR != nil {
			t.Fatalf("pr = %s, want absent when the validator never passed", written.PR)
		}
		if len(written.Reasons) == 0 {
			t.Fatal("expected at least one reason for the failure")
		}
	})

	t.Run("failed outcome when CI stays red, PR reference retained and left open", func(t *testing.T) {
		outcomePath := filepath.Join(t.TempDir(), "fix-outcome.json")
		payload := map[string]string{
			"outcome_path":     outcomePath,
			"validator_status": "passed",
			"ci_status":        "failed",
			"branch_name":      "factory/fix-212-1a2b3c4d",
			"pr_details":       `{"url": "https://github.com/o/r/pull/214", "number": 214, "headRefOid": "deadbeef"}`,
		}
		body, _ := json.Marshal(payload)

		out, err := runScript(t, scriptPath, string(body))
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}

		data, err := os.ReadFile(outcomePath)
		if err != nil {
			t.Fatalf("read outcome file: %v", err)
		}
		var written struct {
			Outcome string `json:"outcome"`
			PR      *struct {
				URL string `json:"url"`
			} `json:"pr"`
			CI struct {
				Status string `json:"status"`
			} `json:"ci"`
		}
		if err := json.Unmarshal(data, &written); err != nil {
			t.Fatalf("unmarshal outcome: %v\n%s", err, data)
		}
		if written.Outcome != "failed" {
			t.Fatalf("outcome = %q, want failed", written.Outcome)
		}
		if written.PR == nil || written.PR.URL == "" {
			t.Fatal("expected the PR reference to be retained so the PR is left open for a human")
		}
		if written.CI.Status != "failed" {
			t.Fatalf("ci.status = %q, want failed", written.CI.Status)
		}
	})

	t.Run("rejects malformed JSON input", func(t *testing.T) {
		out, err := runScript(t, scriptPath, `not json`)
		if err == nil {
			t.Fatalf("expected failure for malformed input:\n%s", out)
		}
	})

}
