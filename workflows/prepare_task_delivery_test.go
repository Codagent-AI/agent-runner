package builtinworkflows

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func runPrepareTaskDelivery(t *testing.T, env []string, input string) (stdout, stderr string, exitCode int) {
	t.Helper()
	script, err := ReadAsset("core/prepare-task-delivery.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/prepare-task-delivery.sh): %v", err)
	}
	scriptPath := filepath.Join(t.TempDir(), "prepare-task-delivery.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	cmd := exec.Command("sh", scriptPath)
	cmd.Stdin = strings.NewReader(input)
	if env != nil {
		cmd.Env = env
	}
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run script: %v", err)
	}
	return out.String(), errOut.String(), exitCode
}

func TestPrepareTaskDeliveryScript(t *testing.T) {
	head := strings.Repeat("0123456789ab", 3) + "0123"

	for _, parser := range []string{"jq", "python3"} {
		t.Run(parser, func(t *testing.T) {
			var env []string
			if parser == "python3" {
				env = withoutJQ(t)
			} else if _, err := exec.LookPath("jq"); err != nil {
				t.Skip("jq not installed")
			}

			t.Run("prints the record path and start time and prepares the directory", func(t *testing.T) {
				sessionDir := filepath.Join(t.TempDir(), "session 'quoted' & spaced")
				before := time.Now().Unix()
				stdout, stderr, code := runPrepareTaskDelivery(t, env, fmt.Sprintf(
					`{"session_dir":%q,"task_file":"openspec/changes/x/tasks/01-add thing!.md","starting_head":%q}`, sessionDir, head))
				after := time.Now().Unix()
				if code != 0 {
					t.Fatalf("exit = %d\n%s", code, stderr)
				}
				var got map[string]string
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("output %q is not a JSON string map: %v", stdout, err)
				}
				if len(got) != 2 {
					t.Fatalf("output = %v, want record_path and started_at only", got)
				}
				startedAt, err := strconv.ParseInt(got["started_at"], 10, 64)
				if err != nil || startedAt < before || startedAt > after {
					t.Fatalf("started_at = %q, want epoch seconds between %d and %d", got["started_at"], before, after)
				}
				want := filepath.Join(sessionDir, "output", "task-delivery", "01-add_thing_-"+got["started_at"]+"-0123456789ab.json")
				if got["record_path"] != want {
					t.Fatalf("record_path = %q, want %q", got["record_path"], want)
				}
				if info, err := os.Stat(filepath.Dir(want)); err != nil || !info.IsDir() {
					t.Fatalf("record directory not created: %v", err)
				}
				if _, err := os.Stat(want); !os.IsNotExist(err) {
					t.Fatalf("record file exists after prepare: %v", err)
				}
			})

			t.Run("accepts a captured starting head with a trailing newline", func(t *testing.T) {
				stdout, stderr, code := runPrepareTaskDelivery(t, env, fmt.Sprintf(
					`{"session_dir":%q,"task_file":"task.md","starting_head":%q}`, t.TempDir(), head+"\n"))
				if code != 0 {
					t.Fatalf("exit = %d\n%s", code, stderr)
				}
				if !strings.Contains(stdout, "-0123456789ab.json") {
					t.Fatalf("stdout = %q, want head prefix in the record path", stdout)
				}
			})

			t.Run("removes a record already at the path", func(t *testing.T) {
				sessionDir := t.TempDir()
				dir := filepath.Join(sessionDir, "output", "task-delivery")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				// Pre-create the records for the current and next second so the
				// run's own path is guaranteed to exist beforehand.
				now := time.Now().Unix()
				for _, ts := range []int64{now, now + 1} {
					mustWriteFile(t, filepath.Join(dir, fmt.Sprintf("task-%d-0123456789ab.json", ts)), "{}")
					mustWriteFile(t, filepath.Join(dir, fmt.Sprintf("task-%d-0123456789ab.accepted", ts)), "stale")
				}
				other := filepath.Join(dir, "other-1-0123456789ab.json")
				mustWriteFile(t, other, "{}")

				stdout, stderr, code := runPrepareTaskDelivery(t, env, fmt.Sprintf(
					`{"session_dir":%q,"task_file":"task.md","starting_head":%q}`, sessionDir, head))
				if code != 0 {
					t.Fatalf("exit = %d\n%s", code, stderr)
				}
				var got map[string]string
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(got["record_path"]); !os.IsNotExist(err) {
					t.Fatalf("stale record at %s not removed: %v", got["record_path"], err)
				}
				accepted := strings.TrimSuffix(got["record_path"], ".json") + ".accepted"
				if _, err := os.Stat(accepted); !os.IsNotExist(err) {
					t.Fatalf("stale accepted marker at %s not removed: %v", accepted, err)
				}
				if _, err := os.Stat(other); err != nil {
					t.Fatalf("unrelated record removed: %v", err)
				}
			})

			t.Run("sanitized task keys", func(t *testing.T) {
				for taskFile, wantKey := range map[string]string{
					"tasks/02-fix.v2_a-b.md": "02-fix.v2_a-b",
					"tasks/é task.md":        "___task",
					"task":                   "task",
				} {
					stdout, stderr, code := runPrepareTaskDelivery(t, env, fmt.Sprintf(
						`{"session_dir":%q,"task_file":%q,"starting_head":%q}`, t.TempDir(), taskFile, head))
					if code != 0 {
						t.Fatalf("%s: exit = %d\n%s", taskFile, code, stderr)
					}
					var got map[string]string
					if err := json.Unmarshal([]byte(stdout), &got); err != nil {
						t.Fatal(err)
					}
					pattern := "^" + regexp.QuoteMeta(wantKey) + `-[0-9]+-0123456789ab\.json$`
					if !regexp.MustCompile(pattern).MatchString(filepath.Base(got["record_path"])) {
						t.Fatalf("%s: record file %q, want key %q", taskFile, filepath.Base(got["record_path"]), wantKey)
					}
				}
			})

			t.Run("rejects invalid inputs", func(t *testing.T) {
				sessionDir := t.TempDir()
				for name, input := range map[string]string{
					"not JSON":              "{",
					"not an object":         "[]",
					"missing session_dir":   fmt.Sprintf(`{"task_file":"t.md","starting_head":%q}`, head),
					"relative session_dir":  fmt.Sprintf(`{"session_dir":"rel","task_file":"t.md","starting_head":%q}`, head),
					"missing task_file":     fmt.Sprintf(`{"session_dir":%q,"starting_head":%q}`, sessionDir, head),
					"empty task_file":       fmt.Sprintf(`{"session_dir":%q,"task_file":"","starting_head":%q}`, sessionDir, head),
					"non-hex starting_head": fmt.Sprintf(`{"session_dir":%q,"task_file":"t.md","starting_head":"HEAD"}`, sessionDir),
					"control character":     fmt.Sprintf(`{"session_dir":%q,"task_file":"t\n.md","starting_head":%q}`, sessionDir, head),
				} {
					t.Run(name, func(t *testing.T) {
						stdout, stderr, code := runPrepareTaskDelivery(t, env, input)
						if code != 2 {
							t.Fatalf("exit = %d, want 2\nstdout:%s\nstderr:%s", code, stdout, stderr)
						}
					})
				}
			})
		})
	}
}
