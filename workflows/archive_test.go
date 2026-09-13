package builtinworkflows

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// archiveTestFixture wires up a temp Git repository plus the archive-transition
// and verify-archive-commit scripts, a fake `openspec` executable that performs
// the directory move, and a commit-msg hook enforcing a ticket-style subject.
type archiveTestFixture struct {
	t          *testing.T
	repo       string
	sessionDir string
	scriptsDir string
	env        []string
	changeName string
	changeDir  string
}

func newArchiveTestFixture(t *testing.T, changeName string) *archiveTestFixture {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "Agent Runner Test")
	runGit(t, repo, "config", "user.email", "agent-runner@example.com")

	scriptsDir := t.TempDir()
	for _, asset := range []string{
		"openspec/archive-transition.sh",
		"openspec/verify-archive-commit.sh",
		"openspec/validate-change-name.sh",
	} {
		data, err := ReadAsset(asset)
		if err != nil {
			t.Fatalf("ReadAsset(%s): %v", asset, err)
		}
		if err := os.WriteFile(filepath.Join(scriptsDir, filepath.Base(asset)), data, 0o700); err != nil {
			t.Fatalf("write %s: %v", asset, err)
		}
	}

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	fakeOpenSpec := `#!/bin/sh
set -eu
if [ "$1" = "validate" ]; then
  exit 0
fi
if [ "$1" = "archive" ]; then
  name="$2"
  mkdir -p openspec/changes/archive
  mv "openspec/changes/$name" "openspec/changes/archive/2026-09-03-$name"
  # Simulate merging the change's spec delta into the canonical spec.
  printf 'spec A merged for %s\n' "$name" > openspec/specs/spec-a-canonical.md
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "openspec"), []byte(fakeOpenSpec), 0o700); err != nil {
		t.Fatalf("write fake openspec: %v", err)
	}

	hooksDir := filepath.Join(repo, ".git", "hooks")
	commitMsgHook := `#!/bin/sh
subject=$(head -n1 "$1")
if [ "${#subject}" -gt 60 ]; then
  echo "commit-msg: subject longer than 60 characters" >&2
  exit 1
fi
case "$subject" in
  TICKET-123:*) ;;
  *)
    echo "commit-msg: subject must start with TICKET-123:" >&2
    exit 1
    ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(hooksDir, "commit-msg"), []byte(commitMsgHook), 0o700); err != nil {
		t.Fatalf("write commit-msg hook: %v", err)
	}

	changeDir := filepath.Join("openspec", "changes", changeName)
	if err := os.MkdirAll(filepath.Join(repo, filepath.Join("openspec", "specs")), 0o755); err != nil {
		t.Fatalf("create specs dir: %v", err)
	}
	mustWriteFile(t, filepath.Join(repo, "openspec", "specs", "spec-a.md"), "spec A original\n")
	mustWriteFile(t, filepath.Join(repo, "openspec", "specs", "spec-b.md"), "spec B original\n")
	if err := os.MkdirAll(filepath.Join(repo, changeDir), 0o755); err != nil {
		t.Fatalf("create change dir: %v", err)
	}
	mustWriteFile(t, filepath.Join(repo, changeDir, "proposal.md"), "proposal\n")
	runGit(t, repo, "add", "openspec")
	runGit(t, repo, "commit", "-m", "TICKET-123: seed fixture")

	sessionDir := t.TempDir()

	return &archiveTestFixture{
		t:          t,
		repo:       repo,
		sessionDir: sessionDir,
		scriptsDir: scriptsDir,
		env:        append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH")),
		changeName: changeName,
		changeDir:  changeDir,
	}
}

func (f *archiveTestFixture) run(t *testing.T, script string, stdin []byte) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", filepath.Join(f.scriptsDir, script))
	cmd.Dir = f.repo
	cmd.Env = f.env
	cmd.Stdin = strings.NewReader(string(stdin))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f *archiveTestFixture) runTransition(t *testing.T) (string, error) {
	t.Helper()
	stdin, err := json.Marshal(map[string]string{
		"change_name": f.changeName,
		"session_dir": f.sessionDir,
	})
	if err != nil {
		t.Fatalf("marshal transition input: %v", err)
	}
	return f.run(t, "archive-transition.sh", stdin)
}

func (f *archiveTestFixture) runVerify(t *testing.T, archiveState string) (string, error) {
	t.Helper()
	stdin, err := json.Marshal(map[string]json.RawMessage{
		"archive_state": json.RawMessage(archiveState),
		"change_name":   mustMarshal(t, f.changeName),
		"session_dir":   mustMarshal(t, f.sessionDir),
	})
	if err != nil {
		t.Fatalf("marshal verify input: %v", err)
	}
	return f.run(t, "verify-archive-commit.sh", stdin)
}

func mustMarshal(t *testing.T, v string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %q: %v", v, err)
	}
	return data
}

func archiveStateField(t *testing.T, archiveState, field string) string {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(archiveState), &decoded); err != nil {
		t.Fatalf("unmarshal archive state: %v\n%s", err, archiveState)
	}
	value, ok := decoded[field].(string)
	if !ok {
		t.Fatalf("archive state missing string field %q: %s", field, archiveState)
	}
	return value
}

func TestArchiveTransitionResolvesExactlyOneArchiveDirectory(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")

	out, err := f.runTransition(t)
	if err != nil {
		t.Fatalf("archive-transition failed: %v\n%s", err, out)
	}

	archiveDir := archiveStateField(t, out, "archive_dir")
	wantArchiveDir := filepath.Join("openspec", "changes", "archive", "2026-09-03-ticket-123-demo")
	if archiveDir != wantArchiveDir {
		t.Fatalf("archive_dir = %q, want %q", archiveDir, wantArchiveDir)
	}
	if changeDir := archiveStateField(t, out, "change_dir"); changeDir != f.changeDir {
		t.Fatalf("change_dir = %q, want %q", changeDir, f.changeDir)
	}
	startHead := archiveStateField(t, out, "start_head")
	if startHead == "" {
		t.Fatal("start_head is empty")
	}
	wantHead := strings.TrimSpace(string(runGitOutput(t, f.repo, "rev-parse", "HEAD")))
	if startHead != wantHead {
		t.Fatalf("start_head = %q, want %q", startHead, wantHead)
	}
}

func TestArchiveTransitionRejectsAmbiguousArchiveMatches(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")
	// Remove the active change so transition looks only at existing archives,
	// and seed two directories matching the change name pattern.
	runGit(t, f.repo, "rm", "-r", f.changeDir)
	runGit(t, f.repo, "commit", "-m", "TICKET-123: remove active change")
	for _, dir := range []string{
		"2026-09-01-ticket-123-demo",
		"2026-09-02-ticket-123-demo",
	} {
		full := filepath.Join(f.repo, "openspec", "changes", "archive", dir)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatalf("create archive dir: %v", err)
		}
		mustWriteFile(t, filepath.Join(full, "proposal.md"), "proposal\n")
	}

	out, err := f.runTransition(t)
	if err == nil {
		t.Fatalf("transition succeeded with ambiguous archive matches:\n%s", out)
	}
	if !strings.Contains(out, "expected exactly one archive directory") {
		t.Fatalf("output = %q, want ambiguity explanation", out)
	}
}

func TestArchiveTransitionRejectsMissingArchive(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")
	runGit(t, f.repo, "rm", "-r", f.changeDir)
	runGit(t, f.repo, "commit", "-m", "TICKET-123: remove active change")

	out, err := f.runTransition(t)
	if err == nil {
		t.Fatalf("transition succeeded with no archive present:\n%s", out)
	}
	if !strings.Contains(out, "expected exactly one archive directory") {
		t.Fatalf("output = %q, want missing-archive explanation", out)
	}
}

func TestArchiveTransitionReplayReusesSnapshot(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")

	first, err := f.runTransition(t)
	if err != nil {
		t.Fatalf("first transition failed: %v\n%s", err, first)
	}

	// The active directory is now gone (moved by the fake openspec binary).
	// Rerunning transition must not invoke `openspec` again and must recompute
	// the identical JSON from the persisted snapshot.
	second, err := f.runTransition(t)
	if err != nil {
		t.Fatalf("replayed transition failed: %v\n%s", err, second)
	}

	var firstDecoded, secondDecoded map[string]any
	if err := json.Unmarshal([]byte(first), &firstDecoded); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}
	if err := json.Unmarshal([]byte(second), &secondDecoded); err != nil {
		t.Fatalf("unmarshal second: %v", err)
	}
	if diff := cmp.Diff(firstDecoded, secondDecoded); diff != "" {
		t.Fatalf("replayed transition JSON differs (-first +second):\n%s", diff)
	}
}

func TestVerifyArchiveCommitAgainstRealGit(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")

	// Pre-existing staged edit and unstaged edit inside openspec/specs, plus an
	// unrelated staged file outside the allowed paths.
	mustWriteFile(t, filepath.Join(f.repo, "unrelated.txt"), "leave me alone\n")
	runGit(t, f.repo, "add", "unrelated.txt")
	mustWriteFile(t, filepath.Join(f.repo, "openspec", "specs", "spec-a.md"), "spec A pre-existing staged edit\n")
	runGit(t, f.repo, "add", "openspec/specs/spec-a.md")
	mustWriteFile(t, filepath.Join(f.repo, "openspec", "specs", "spec-b.md"), "spec B pre-existing unstaged edit\n")

	transitionOut, err := f.runTransition(t)
	if err != nil {
		t.Fatalf("archive-transition failed: %v\n%s", err, transitionOut)
	}
	archiveDir := archiveStateField(t, transitionOut, "archive_dir")

	t.Run("fails before any commit", func(t *testing.T) {
		out, err := f.runVerify(t, transitionOut)
		if err == nil {
			t.Fatalf("verify succeeded before any commit:\n%s", out)
		}
		if strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]) == "" {
			t.Fatalf("expected a non-empty first stderr line, got %q", out)
		}
	})

	canonicalSpec := filepath.Join("openspec", "specs", "spec-a-canonical.md")

	t.Run("passes after a compliant commit of exactly the owned delta", func(t *testing.T) {
		runGit(t, f.repo, "add", "--", f.changeDir, archiveDir, canonicalSpec)
		out, err := runGitCombined(t, f.repo, "commit", "-m", "chore: archive change", "--", f.changeDir, archiveDir, canonicalSpec)
		if err == nil {
			t.Fatalf("git accepted a non-compliant commit subject:\n%s", out)
		}
		runGit(t, f.repo, "commit", "-m", "TICKET-123: archive change", "--", f.changeDir, archiveDir, canonicalSpec)

		out, err = f.runVerify(t, transitionOut)
		if err != nil {
			t.Fatalf("verify failed after compliant commit: %v\n%s", err, out)
		}

		status := string(runGitOutput(t, f.repo, "status", "--porcelain", "--",
			"unrelated.txt", "openspec/specs/spec-a.md", "openspec/specs/spec-b.md"))
		if !strings.Contains(status, "A  unrelated.txt") {
			t.Fatalf("unrelated staged file no longer staged:\n%s", status)
		}
		if !strings.Contains(status, "M  openspec/specs/spec-a.md") {
			t.Fatalf("pre-existing staged spec edit no longer staged:\n%s", status)
		}
		if !strings.Contains(status, " M openspec/specs/spec-b.md") {
			t.Fatalf("pre-existing unstaged spec edit no longer present:\n%s", status)
		}

		committedCanonical := string(runGitOutput(t, f.repo, "show", "HEAD:"+filepath.ToSlash(canonicalSpec)))
		if !strings.Contains(committedCanonical, "spec A merged for ticket-123-demo") {
			t.Fatalf("canonical spec content = %q, want the merged delta content committed", committedCanonical)
		}

		startHead := archiveStateField(t, transitionOut, "start_head")
		touchedOut := string(runGitOutput(t, f.repo, "diff", "--name-only", startHead, "HEAD"))
		for path := range strings.FieldsSeq(touchedOut) {
			if !strings.HasPrefix(path, f.changeDir) && !strings.HasPrefix(path, archiveDir) && path != filepath.ToSlash(canonicalSpec) {
				t.Fatalf("commit range touched unexpected path: %s", path)
			}
		}

		t.Run("rerunning verify after success is a no-op", func(t *testing.T) {
			headBefore := strings.TrimSpace(string(runGitOutput(t, f.repo, "rev-parse", "HEAD")))
			out, err := f.runVerify(t, transitionOut)
			if err != nil {
				t.Fatalf("replayed verify failed: %v\n%s", err, out)
			}
			headAfter := strings.TrimSpace(string(runGitOutput(t, f.repo, "rev-parse", "HEAD")))
			if headBefore != headAfter {
				t.Fatalf("replayed verify created a new commit: before %s, after %s", headBefore, headAfter)
			}
		})

		t.Run("history rewrite invalidates verify", func(t *testing.T) {
			runGit(t, f.repo, "checkout", "--orphan", "rewritten")
			runGit(t, f.repo, "rm", "-rf", "--cached", ".")
			mustWriteFile(t, filepath.Join(f.repo, "other.txt"), "unrelated\n")
			runGit(t, f.repo, "add", "other.txt")
			runGit(t, f.repo, "commit", "-m", "TICKET-123: unrelated history")

			out, err := f.runVerify(t, transitionOut)
			if err == nil {
				t.Fatalf("verify succeeded against rewritten history:\n%s", out)
			}
			if !strings.Contains(out, "not an ancestor") {
				t.Fatalf("output = %q, want ancestor explanation", out)
			}
		})
	})
}

func TestVerifyArchiveCommitRejectsCommitOutsideOwnedDelta(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")
	mustWriteFile(t, filepath.Join(f.repo, "unrelated.txt"), "leave me alone\n")
	runGit(t, f.repo, "add", "unrelated.txt")

	transitionOut, err := f.runTransition(t)
	if err != nil {
		t.Fatalf("archive-transition failed: %v\n%s", err, transitionOut)
	}
	archiveDir := archiveStateField(t, transitionOut, "archive_dir")
	canonicalSpec := filepath.Join("openspec", "specs", "spec-a-canonical.md")

	runGit(t, f.repo, "add", "--", f.changeDir, archiveDir, canonicalSpec, "unrelated.txt")
	runGit(t, f.repo, "commit", "-m", "TICKET-123: archive change with unrelated file")

	out, err := f.runVerify(t, transitionOut)
	if err == nil {
		t.Fatalf("verify succeeded with an unrelated committed file:\n%s", out)
	}
	if !strings.Contains(out, "unrelated.txt") {
		t.Fatalf("output = %q, want the unrelated path named", out)
	}
}

func TestVerifyArchiveCommitRejectsCommittingPreExistingStagedEdit(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")
	mustWriteFile(t, filepath.Join(f.repo, "openspec", "specs", "spec-a.md"), "spec A pre-existing staged edit\n")
	runGit(t, f.repo, "add", "openspec/specs/spec-a.md")

	transitionOut, err := f.runTransition(t)
	if err != nil {
		t.Fatalf("archive-transition failed: %v\n%s", err, transitionOut)
	}
	archiveDir := archiveStateField(t, transitionOut, "archive_dir")
	canonicalSpec := filepath.Join("openspec", "specs", "spec-a-canonical.md")

	runGit(t, f.repo, "add", "--", f.changeDir, archiveDir, canonicalSpec, "openspec/specs/spec-a.md")
	runGit(t, f.repo, "commit", "-m", "TICKET-123: archive change including pre-existing edit")

	out, err := f.runVerify(t, transitionOut)
	if err == nil {
		t.Fatalf("verify succeeded after committing a pre-existing staged edit:\n%s", out)
	}
	if !strings.Contains(out, "spec-a.md") {
		t.Fatalf("output = %q, want the pre-existing edit path named", out)
	}
}

func TestArchiveTransitionOwnsDirtyActiveChangeDirectory(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")

	// A staged edit inside the active change directory itself must be fully
	// owned by the archive move, not preserved as pre-existing baseline state
	// the way a pre-existing edit under openspec/specs is.
	mustWriteFile(t, filepath.Join(f.repo, f.changeDir, "proposal.md"), "proposal, still in progress\n")
	runGit(t, f.repo, "add", filepath.Join(f.changeDir, "proposal.md"))

	transitionOut, err := f.runTransition(t)
	if err != nil {
		t.Fatalf("archive-transition failed: %v\n%s", err, transitionOut)
	}
	archiveDir := archiveStateField(t, transitionOut, "archive_dir")
	canonicalSpec := filepath.Join("openspec", "specs", "spec-a-canonical.md")

	runGit(t, f.repo, "add", "--", archiveDir, canonicalSpec)
	runGit(t, f.repo, "commit", "-m", "TICKET-123: archive change", "--", f.changeDir, archiveDir, canonicalSpec)

	out, err := f.runVerify(t, transitionOut)
	if err != nil {
		t.Fatalf("verify failed after archiving a dirty active change directory: %v\n%s", err, out)
	}

	committedProposal := string(runGitOutput(t, f.repo, "show", "HEAD:"+filepath.ToSlash(filepath.Join(archiveDir, "proposal.md"))))
	if !strings.Contains(committedProposal, "still in progress") {
		t.Fatalf("archived proposal content = %q, want the dirty edit committed", committedProposal)
	}
}

func runGitCombined(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The runner passes captured variables to script_inputs as strings, so the
// verifier receives archive_state as the JSON text the transition printed,
// not as a nested object. It must accept both shapes.
func TestVerifyArchiveCommitAcceptsArchiveStateAsString(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")
	transitionOut, err := f.runTransition(t)
	if err != nil {
		t.Fatalf("archive-transition failed: %v\n%s", err, transitionOut)
	}
	archiveDir := archiveStateField(t, transitionOut, "archive_dir")
	canonicalSpec := filepath.Join("openspec", "specs", "spec-a-canonical.md")
	runGit(t, f.repo, "add", "--", f.changeDir, archiveDir, canonicalSpec)
	runGit(t, f.repo, "commit", "-m", "TICKET-123: archive change", "--", f.changeDir, archiveDir, canonicalSpec)

	stdin, err := json.Marshal(map[string]string{
		"archive_state": strings.TrimSpace(transitionOut),
		"change_name":   f.changeName,
		"session_dir":   f.sessionDir,
	})
	if err != nil {
		t.Fatalf("marshal verify input: %v", err)
	}
	out, err := f.run(t, "verify-archive-commit.sh", stdin)
	if err != nil {
		t.Fatalf("verify rejected archive_state passed as a string: %v\n%s", err, out)
	}
}

// chattyOpenSpec replaces the fixture's silent fake with one that prints
// progress on stdout, as the real CLI does for validate and archive.
func (f *archiveTestFixture) chattyOpenSpec(t *testing.T) {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	script := `#!/bin/sh
set -eu
if [ "$1" = "validate" ]; then
  echo "Change '$3' is valid"
  echo ""
  echo "Proposal warnings in proposal.md (non-blocking):"
  exit 0
fi
if [ "$1" = "archive" ]; then
  name="$2"
  echo "Specs to update:"
  echo "  spec-a: update"
  mkdir -p openspec/changes/archive
  mv "openspec/changes/$name" "openspec/changes/archive/2026-09-03-$name"
  printf 'spec A merged for %s\n' "$name" > openspec/specs/spec-a-canonical.md
  echo "Change '$name' archived as '2026-09-03-$name'."
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "openspec"), []byte(script), 0o700); err != nil {
		t.Fatalf("write chatty openspec: %v", err)
	}
	f.env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
}

func (f *archiveTestFixture) runStdout(t *testing.T, script string, stdin []byte) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command("sh", filepath.Join(f.scriptsDir, script))
	cmd.Dir = f.repo
	cmd.Env = f.env
	cmd.Stdin = strings.NewReader(string(stdin))
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// TestArchiveTransitionStdoutIsOnlyTheStateJSON proves that the openspec
// CLI's own progress output never reaches the captured archive_state: the
// verify step parses that capture as JSON.
func TestArchiveTransitionStdoutIsOnlyTheStateJSON(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")
	f.chattyOpenSpec(t)
	stdin, err := json.Marshal(map[string]string{"change_name": f.changeName, "session_dir": f.sessionDir})
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := f.runStdout(t, "archive-transition.sh", stdin)
	if err != nil {
		t.Fatalf("archive-transition failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if got := archiveStateField(t, stdout, "archive_dir"); got == "" {
		t.Fatal("archive_dir missing from stdout JSON")
	}
	if !strings.Contains(stderr, "archived as") {
		t.Fatalf("openspec progress should go to stderr, got:\n%s", stderr)
	}
}

// TestVerifyArchiveCommitAcceptsStateWithLeadingOutput covers a capture
// recorded before the transition sent openspec output to stderr: the JSON
// object at the end of the text is still the state.
func TestVerifyArchiveCommitAcceptsStateWithLeadingOutput(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")
	transitionOut, err := f.runTransition(t)
	if err != nil {
		t.Fatalf("archive-transition failed: %v\n%s", err, transitionOut)
	}
	archiveDir := archiveStateField(t, transitionOut, "archive_dir")
	canonicalSpec := filepath.Join("openspec", "specs", "spec-a-canonical.md")
	runGit(t, f.repo, "add", "--", f.changeDir, archiveDir, canonicalSpec)
	runGit(t, f.repo, "commit", "-m", "TICKET-123: archive change", "--", f.changeDir, archiveDir, canonicalSpec)

	// The real CLI prints multi-byte glyphs, which break any byte-offset slicing.
	polluted := "Change 'ticket-123-demo' is valid\n\nProposal warnings:\n  ⚠ Why section too long\nTask status: ✓ Complete\nChange 'ticket-123-demo' archived as '2026-09-03-ticket-123-demo'.\n" + transitionOut
	stdin, err := json.Marshal(map[string]string{
		"archive_state": polluted,
		"change_name":   f.changeName,
		"session_dir":   f.sessionDir,
	})
	if err != nil {
		t.Fatalf("marshal verify input: %v", err)
	}
	out, err := f.run(t, "verify-archive-commit.sh", stdin)
	if err != nil {
		t.Fatalf("verify rejected archive_state with leading output: %v\n%s", err, out)
	}
}

// TestVerifyArchiveCommitToleratesSeparateUnrelatedCommit covers a fix
// committed between a failed verification and its resume: a commit that
// touches no archive path is not part of the archive delta and must not fail
// verification, while the archive commit itself stays pure.
func TestVerifyArchiveCommitToleratesSeparateUnrelatedCommit(t *testing.T) {
	f := newArchiveTestFixture(t, "ticket-123-demo")
	transitionOut, err := f.runTransition(t)
	if err != nil {
		t.Fatalf("archive-transition failed: %v\n%s", err, transitionOut)
	}
	archiveDir := archiveStateField(t, transitionOut, "archive_dir")
	canonicalSpec := filepath.Join("openspec", "specs", "spec-a-canonical.md")
	runGit(t, f.repo, "add", "--", f.changeDir, archiveDir, canonicalSpec)
	runGit(t, f.repo, "commit", "-m", "TICKET-123: archive change", "--", f.changeDir, archiveDir, canonicalSpec)

	mustWriteFile(t, filepath.Join(f.repo, "tooling.sh"), "echo fixed\n")
	runGit(t, f.repo, "add", "tooling.sh")
	runGit(t, f.repo, "commit", "-m", "TICKET-123: fix tooling", "--", "tooling.sh")

	out, err := f.runVerify(t, transitionOut)
	if err != nil {
		t.Fatalf("verify rejected a separate unrelated commit: %v\n%s", err, out)
	}
}
