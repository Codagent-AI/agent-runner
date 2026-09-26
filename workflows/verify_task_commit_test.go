package builtinworkflows

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestVerifyTaskCommitScript(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "Agent Runner Test")
	runGit(t, repo, "config", "user.email", "agent-runner@example.com")
	mustWriteFile(t, filepath.Join(repo, "file.txt"), "initial\n")
	runGit(t, repo, "add", "file.txt")
	runGit(t, repo, "commit", "-m", "initial")
	startingHead := strings.TrimSpace(string(runGitOutput(t, repo, "rev-parse", "HEAD")))

	script, err := ReadAsset("core/verify-task-commit.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/verify-task-commit.sh): %v", err)
	}
	scriptPath := filepath.Join(t.TempDir(), "verify-task-commit.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	run := func(t *testing.T, head string) (string, error) {
		t.Helper()
		cmd := exec.Command("sh", scriptPath)
		cmd.Dir = repo
		cmd.Stdin = strings.NewReader(`{"starting_head":` + strconv.Quote(head) + `}`)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	t.Run("rejects unchanged HEAD", func(t *testing.T) {
		out, err := run(t, startingHead)
		if err == nil {
			t.Fatalf("script succeeded unexpectedly:\n%s", out)
		}
		if !strings.Contains(out, "did not produce a commit") {
			t.Fatalf("output = %q, want missing-commit explanation", out)
		}
	})

	t.Run("rejects an empty commit", func(t *testing.T) {
		runGit(t, repo, "commit", "--allow-empty", "-m", "empty")

		out, err := run(t, startingHead)
		if err == nil {
			t.Fatalf("script succeeded unexpectedly:\n%s", out)
		}
		if !strings.Contains(out, "no tracked implementation changes") {
			t.Fatalf("output = %q, want empty-change explanation", out)
		}

		runGit(t, repo, "reset", "--hard", startingHead)
	})

	t.Run("accepts a descendant commit", func(t *testing.T) {
		mustWriteFile(t, filepath.Join(repo, "file.txt"), "implemented\n")
		runGit(t, repo, "add", "file.txt")
		runGit(t, repo, "commit", "-m", "implement task")

		out, err := run(t, startingHead)
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}
		if !strings.Contains(out, "produced 1 commit") {
			t.Fatalf("output = %q, want commit count", out)
		}
	})

	t.Run("rejects rewritten history", func(t *testing.T) {
		runGit(t, repo, "checkout", "--orphan", "unrelated")
		runGit(t, repo, "rm", "-f", "file.txt")
		mustWriteFile(t, filepath.Join(repo, "other.txt"), "unrelated\n")
		runGit(t, repo, "add", "other.txt")
		runGit(t, repo, "commit", "-m", "unrelated")

		out, err := run(t, startingHead)
		if err == nil {
			t.Fatalf("script succeeded unexpectedly:\n%s", out)
		}
		if !strings.Contains(out, "not a descendant") {
			t.Fatalf("output = %q, want history explanation", out)
		}
	})
}

// deliveryFixture is a run repository plus an external clone of a bare remote,
// used to exercise verify-task-commit's external delivery path.
type deliveryFixture struct {
	script       string
	run          string
	remote       string
	ext          string
	extRoot      string
	recordPath   string
	startingHead string
	startedAt    int64
	// env overrides the script's environment, for example to hide jq.
	env []string
}

func newDeliveryFixture(t *testing.T) *deliveryFixture {
	t.Helper()
	f := &deliveryFixture{startedAt: time.Now().Unix()}

	f.run = t.TempDir()
	initTestRepo(t, f.run)
	mustWriteFile(t, filepath.Join(f.run, "file.txt"), "initial\n")
	runGit(t, f.run, "add", "file.txt")
	runGit(t, f.run, "commit", "-q", "-m", "initial")
	f.startingHead = gitString(t, f.run, "rev-parse", "HEAD")

	f.remote = filepath.Join(t.TempDir(), "remote.git")
	runGit(t, filepath.Dir(f.remote), "init", "-q", "--bare", "-b", "main", f.remote)
	seed := t.TempDir()
	initTestRepo(t, seed)
	mustWriteFile(t, filepath.Join(seed, "README.md"), "external\n")
	runGit(t, seed, "add", "README.md")
	gitWithEnv(t, seed, []string{"GIT_COMMITTER_DATE=" + strconv.FormatInt(f.startedAt-3600, 10) + " +0000"}, "commit", "-q", "-m", "seed")
	runGit(t, seed, "remote", "add", "origin", f.remote)
	runGit(t, seed, "push", "-q", "origin", "main")

	f.ext = filepath.Join(t.TempDir(), "ext")
	runGit(t, filepath.Dir(f.ext), "clone", "-q", f.remote, f.ext)
	runGit(t, f.ext, "config", "user.name", "Agent Runner Test")
	runGit(t, f.ext, "config", "user.email", "agent-runner@example.com")
	runGit(t, f.ext, "config", "commit.gpgsign", "false")
	f.extRoot = gitString(t, f.ext, "rev-parse", "--show-toplevel")
	if got := gitString(t, f.ext, "symbolic-ref", "refs/remotes/origin/HEAD"); got != "refs/remotes/origin/main" {
		t.Fatalf("origin/HEAD = %q, want refs/remotes/origin/main", got)
	}
	runGit(t, f.ext, "checkout", "-q", "-b", "feature")

	sessionDir := t.TempDir()
	f.recordPath = filepath.Join(sessionDir, "output", "task-delivery", "01-task-"+strconv.FormatInt(f.startedAt, 10)+".json")
	if err := os.MkdirAll(filepath.Dir(f.recordPath), 0o755); err != nil {
		t.Fatal(err)
	}

	script, err := ReadAsset("core/verify-task-commit.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/verify-task-commit.sh): %v", err)
	}
	f.script = filepath.Join(t.TempDir(), "verify-task-commit.sh")
	if err := os.WriteFile(f.script, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return f
}

func initTestRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.name", "Agent Runner Test")
	runGit(t, dir, "config", "user.email", "agent-runner@example.com")
	runGit(t, dir, "config", "commit.gpgsign", "false")
}

func gitString(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func gitWithEnv(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// commitExt commits a change to name in the external clone, committed at the
// given offset from the task start, and returns the full commit ID.
func (f *deliveryFixture) commitExt(t *testing.T, name, content string, offset int64) string {
	t.Helper()
	mustWriteFile(t, filepath.Join(f.ext, name), content)
	runGit(t, f.ext, "add", name)
	date := strconv.FormatInt(f.startedAt+offset, 10) + " +0000"
	gitWithEnv(t, f.ext, []string{"GIT_COMMITTER_DATE=" + date, "GIT_AUTHOR_DATE=" + date}, "commit", "-q", "-m", "deliver "+name)
	return gitString(t, f.ext, "rev-parse", "HEAD")
}

func (f *deliveryFixture) push(t *testing.T, branch string) {
	t.Helper()
	runGit(t, f.ext, "push", "-q", "-u", "origin", branch)
}

func (f *deliveryFixture) writeRecord(t *testing.T, record map[string]any) {
	t.Helper()
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, f.recordPath, string(body))
}

func (f *deliveryFixture) writeRawRecord(t *testing.T, body string) {
	t.Helper()
	mustWriteFile(t, f.recordPath, body)
}

func (f *deliveryFixture) input() string {
	return fmt.Sprintf(`{"starting_head":%q,"started_at":"%d","record_path":%q}`, f.startingHead, f.startedAt, f.recordPath)
}

func (f *deliveryFixture) verify(t *testing.T, input string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command("sh", f.script)
	cmd.Dir = f.run
	cmd.Stdin = strings.NewReader(input)
	if f.env != nil {
		cmd.Env = f.env
	}
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run script: %v", err)
	}
	return out.String(), errOut.String(), exitCode
}

// acceptedPath is where the gate marks a record it accepted.
func (f *deliveryFixture) acceptedPath() string {
	return strings.TrimSuffix(f.recordPath, ".json") + ".accepted"
}

func (f *deliveryFixture) wantReject(t *testing.T, want string) string {
	t.Helper()
	stdout, stderr, code := f.verify(t, f.input())
	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(f.acceptedPath()); !os.IsNotExist(err) {
		t.Fatalf("rejected record was marked accepted (stat err %v)", err)
	}
	if !strings.Contains(stderr, want) {
		t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
	}
	return stderr
}

func (f *deliveryFixture) wantAccept(t *testing.T) string {
	t.Helper()
	stdout, stderr, code := f.verify(t, f.input())
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "task delivered outside this repository") {
		t.Fatalf("stdout = %q, want external delivery statement", stdout)
	}
	return stdout
}

// withoutJQ returns an environment whose PATH hides jq, so the script takes its
// python3 fallback.
func withoutJQ(t *testing.T) []string {
	t.Helper()
	bin := t.TempDir()
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == "jq" || seen[name] {
				continue
			}
			seen[name] = true
			if err := os.Symlink(filepath.Join(dir, name), filepath.Join(bin, name)); err != nil {
				t.Fatalf("symlink %s: %v", name, err)
			}
		}
	}
	env := []string{"PATH=" + bin}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	return env
}

func TestVerifyTaskCommitExternalDelivery(t *testing.T) {
	for _, parser := range []string{"jq", "python3"} {
		t.Run(parser, func(t *testing.T) {
			if parser == "jq" {
				if _, err := exec.LookPath("jq"); err != nil {
					t.Skip("jq not installed")
				}
			}
			fixture := func(t *testing.T) *deliveryFixture {
				f := newDeliveryFixture(t)
				if parser == "python3" {
					f.env = withoutJQ(t)
				}
				return f
			}

			t.Run("accepts pushed commits and reports them", func(t *testing.T) {
				f := fixture(t)
				first := f.commitExt(t, "a.txt", "a\n", 10)
				second := f.commitExt(t, "b.txt", "b\n", 20)
				f.push(t, "feature")
				f.writeRecord(t, map[string]any{
					"repository":   f.ext,
					"commits":      []string{first, second[:12]},
					"branch":       "feature",
					"pull_request": "https://example.com/pr/1",
					"extra":        "ignored",
				})

				stdout := f.wantAccept(t)
				want := strings.Join([]string{
					"task delivered outside this repository",
					"repository: " + f.extRoot,
					"commit: " + first + " (contained in refs/remotes/origin/feature)",
					"commit: " + second + " (contained in refs/remotes/origin/feature)",
					"branch (reported, unverified): feature",
					"pull request (reported, unverified): https://example.com/pr/1",
					"note: pushed state was judged from this clone's local remote-tracking refs; the remote was not contacted",
					"note: this run's validator and task-compliance review did not cover the external work",
					"record: " + f.recordPath,
				}, "\n") + "\n"
				if diff := cmp.Diff(want, stdout); diff != "" {
					t.Fatalf("stdout mismatch (-want +got):\n%s", diff)
				}
				accepted, err := os.ReadFile(f.acceptedPath())
				if err != nil {
					t.Fatalf("accepted marker not written: %v", err)
				}
				if diff := cmp.Diff(want, string(accepted)); diff != "" {
					t.Fatalf("accepted marker mismatch (-want +got):\n%s", diff)
				}
			})

			t.Run("omits unreported optional fields", func(t *testing.T) {
				f := fixture(t)
				c := f.commitExt(t, "a.txt", "a\n", 10)
				f.push(t, "feature")
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})

				stdout := f.wantAccept(t)
				if strings.Contains(stdout, "reported, unverified") {
					t.Fatalf("stdout = %q, want no unverified fields", stdout)
				}
			})

			t.Run("rejects a fabricated commit", func(t *testing.T) {
				f := fixture(t)
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{"deadbeefdeadbeef"}})
				f.wantReject(t, "commit deadbeefdeadbeef: not found")
			})

			t.Run("rejects an unpushed commit", func(t *testing.T) {
				f := fixture(t)
				c := f.commitExt(t, "a.txt", "a\n", 10)
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
				f.wantReject(t, "commit "+c+": not reachable from any remote-tracking ref")
			})

			t.Run("rejects an empty commit", func(t *testing.T) {
				f := fixture(t)
				runGit(t, f.ext, "commit", "-q", "--allow-empty", "-m", "empty")
				c := gitString(t, f.ext, "rev-parse", "HEAD")
				f.push(t, "feature")
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
				f.wantReject(t, "commit "+c+": contains no tracked changes")
			})

			t.Run("accepts a root commit with files", func(t *testing.T) {
				f := fixture(t)
				runGit(t, f.ext, "checkout", "-q", "--orphan", "fresh")
				runGit(t, f.ext, "rm", "-q", "-rf", ".")
				c := f.commitExt(t, "new.txt", "new\n", 10)
				f.push(t, "fresh")
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
				f.wantAccept(t)
			})

			t.Run("rejects a root commit without files", func(t *testing.T) {
				f := fixture(t)
				runGit(t, f.ext, "checkout", "-q", "--orphan", "fresh")
				runGit(t, f.ext, "rm", "-q", "-rf", ".")
				runGit(t, f.ext, "commit", "-q", "--allow-empty", "-m", "empty root")
				c := gitString(t, f.ext, "rev-parse", "HEAD")
				f.push(t, "fresh")
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
				f.wantReject(t, "commit "+c+": contains no tracked changes")
			})

			t.Run("rejects a commit that predates the task", func(t *testing.T) {
				f := fixture(t)
				c := f.commitExt(t, "a.txt", "a\n", -301)
				f.push(t, "feature")
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
				f.wantReject(t, "commit "+c+": predates the task")
			})

			t.Run("accepts a commit within the skew tolerance", func(t *testing.T) {
				f := fixture(t)
				c := f.commitExt(t, "a.txt", "a\n", -300)
				f.push(t, "feature")
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
				f.wantAccept(t)
			})

			t.Run("rejects a commit already on the default branch", func(t *testing.T) {
				f := fixture(t)
				runGit(t, f.ext, "checkout", "-q", "main")
				c := f.commitExt(t, "a.txt", "a\n", 10)
				f.push(t, "main")
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
				f.wantReject(t, "commit "+c+": already on the default branch")
			})

			t.Run("skips the merged check without a recorded default branch", func(t *testing.T) {
				f := fixture(t)
				runGit(t, f.ext, "checkout", "-q", "main")
				c := f.commitExt(t, "a.txt", "a\n", 10)
				f.push(t, "main")
				runGit(t, f.ext, "remote", "set-head", "origin", "-d")
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
				stdout := f.wantAccept(t)
				if !strings.Contains(stdout, "contained in refs/remotes/origin/main") {
					t.Fatalf("stdout = %q, want containing ref", stdout)
				}
			})

			t.Run("one bad commit fails the record", func(t *testing.T) {
				f := fixture(t)
				good := f.commitExt(t, "a.txt", "a\n", 10)
				f.push(t, "feature")
				bad := f.commitExt(t, "b.txt", "b\n", 20)
				f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{good, bad}})
				stderr := f.wantReject(t, "commit "+bad+": not reachable from any remote-tracking ref")
				if strings.Contains(stderr, "commit "+good+":") {
					t.Fatalf("stderr = %q, want only the failing commit named", stderr)
				}
			})

			t.Run("rejects a worktree of the run repository", func(t *testing.T) {
				f := fixture(t)
				wt := filepath.Join(t.TempDir(), "wt")
				runGit(t, f.run, "worktree", "add", "-q", "-b", "side", wt)
				mustWriteFile(t, filepath.Join(wt, "side.txt"), "side\n")
				runGit(t, wt, "add", "side.txt")
				runGit(t, wt, "commit", "-q", "-m", "side")
				c := gitString(t, wt, "rev-parse", "HEAD")
				f.writeRecord(t, map[string]any{"repository": wt, "commits": []string{c}})
				f.wantReject(t, "named repository is the run repository")
			})

			t.Run("rejects a subdirectory of the run repository", func(t *testing.T) {
				f := fixture(t)
				sub := filepath.Join(f.run, "sub")
				if err := os.MkdirAll(sub, 0o755); err != nil {
					t.Fatal(err)
				}
				f.writeRecord(t, map[string]any{"repository": sub, "commits": []string{f.startingHead}})
				f.wantReject(t, "named repository is the run repository")
			})

			t.Run("rejects a bare repository", func(t *testing.T) {
				f := fixture(t)
				f.writeRecord(t, map[string]any{"repository": f.remote, "commits": []string{f.startingHead}})
				f.wantReject(t, "not a usable repository worktree")
			})

			t.Run("rejects a non-repository directory", func(t *testing.T) {
				f := fixture(t)
				f.writeRecord(t, map[string]any{"repository": t.TempDir(), "commits": []string{f.startingHead}})
				f.wantReject(t, "not a usable repository worktree")
			})

			t.Run("rejects a missing directory", func(t *testing.T) {
				f := fixture(t)
				f.writeRecord(t, map[string]any{"repository": filepath.Join(t.TempDir(), "missing"), "commits": []string{f.startingHead}})
				f.wantReject(t, "not a usable repository worktree")
			})

			t.Run("rejects malformed records", func(t *testing.T) {
				f := fixture(t)
				hexID := strings.Repeat("a", 40)
				tooMany := make([]string, 101)
				for i := range tooMany {
					tooMany[i] = hexID
				}
				marshal := func(v any) string {
					body, err := json.Marshal(v)
					if err != nil {
						t.Fatal(err)
					}
					return string(body)
				}
				cases := []struct {
					name string
					body string
				}{
					{"invalid JSON", "{"},
					{"not an object", `["` + hexID + `"]`},
					{"multiple values", marshal(map[string]any{"repository": f.ext, "commits": []string{hexID}}) + "{}"},
					{"missing repository", marshal(map[string]any{"commits": []string{hexID}})},
					{"non-string repository", marshal(map[string]any{"repository": 7, "commits": []string{hexID}})},
					{"relative repository", marshal(map[string]any{"repository": "ext", "commits": []string{hexID}})},
					{"missing commits", marshal(map[string]any{"repository": f.ext})},
					{"non-array commits", marshal(map[string]any{"repository": f.ext, "commits": hexID})},
					{"empty commits", marshal(map[string]any{"repository": f.ext, "commits": []string{}})},
					{"too many commits", marshal(map[string]any{"repository": f.ext, "commits": tooMany})},
					{"non-string commit", marshal(map[string]any{"repository": f.ext, "commits": []any{7}})},
					{"uppercase commit", marshal(map[string]any{"repository": f.ext, "commits": []string{strings.ToUpper(hexID)}})},
					{"short commit", marshal(map[string]any{"repository": f.ext, "commits": []string{"abcdef"}})},
					{"long commit", marshal(map[string]any{"repository": f.ext, "commits": []string{strings.Repeat("a", 65)}})},
					{"non-string branch", marshal(map[string]any{"repository": f.ext, "commits": []string{hexID}, "branch": 1})},
					{"control character in branch", marshal(map[string]any{"repository": f.ext, "commits": []string{hexID}, "branch": "a\nb"})},
					{"control character in pull request", marshal(map[string]any{"repository": f.ext, "commits": []string{hexID}, "pull_request": "a\tb"})},
					{"control character in repository", marshal(map[string]any{"repository": f.ext + "\n/x", "commits": []string{hexID}})},
					{"deeply nested record", strings.Repeat("[", 30000) + strings.Repeat("]", 30000)},
					{"oversized record", marshal(map[string]any{"repository": f.ext, "commits": []string{hexID}, "pad": strings.Repeat("x", 64*1024)})},
				}
				for _, tc := range cases {
					t.Run(tc.name, func(t *testing.T) {
						f.writeRawRecord(t, tc.body)
						f.wantReject(t, "malformed external delivery record")
					})
				}
			})
		})
	}
}

func TestVerifyTaskCommitRecordLocation(t *testing.T) {
	t.Run("without a record location HEAD unchanged fails and reads no record", func(t *testing.T) {
		f := newDeliveryFixture(t)
		f.writeRawRecord(t, "{")
		stdout, stderr, code := f.verify(t, fmt.Sprintf(`{"starting_head":%q}`, f.startingHead))
		if code != 1 {
			t.Fatalf("exit = %d, want 1\n%s%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "did not produce a commit") || strings.Contains(stderr, "external delivery record") {
			t.Fatalf("stderr = %q, want only the missing-commit message", stderr)
		}
	})

	t.Run("a missing record prints the expected path", func(t *testing.T) {
		f := newDeliveryFixture(t)
		stderr := f.wantReject(t, "did not produce a commit")
		if !strings.Contains(stderr, "no external delivery record at "+f.recordPath) {
			t.Fatalf("stderr = %q, want record path", stderr)
		}
	})

	t.Run("a local commit is accepted without reading the record", func(t *testing.T) {
		f := newDeliveryFixture(t)
		f.writeRawRecord(t, "{")
		mustWriteFile(t, filepath.Join(f.run, "file.txt"), "implemented\n")
		runGit(t, f.run, "commit", "-q", "-am", "implement")
		stdout, stderr, code := f.verify(t, f.input())
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s%s", code, stdout, stderr)
		}
		if stdout != "implementation task produced 1 commit(s)\n" {
			t.Fatalf("stdout = %q, want local delivery output", stdout)
		}
	})

	validRecord := func(t *testing.T, f *deliveryFixture) {
		t.Helper()
		c := f.commitExt(t, "a.txt", "a\n", 10)
		f.push(t, "feature")
		f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
	}

	t.Run("a non-descendant local commit is not rescued by a valid record", func(t *testing.T) {
		f := newDeliveryFixture(t)
		validRecord(t, f)
		runGit(t, f.run, "checkout", "-q", "--orphan", "unrelated")
		mustWriteFile(t, filepath.Join(f.run, "other.txt"), "unrelated\n")
		runGit(t, f.run, "add", "other.txt")
		runGit(t, f.run, "commit", "-q", "-m", "unrelated")
		f.wantReject(t, "not a descendant")
	})

	t.Run("an empty local commit is not rescued by a valid record", func(t *testing.T) {
		f := newDeliveryFixture(t)
		validRecord(t, f)
		runGit(t, f.run, "commit", "-q", "--allow-empty", "-m", "empty")
		f.wantReject(t, "no tracked implementation changes")
	})

	t.Run("a captured starting head keeps its trailing newline", func(t *testing.T) {
		f := newDeliveryFixture(t)
		c := f.commitExt(t, "a.txt", "a\n", 10)
		f.push(t, "feature")
		f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
		for _, env := range [][]string{nil, withoutJQ(t)} {
			f.env = env
			stdout, stderr, code := f.verify(t, fmt.Sprintf(`{"starting_head":%q,"started_at":"%d","record_path":%q}`, f.startingHead+"\n", f.startedAt, f.recordPath))
			if code != 0 || !strings.Contains(stdout, "task delivered outside this repository") {
				t.Fatalf("exit = %d, want external delivery\n%s%s", code, stdout, stderr)
			}
		}
	})

	t.Run("invalid inputs exit 2", func(t *testing.T) {
		f := newDeliveryFixture(t)
		for name, input := range map[string]string{
			"started_at without record_path": fmt.Sprintf(`{"starting_head":%q,"started_at":"1"}`, f.startingHead),
			"record_path without started_at": fmt.Sprintf(`{"starting_head":%q,"record_path":%q}`, f.startingHead, f.recordPath),
			"non-numeric started_at":         fmt.Sprintf(`{"starting_head":%q,"started_at":"soon","record_path":%q}`, f.startingHead, f.recordPath),
			"relative record_path":           fmt.Sprintf(`{"starting_head":%q,"started_at":"1","record_path":"record.json"}`, f.startingHead),
			"control character in head":      fmt.Sprintf(`{"starting_head":%q,"started_at":"1","record_path":%q}`, f.startingHead+"\nabc", f.recordPath),
		} {
			t.Run(name, func(t *testing.T) {
				stdout, stderr, code := f.verify(t, input)
				if code != 2 {
					t.Fatalf("exit = %d, want 2\n%s%s", code, stdout, stderr)
				}
			})
		}
	})
}

func TestVerifyTaskCommitDoesNotRunExternalRepositoryPrograms(t *testing.T) {
	for _, pass := range []bool{true, false} {
		t.Run(fmt.Sprintf("verification passes=%v", pass), func(t *testing.T) {
			f := newDeliveryFixture(t)
			c := f.commitExt(t, "a.txt", "a\n", 10)
			if pass {
				f.push(t, "feature")
			}
			mustWriteFile(t, filepath.Join(f.ext, "dirty.txt"), "untracked\n")
			mustWriteFile(t, filepath.Join(f.ext, "README.md"), "modified\n")

			marker := filepath.Join(t.TempDir(), "fsmonitor-ran")
			monitor := filepath.Join(t.TempDir(), "fsmonitor.sh")
			if err := os.WriteFile(monitor, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			runGit(t, f.ext, "config", "core.fsmonitor", monitor)

			snapshot := func() string {
				refs := gitString(t, f.ext, "-c", "core.fsmonitor=false", "for-each-ref", "--format=%(refname) %(objectname) %(symref)")
				status := gitString(t, f.ext, "-c", "core.fsmonitor=false", "--no-optional-locks", "status", "--porcelain")
				index, err := os.ReadFile(filepath.Join(f.ext, ".git", "index"))
				if err != nil {
					t.Fatal(err)
				}
				return refs + "\n--\n" + status + "\n--\n" + string(index)
			}
			before := snapshot()

			f.writeRecord(t, map[string]any{"repository": f.ext, "commits": []string{c}})
			_, stderr, code := f.verify(t, f.input())
			if pass != (code == 0) {
				t.Fatalf("exit = %d, want pass=%v\n%s", code, pass, stderr)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("fsmonitor marker exists after verification (stat err %v)", err)
			}
			if after := snapshot(); after != before {
				t.Fatal("external repository refs, index, or status changed during verification")
			}

			// The fixture is only meaningful if git would run the monitor.
			runGit(t, f.ext, "status", "--porcelain")
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("fsmonitor fixture never ran under git status: %v", err)
			}
		})
	}
}
