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

func TestPrepareTaskDeliveryScript(t *testing.T) {
	head := strings.Repeat("0123456789ab", 3) + "0123"
	script := writeAssetScript(t, "core/prepare-task-delivery.sh")

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
				stdout, stderr, code := runScriptSplit(t, script, "", env, fmt.Sprintf(
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
				wantDir := filepath.Join(sessionDir, "output", "task-delivery")
				pattern := `^01-add_thing_-` + regexp.QuoteMeta(got["started_at"]) + `-0123456789ab-[A-Za-z0-9]+\.json$`
				if filepath.Dir(got["record_path"]) != wantDir || !regexp.MustCompile(pattern).MatchString(filepath.Base(got["record_path"])) {
					t.Fatalf("record_path = %q, want filename matching %q in %q", got["record_path"], pattern, wantDir)
				}
				if info, err := os.Stat(wantDir); err != nil || !info.IsDir() {
					t.Fatalf("record directory not created: %v", err)
				}
				if _, err := os.Stat(got["record_path"]); !os.IsNotExist(err) {
					t.Fatalf("record file exists after prepare: %v", err)
				}
				if _, err := os.Stat(strings.TrimSuffix(got["record_path"], ".json")); err != nil {
					t.Fatalf("record path reservation missing: %v", err)
				}
			})

			t.Run("concurrent deliveries get distinct paths and keep each other's records", func(t *testing.T) {
				sessionDir := t.TempDir()
				// Fix the clock so both starts exercise the same-second collision.
				bin := t.TempDir()
				if err := os.WriteFile(filepath.Join(bin, "date"), []byte("#!/bin/sh\necho 1234567890\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				path := os.Getenv("PATH")
				for _, pair := range env {
					if strings.HasPrefix(pair, "PATH=") {
						path = strings.TrimPrefix(pair, "PATH=")
					}
				}
				testEnv := append(append([]string(nil), env...), "PATH="+bin+string(os.PathListSeparator)+path)
				dir := filepath.Join(sessionDir, "output", "task-delivery")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				other := filepath.Join(dir, "other-1-0123456789ab.json")
				mustWriteFile(t, other, "other record")

				prepare := func() map[string]string {
					stdout, stderr, code := runScriptSplit(t, script, "", testEnv, fmt.Sprintf(
						`{"session_dir":%q,"task_file":"task.md","starting_head":%q}`, sessionDir, head))
					if code != 0 {
						t.Fatalf("exit = %d\n%s", code, stderr)
					}
					var got map[string]string
					if err := json.Unmarshal([]byte(stdout), &got); err != nil {
						t.Fatal(err)
					}
					return got
				}
				first := prepare()
				mustWriteFile(t, first["record_path"], "first record")
				accepted := strings.TrimSuffix(first["record_path"], ".json") + ".accepted"
				mustWriteFile(t, accepted, "accepted")
				second := prepare()
				if first["record_path"] == second["record_path"] {
					t.Fatalf("two deliveries got the same record path: %q", first["record_path"])
				}
				for path, want := range map[string]string{
					first["record_path"]: "first record",
					accepted:             "accepted",
					other:                "other record",
				} {
					got, err := os.ReadFile(path)
					if err != nil || string(got) != want {
						t.Fatalf("record %q = %q, %v; want %q", path, got, err, want)
					}
				}
			})

			t.Run("sanitized task keys", func(t *testing.T) {
				for taskFile, wantKey := range map[string]string{
					"tasks/02-fix.v2_a-b.md": "02-fix.v2_a-b",
					"tasks/é task.md":        "___task",
					"task":                   "task",
				} {
					stdout, stderr, code := runScriptSplit(t, script, "", env, fmt.Sprintf(
						`{"session_dir":%q,"task_file":%q,"starting_head":%q}`, t.TempDir(), taskFile, head))
					if code != 0 {
						t.Fatalf("%s: exit = %d\n%s", taskFile, code, stderr)
					}
					var got map[string]string
					if err := json.Unmarshal([]byte(stdout), &got); err != nil {
						t.Fatal(err)
					}
					pattern := "^" + regexp.QuoteMeta(wantKey) + `-[0-9]+-0123456789ab-[A-Za-z0-9]+\.json$`
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
					"newline after head":    fmt.Sprintf(`{"session_dir":%q,"task_file":"t.md","starting_head":%q}`, sessionDir, head+"\n"),
				} {
					t.Run(name, func(t *testing.T) {
						stdout, stderr, code := runScriptSplit(t, script, "", env, input)
						if code != 2 {
							t.Fatalf("exit = %d, want 2\nstdout:%s\nstderr:%s", code, stdout, stderr)
						}
					})
				}
			})
		})
	}
}
