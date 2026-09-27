package scripts_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeDockerScript stands in for the Docker CLI. It logs each argv as one
// tab-separated line and simulates build, run, image inspect, image rm, and rm.
const fakeDockerScript = `#!/usr/bin/env bash
set -u
{ printf '%s\t' "$@"; printf '\n'; } >> "$FAKE_DOCKER_LOG"
case "${1:-}" in
  build)
    exit "${FAKE_DOCKER_BUILD_EXIT:-0}"
    ;;
  run)
    artifacts=""
    prev=""
    for arg in "$@"; do
      if [[ "$prev" == "-v" && "$arg" == *:/artifacts ]]; then
        artifacts="${arg%:/artifacts}"
      fi
      prev="$arg"
    done
    if [[ -n "$artifacts" ]]; then
      evidence="$artifacts/dev-audit-smoke.fake"
      mkdir -p "$evidence/locked"
      echo "evidence" > "$evidence/evidence.txt"
      echo "restricted" > "$evidence/locked/file.txt"
      chmod 0500 "$evidence/locked"
    fi
    echo "$$" > "$FAKE_DOCKER_STATE/run.pid"
    if [[ "${FAKE_DOCKER_RUN_BLOCK:-0}" == 1 ]]; then
      while :; do sleep 0.1; done
    fi
    exit "${FAKE_DOCKER_RUN_EXIT:-0}"
    ;;
  image)
    case "${2:-}" in
      inspect)
        if [[ "${FAKE_DOCKER_IMAGE_PRESENT:-1}" == 1 ]]; then
          echo "[]"
          exit 0
        fi
        echo "Error: No such image: ${3:-}" >&2
        exit 1
        ;;
      rm)
        if [[ "${FAKE_DOCKER_IMAGE_RM_FAIL:-0}" == 1 ]]; then
          echo "Error response from daemon: conflict: unable to remove repository reference" >&2
          exit 1
        fi
        echo "Untagged: ${3:-}"
        exit 0
        ;;
    esac
    ;;
  rm)
    if [[ -f "$FAKE_DOCKER_STATE/run.pid" ]]; then
      kill -TERM "$(cat "$FAKE_DOCKER_STATE/run.pid")" 2>/dev/null || true
    fi
    exit 0
    ;;
esac
echo "fake docker: unsupported command: $*" >&2
exit 99
`

// fakeRmScript fails only for paths under FAKE_RM_FAIL_PREFIX and delegates
// every other invocation to the real rm.
const fakeRmScript = `#!/usr/bin/env bash
for arg in "$@"; do
  case "$arg" in
    "$FAKE_RM_FAIL_PREFIX"*)
      echo "rm: cannot remove '$arg': Operation not permitted" >&2
      exit 1
      ;;
  esac
done
exec "$REAL_RM" "$@"
`

var (
	smokeTagPattern       = regexp.MustCompile(`^agent-runner-dev-audit-smoke:(\d{14}-\d+-\d+)$`)
	smokeContainerPattern = regexp.MustCompile(`^--name=(agent-runner-dev-audit-smoke-(\d{14}-\d+-\d+))$`)
)

type smokeFixture struct {
	t        *testing.T
	binDir   string
	stateDir string
	tmpDir   string
	logPath  string
	env      map[string]string
}

type smokeResult struct {
	exitCode int
	stderr   string
	stdout   string
	calls    [][]string
}

func newSmokeFixture(t *testing.T) *smokeFixture {
	t.Helper()
	root := t.TempDir()
	f := &smokeFixture{
		t:        t,
		binDir:   filepath.Join(root, "bin"),
		stateDir: filepath.Join(root, "state"),
		tmpDir:   filepath.Join(root, "tmp"),
		env:      map[string]string{},
	}
	f.logPath = filepath.Join(f.stateDir, "docker.log")
	for _, dir := range []string{f.binDir, f.stateDir, f.tmpDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable(t, filepath.Join(f.binDir, "docker"), fakeDockerScript)
	t.Cleanup(func() { makeTreeWritable(f.tmpDir) })
	return f
}

func (f *smokeFixture) installFailingRm(prefix string) {
	f.t.Helper()
	realRm, err := exec.LookPath("rm")
	if err != nil {
		f.t.Fatalf("locate rm: %v", err)
	}
	writeExecutable(f.t, filepath.Join(f.binDir, "rm"), fakeRmScript)
	f.env["REAL_RM"] = realRm
	f.env["FAKE_RM_FAIL_PREFIX"] = prefix
}

func (f *smokeFixture) command() *exec.Cmd {
	cmd := exec.Command("bash", "./docker-dev-audit-smoke.sh")
	var env []string
	for _, entry := range withoutHostLauncherEnv(os.Environ()) {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "IMAGE", "ARTIFACT_DIR", "TMPDIR", "PATH", "AGENT_RUNNER_SOURCE_COMMIT", "AGENT_RUNNER_SOURCE_DIRTY":
			continue
		}
		if strings.HasPrefix(name, "FAKE_") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"PATH="+f.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TMPDIR="+f.tmpDir,
		"FAKE_DOCKER_LOG="+f.logPath,
		"FAKE_DOCKER_STATE="+f.stateDir,
	)
	for name, value := range f.env {
		env = append(env, name+"="+value)
	}
	cmd.Env = env
	// A leaked fake docker process would otherwise hold the output pipes open
	// and block Wait forever.
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func (f *smokeFixture) run() smokeResult {
	f.t.Helper()
	cmd := f.command()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := waitForCommandStart(cmd, 60*time.Second)
	return f.result(err, stdout.String(), stderr.String())
}

func (f *smokeFixture) result(err error, stdout, stderr string) smokeResult {
	f.t.Helper()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			f.t.Fatalf("smoke wrapper did not run: %v\nstderr:\n%s", err, stderr)
		}
		code = exitErr.ExitCode()
	}
	return smokeResult{exitCode: code, stdout: stdout, stderr: stderr, calls: f.calls()}
}

func (f *smokeFixture) calls() [][]string {
	f.t.Helper()
	data, err := os.ReadFile(f.logPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		calls = append(calls, strings.Split(strings.TrimSuffix(line, "\t"), "\t"))
	}
	return calls
}

func (f *smokeFixture) ownedArtifactDirs() []string {
	f.t.Helper()
	matches, err := filepath.Glob(filepath.Join(f.tmpDir, "agent-runner-dev-audit-smoke.*"))
	if err != nil {
		f.t.Fatal(err)
	}
	return matches
}

func waitForCommandStart(cmd *exec.Cmd, timeout time.Duration) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	return waitForCommand(cmd, timeout)
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func makeTreeWritable(root string) {
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			_ = os.Chmod(path, 0o755)
		}
		return nil
	})
}

func callsOf(calls [][]string, prefix ...string) [][]string {
	var matched [][]string
	for _, call := range calls {
		if len(call) >= len(prefix) && strings.Join(call[:len(prefix)], "\x00") == strings.Join(prefix, "\x00") {
			matched = append(matched, call)
		}
	}
	return matched
}

func callIndex(calls [][]string, prefix ...string) int {
	for i, call := range calls {
		if len(call) >= len(prefix) && strings.Join(call[:len(prefix)], "\x00") == strings.Join(prefix, "\x00") {
			return i
		}
	}
	return -1
}

func buildTag(t *testing.T, calls [][]string) string {
	t.Helper()
	builds := callsOf(calls, "build")
	if len(builds) != 1 {
		t.Fatalf("expected one docker build, got %v", builds)
	}
	for i, arg := range builds[0] {
		if arg == "-t" && i+1 < len(builds[0]) {
			return builds[0][i+1]
		}
	}
	t.Fatalf("docker build has no -t: %v", builds[0])
	return ""
}

func runImageAndName(t *testing.T, calls [][]string, tag string) string {
	t.Helper()
	runs := callsOf(calls, "run")
	if len(runs) != 1 {
		t.Fatalf("expected one docker run, got %v", runs)
	}
	name := ""
	hasTag := false
	for _, arg := range runs[0] {
		if m := smokeContainerPattern.FindStringSubmatch(arg); m != nil {
			name = m[1]
		}
		if arg == tag {
			hasTag = true
		}
	}
	if !hasTag {
		t.Fatalf("docker run does not use image %q: %v", tag, runs[0])
	}
	if name == "" {
		t.Fatalf("docker run has no run-owned --name: %v", runs[0])
	}
	return name
}

// assertOwnedDockerMutations applies the shared assertions: mutating Docker
// commands target only the run's own tag and container, image rm is never
// forced, nothing is pruned, and agent-runner-dev:local appears only when the
// caller supplied it.
func assertOwnedDockerMutations(t *testing.T, calls [][]string, ownedImage, container, callerImage string) {
	t.Helper()
	for _, call := range calls {
		for _, arg := range call {
			if arg == "prune" {
				t.Fatalf("docker prune must never run: %v", call)
			}
			if strings.Contains(arg, "agent-runner-dev:local") && callerImage != "agent-runner-dev:local" {
				t.Fatalf("agent-runner-dev:local must not be referenced: %v", call)
			}
		}
		switch {
		case len(call) >= 2 && call[0] == "image" && call[1] == "rm":
			for _, arg := range call[2:] {
				if arg == "-f" || arg == "--force" {
					t.Fatalf("image rm must not be forced: %v", call)
				}
				if !strings.HasPrefix(arg, "-") && (ownedImage == "" || arg != ownedImage) {
					t.Fatalf("image rm targets %q, which the run does not own: %v", arg, call)
				}
			}
		case len(call) >= 1 && call[0] == "rmi":
			t.Fatalf("docker rmi must not be used: %v", call)
		case len(call) >= 1 && call[0] == "rm":
			for _, arg := range call[1:] {
				if !strings.HasPrefix(arg, "-") && arg != container {
					t.Fatalf("docker rm targets %q, which is not the run's container %q: %v", arg, container, call)
				}
			}
		}
	}
}

func assertRunOwnedNaming(t *testing.T, calls [][]string) (tag, container string) {
	t.Helper()
	tag = buildTag(t, calls)
	m := smokeTagPattern.FindStringSubmatch(tag)
	if m == nil {
		t.Fatalf("build tag %q is not a run-unique agent-runner-dev-audit-smoke tag", tag)
	}
	container = runImageAndName(t, calls, tag)
	if want := "agent-runner-dev-audit-smoke-" + m[1]; container != want {
		t.Fatalf("container name %q does not share run id with tag %q", container, tag)
	}
	return tag, container
}

func assertSingleUnforcedImageRm(t *testing.T, calls [][]string, tag string) {
	t.Helper()
	rms := callsOf(calls, "image", "rm")
	if len(rms) != 1 {
		t.Fatalf("expected exactly one image rm, got %v", rms)
	}
	if got := rms[0][2:]; len(got) != 1 || got[0] != tag {
		t.Fatalf("image rm arguments = %v, want [%s]", got, tag)
	}
}

// INT-001
func TestDevAuditSmokeCleanupSuccessReleasesOwnedImageAndArtifacts(t *testing.T) {
	f := newSmokeFixture(t)
	res := f.run()
	if res.exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr:\n%s", res.exitCode, res.stderr)
	}
	tag, container := assertRunOwnedNaming(t, res.calls)
	assertOwnedDockerMutations(t, res.calls, tag, container, "")
	assertSingleUnforcedImageRm(t, res.calls, tag)
	if dirs := f.ownedArtifactDirs(); len(dirs) != 0 {
		t.Fatalf("owned artifact directories remain after success: %v", dirs)
	}
	if !strings.Contains(res.stderr, "smoke: removed artifact directory") {
		t.Fatalf("stderr does not report artifact removal:\n%s", res.stderr)
	}
}

// INT-002
func TestDevAuditSmokeCleanupFailureRemovesImageAndKeepsEvidence(t *testing.T) {
	f := newSmokeFixture(t)
	f.env["FAKE_DOCKER_RUN_EXIT"] = "1"
	res := f.run()
	if res.exitCode != 1 {
		t.Fatalf("exit code = %d, want 1\nstderr:\n%s", res.exitCode, res.stderr)
	}
	tag, container := assertRunOwnedNaming(t, res.calls)
	assertOwnedDockerMutations(t, res.calls, tag, container, "")
	assertSingleUnforcedImageRm(t, res.calls, tag)
	dirs := f.ownedArtifactDirs()
	if len(dirs) != 1 {
		t.Fatalf("expected the owned artifact directory to remain, got %v", dirs)
	}
	if _, err := os.Stat(filepath.Join(dirs[0], "dev-audit-smoke.fake", "evidence.txt")); err != nil {
		t.Fatalf("evidence missing from retained directory: %v", err)
	}
	if want := "evidence retained in " + dirs[0]; !strings.Contains(res.stderr, want) {
		t.Fatalf("stderr missing %q:\n%s", want, res.stderr)
	}
}

// INT-003
func TestDevAuditSmokeCleanupKeepsCallerImageAndArtifactDir(t *testing.T) {
	for _, tc := range []struct {
		name     string
		runExit  string
		wantCode int
	}{
		{name: "success", runExit: "0", wantCode: 0},
		{name: "failure", runExit: "1", wantCode: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSmokeFixture(t)
			callerDir := filepath.Join(t.TempDir(), "caller-artifacts")
			if err := os.MkdirAll(callerDir, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { makeTreeWritable(callerDir) })
			sentinel := filepath.Join(callerDir, "sentinel.txt")
			if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			const callerImage = "example/caller:tag"
			f.env["IMAGE"] = callerImage
			f.env["ARTIFACT_DIR"] = callerDir
			f.env["FAKE_DOCKER_RUN_EXIT"] = tc.runExit
			res := f.run()
			if res.exitCode != tc.wantCode {
				t.Fatalf("exit code = %d, want %d\nstderr:\n%s", res.exitCode, tc.wantCode, res.stderr)
			}
			if tag := buildTag(t, res.calls); tag != callerImage {
				t.Fatalf("build tag = %q, want %q", tag, callerImage)
			}
			container := runImageAndName(t, res.calls, callerImage)
			assertOwnedDockerMutations(t, res.calls, "", container, callerImage)
			if rms := callsOf(res.calls, "image", "rm"); len(rms) != 0 {
				t.Fatalf("caller-owned image must not be removed: %v", rms)
			}
			if _, err := os.Stat(sentinel); err != nil {
				t.Fatalf("caller sentinel removed: %v", err)
			}
			if _, err := os.Stat(filepath.Join(callerDir, "dev-audit-smoke.fake", "evidence.txt")); err != nil {
				t.Fatalf("smoke evidence removed from caller directory: %v", err)
			}
			if dirs := f.ownedArtifactDirs(); len(dirs) != 0 {
				t.Fatalf("smoke created its own artifact directory despite ARTIFACT_DIR: %v", dirs)
			}
		})
	}
}

// INT-004
func TestDevAuditSmokeCleanupSIGTERMStopsOwnContainerAndCleansUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal delivery is POSIX-only")
	}
	f := newSmokeFixture(t)
	f.env["FAKE_DOCKER_RUN_BLOCK"] = "1"
	cmd := f.command()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(f.stateDir, "run.pid")
	waitForPath(t, pidFile, 60*time.Second)
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	runPID, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitErr := waitForCommand(cmd, 45*time.Second)
	res := f.result(waitErr, stdout.String(), stderr.String())
	if res.exitCode != 143 {
		t.Fatalf("exit code = %d, want 143\nstderr:\n%s", res.exitCode, res.stderr)
	}
	tag, container := assertRunOwnedNaming(t, res.calls)
	assertOwnedDockerMutations(t, res.calls, tag, container, "")
	assertSingleUnforcedImageRm(t, res.calls, tag)
	rmIdx := callIndex(res.calls, "rm", "-f", container)
	imageRmIdx := callIndex(res.calls, "image", "rm")
	if rmIdx < 0 || rmIdx > imageRmIdx {
		t.Fatalf("expected rm -f %s before image rm, calls: %v", container, res.calls)
	}
	dirs := f.ownedArtifactDirs()
	if len(dirs) != 1 {
		t.Fatalf("expected the owned artifact directory to remain, got %v", dirs)
	}
	if !strings.Contains(res.stderr, dirs[0]) {
		t.Fatalf("stderr does not name retained directory %s:\n%s", dirs[0], res.stderr)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(runPID, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("fake docker run process %d still running", runPID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// INT-005
func TestDevAuditSmokeCleanupFailuresAreReportedWithoutChangingStatus(t *testing.T) {
	t.Run("image rm fails", func(t *testing.T) {
		f := newSmokeFixture(t)
		f.env["FAKE_DOCKER_IMAGE_RM_FAIL"] = "1"
		res := f.run()
		if res.exitCode != 0 {
			t.Fatalf("exit code = %d, want 0\nstderr:\n%s", res.exitCode, res.stderr)
		}
		tag, container := assertRunOwnedNaming(t, res.calls)
		assertOwnedDockerMutations(t, res.calls, tag, container, "")
		if want := "smoke: could not remove image " + tag; !strings.Contains(res.stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, res.stderr)
		}
	})

	t.Run("build fails", func(t *testing.T) {
		f := newSmokeFixture(t)
		f.env["FAKE_DOCKER_BUILD_EXIT"] = "1"
		f.env["FAKE_DOCKER_IMAGE_PRESENT"] = "0"
		res := f.run()
		if res.exitCode == 0 {
			t.Fatalf("exit code = 0, want nonzero\nstderr:\n%s", res.stderr)
		}
		tag := buildTag(t, res.calls)
		if smokeTagPattern.FindStringSubmatch(tag) == nil {
			t.Fatalf("build tag %q is not run-unique", tag)
		}
		assertOwnedDockerMutations(t, res.calls, tag, "", "")
		if rms := callsOf(res.calls, "image", "rm"); len(rms) != 0 {
			t.Fatalf("image rm must not run for an image that was never built: %v", rms)
		}
		if strings.Contains(res.stderr, "could not remove image") {
			t.Fatalf("stderr reports image cleanup failure for unbuilt image:\n%s", res.stderr)
		}
	})

	t.Run("artifact removal fails", func(t *testing.T) {
		f := newSmokeFixture(t)
		f.installFailingRm(filepath.Join(f.tmpDir, "agent-runner-dev-audit-smoke."))
		res := f.run()
		if res.exitCode != 0 {
			t.Fatalf("exit code = %d, want 0\nstderr:\n%s", res.exitCode, res.stderr)
		}
		tag, container := assertRunOwnedNaming(t, res.calls)
		assertOwnedDockerMutations(t, res.calls, tag, container, "")
		dirs := f.ownedArtifactDirs()
		if len(dirs) != 1 {
			t.Fatalf("expected the unremovable directory to remain, got %v", dirs)
		}
		if want := "smoke: could not remove artifact directory " + dirs[0]; !strings.Contains(res.stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, res.stderr)
		}
	})
}
