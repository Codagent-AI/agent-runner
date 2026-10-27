package builtinworkflows

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newScratchRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "feature"}, {"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"}, {"config", "commit.gpgsign", "false"},
	} {
		runCmdIn(t, dir, "", "git", args...)
	}
	commitFile(t, dir, "one")
	return dir
}

func commitFile(t *testing.T, dir, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(msg+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCmdIn(t, dir, "", "git", "add", "f.txt")
	runCmdIn(t, dir, "", "git", "commit", "-q", "-m", msg)
	return strings.TrimSpace(runCmdIn(t, dir, "", "git", "rev-parse", "HEAD"))
}

func runCmdIn(t *testing.T, dir, stdin, name string, args ...string) string {
	t.Helper()
	cmd := osexec.Command(name, args...)
	cmd.Dir = dir
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func materializeAsset(t *testing.T, asset string) string {
	t.Helper()
	body, err := ReadAsset(asset)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), filepath.Base(asset))
	if err := os.WriteFile(path, body, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func readText(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestValidationHandoffRecordsResultAndLaterCommits(t *testing.T) {
	script := materializeAsset(t, "core/validation-handoff.sh")
	for _, tc := range []struct {
		name, result string
		commitAfter  bool
		want         []string
	}{
		{"pass no commits", "PASS\n", false, []string{"Validation passed.", "No commits were made after the tested revision"}},
		{"skipped", "SKIPPED\nno config\n", false, []string{"Validation was skipped. no config"}},
		{"fail with fixes", "FAIL\ncheck: lint\n", true, []string{"findings are unresolved", "    check: lint", "validation fix", "not exploratory-tested"}},
		{"empty result", "", false, []string{"recorded no result"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newScratchRepo(t)
			evidence := t.TempDir()
			tested := strings.TrimSpace(runCmdIn(t, repo, "", "git", "rev-parse", "HEAD"))
			if err := os.WriteFile(filepath.Join(evidence, "acceptance-tested-revision.txt"), []byte(tested+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(evidence, "acceptance-handoff.md"), []byte("# Handoff\n\nready\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			resultFile := filepath.Join(t.TempDir(), "result.txt")
			if err := os.WriteFile(resultFile, []byte(tc.result), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.commitAfter {
				commitFile(t, repo, "validation fix")
			}
			payload := `{"evidence_dir":"` + evidence + `","result_file":"` + resultFile + `"}`
			// Running twice must not duplicate the section.
			runCmdIn(t, repo, payload, "sh", script)
			runCmdIn(t, repo, payload, "sh", script)
			got := readText(t, filepath.Join(evidence, "acceptance-handoff.md"))
			if strings.Count(got, "## Validation") != 1 || !strings.HasPrefix(got, "# Handoff\n\nready\n") {
				t.Errorf("handoff not replaced cleanly:\n%s", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("handoff lacks %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestValidationHandoffRequiresEvidenceDir(t *testing.T) {
	script := materializeAsset(t, "core/validation-handoff.sh")
	cmd := osexec.Command("sh", script)
	cmd.Dir = newScratchRepo(t)
	cmd.Stdin = strings.NewReader("{}")
	if err := cmd.Run(); err == nil {
		t.Error("script succeeded without evidence_dir")
	}
}

func TestRecordUntestedCommits(t *testing.T) {
	script := materializeAsset(t, "core/record-untested-commits.sh")
	t.Run("lists commits after the tested revision", func(t *testing.T) {
		repo := newScratchRepo(t)
		evidence := t.TempDir()
		tested := strings.TrimSpace(runCmdIn(t, repo, "", "git", "rev-parse", "HEAD"))
		_ = os.WriteFile(filepath.Join(evidence, "acceptance-tested-revision.txt"), []byte(tested+"\n"), 0o600)
		commitFile(t, repo, "late fix")
		payload := `{"evidence_dir":"` + evidence + `"}`
		runCmdIn(t, repo, payload, "sh", script)
		runCmdIn(t, repo, payload, "sh", script)
		got := readText(t, filepath.Join(evidence, "acceptance-handoff.md"))
		if strings.Count(got, "## Commits not exploratory-tested") != 1 || !strings.Contains(got, "late fix") {
			t.Errorf("handoff:\n%s", got)
		}
	})
	t.Run("writes nothing when HEAD was tested", func(t *testing.T) {
		repo := newScratchRepo(t)
		evidence := t.TempDir()
		head := strings.TrimSpace(runCmdIn(t, repo, "", "git", "rev-parse", "HEAD"))
		_ = os.WriteFile(filepath.Join(evidence, "acceptance-tested-revision.txt"), []byte(head+"\n"), 0o600)
		runCmdIn(t, repo, `{"evidence_dir":"`+evidence+`"}`, "sh", script)
		if got := readText(t, filepath.Join(evidence, "acceptance-handoff.md")); strings.Contains(got, "not exploratory-tested") {
			t.Errorf("handoff has a section for a tested HEAD:\n%s", got)
		}
	})
	t.Run("notes a missing tested revision", func(t *testing.T) {
		repo := newScratchRepo(t)
		evidence := t.TempDir()
		runCmdIn(t, repo, `{"evidence_dir":"`+evidence+`"}`, "sh", script)
		if got := readText(t, filepath.Join(evidence, "acceptance-handoff.md")); !strings.Contains(got, "No tested revision is recorded") {
			t.Errorf("handoff:\n%s", got)
		}
	})
}

// checkDraftPR runs check-draft-pr.sh with a gh stub that reports one open pull
// request at the repository's HEAD with the given draft state.
func checkDraftPR(t *testing.T, draft bool, acceptReady string) (string, error) {
	t.Helper()
	repo := newScratchRepo(t)
	head := strings.TrimSpace(runCmdIn(t, repo, "", "git", "rev-parse", "HEAD"))
	bin := t.TempDir()
	isDraft := "false"
	if draft {
		isDraft = "true"
	}
	stub := "#!/bin/sh\nprintf '[{\"number\":1,\"url\":\"https://example.test/pr/1\",\"state\":\"OPEN\",\"isDraft\":" + isDraft + ",\"baseRefName\":\"main\",\"headRefOid\":\"" + head + "\"}]'\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	script := materializeAsset(t, "core/check-draft-pr.sh")
	cmd := osexec.Command("sh", script)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	payload := "{}"
	if acceptReady != "" {
		payload = `{"accept_ready":"` + acceptReady + `"}`
	}
	cmd.Stdin = strings.NewReader(payload)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCheckDraftPRAcceptReady(t *testing.T) {
	for _, tc := range []struct {
		name        string
		draft       bool
		acceptReady string
		wantOK      bool
	}{
		{"draft by default", true, "", true},
		{"ready rejected by default", false, "", false},
		{"ready rejected when false", false, "false", false},
		{"ready accepted when true", false, "true", true},
		{"draft accepted when true", true, "true", true},
		{"invalid value", true, "yes", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := checkDraftPR(t, tc.draft, tc.acceptReady)
			if (err == nil) != tc.wantOK {
				t.Fatalf("ok = %v, want %v; output %q", err == nil, tc.wantOK, out)
			}
			if tc.wantOK && !strings.Contains(out, "https://example.test/pr/1") {
				t.Errorf("output %q lacks the pull request URL", out)
			}
		})
	}
}
