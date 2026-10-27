package builtinworkflows

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bookkeepingEnv is a scratch project whose PATH holds stub agent-validator and
// openspec executables that log each invocation.
type bookkeepingEnv struct {
	t       *testing.T
	dir     string
	scripts string
	bin     string
	log     string
}

func newBookkeepingEnv(t *testing.T, namespace string, config, validatorInstalled bool, detectExit int) *bookkeepingEnv {
	t.Helper()
	root := t.TempDir()
	env := &bookkeepingEnv{t: t, dir: filepath.Join(root, "project"), scripts: filepath.Join(root, "scripts"), bin: filepath.Join(root, "bin"), log: filepath.Join(root, "calls.log")}
	for _, d := range []string{env.dir, env.scripts, env.bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	assets, err := ListAssets(namespace)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range assets {
		if !strings.HasSuffix(asset, ".sh") {
			continue
		}
		body, err := ReadAsset(namespace + "/" + asset)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(env.scripts, filepath.Base(asset)), body, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if config {
		writeValidatorConfig(t, env.dir)
	}
	if validatorInstalled {
		env.stub("agent-validator", `printf 'agent-validator %s\n' "$*" >>"$CALL_LOG"
if [ "$1" = detect ]; then exit `+string(rune('0'+detectExit))+`; fi
exit 0`)
	}
	env.stub("openspec", `printf 'openspec %s\n' "$*" >>"$CALL_LOG"; mkdir -p "openspec/changes/$3"`)
	runCmdIn(t, env.dir, "", "git", "init", "-q", "-b", "feature")
	runCmdIn(t, env.dir, "", "git", "config", "user.email", "t@example.com")
	runCmdIn(t, env.dir, "", "git", "config", "user.name", "T")
	runCmdIn(t, env.dir, "", "git", "config", "commit.gpgsign", "false")
	return env
}

func (e *bookkeepingEnv) stub(name, body string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		e.t.Fatal(err)
	}
}

func (e *bookkeepingEnv) run(script, payload string) (string, error) {
	e.t.Helper()
	cmd := osexec.Command("sh", filepath.Join(e.scripts, script))
	cmd.Dir = e.dir
	cmd.Env = append(os.Environ(), "PATH="+e.bin+":/usr/bin:/bin", "CALL_LOG="+e.log)
	cmd.Stdin = strings.NewReader(payload)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *bookkeepingEnv) validatorCalls() []string {
	body, _ := os.ReadFile(e.log)
	var calls []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.HasPrefix(line, "agent-validator") {
			calls = append(calls, line)
		}
	}
	return calls
}

func selectionPayload(selection string) string {
	if selection == "-" {
		return `{"change_name":"demo","change_dir":"openspec/changes/demo"}`
	}
	return `{"change_name":"demo","change_dir":"openspec/changes/demo","validation_workflow":"` + selection + `"}`
}

// INT-006: creation runs the install check and detect only when the selected
// validation workflow runs Agent Validator.
func TestCreateChangeBookkeepingFollowsSelection(t *testing.T) {
	for _, namespace := range []string{"openspec", "spec-driven"} {
		for _, tc := range []struct {
			name               string
			selection          string // "-" omits the param
			config, installed  bool
			detectExit         int // 0 unvalidated changes, 2 clean
			wantOK             bool
			wantDetect         bool
			wantOutputContains string
		}{
			{"none in configured project with unvalidated changes", "none", true, true, 0, true, false, ""},
			{"none with nothing installed", "none", true, false, 2, true, false, ""},
			{"auto without config and without executable", "auto", false, false, 2, true, false, ""},
			{"auto with config and unvalidated changes", "auto", true, true, 0, false, true, "Unvalidated changes detected"},
			{"auto with config and clean tree", "auto", true, true, 2, true, true, ""},
			{"built-in auto reference without config", "builtin:core/validation-auto-v1.0.yaml", false, false, 2, true, false, ""},
			{"agent-validator without config but installed", "agent-validator", false, true, 2, true, true, ""},
			{"agent-validator not installed", "agent-validator", true, false, 2, false, false, "agent-validator is not installed"},
			{"omitted param with config", "-", true, true, 2, true, true, ""},
			{"omitted param without config", "-", false, false, 2, true, false, ""},
		} {
			t.Run(namespace+"/"+tc.name, func(t *testing.T) {
				env := newBookkeepingEnv(t, namespace, tc.config, tc.installed, tc.detectExit)
				out, err := env.run("create-change.sh", selectionPayload(tc.selection))
				if (err == nil) != tc.wantOK {
					t.Fatalf("ok = %v, want %v; output %q", err == nil, tc.wantOK, out)
				}
				if detected := len(env.validatorCalls()) > 0; detected != tc.wantDetect {
					t.Errorf("agent-validator calls = %v, want detect = %v", env.validatorCalls(), tc.wantDetect)
				}
				if tc.wantOutputContains != "" && !strings.Contains(out, tc.wantOutputContains) {
					t.Errorf("output %q lacks %q", out, tc.wantOutputContains)
				}
			})
		}
	}
}

// INT-006: committing the plan advances the Validator baseline only when the
// selected validation workflow runs Agent Validator.
func TestCommitChangePlanBaselineFollowsSelection(t *testing.T) {
	for _, tc := range []struct {
		name        string
		selection   string
		config      bool
		wantAdvance bool
	}{
		{"none in configured project", "none", true, false},
		{"auto with config", "auto", true, true},
		{"auto without config", "auto", false, false},
		{"agent-validator", "agent-validator", false, true},
		{"omitted with config", "-", true, true},
		{"omitted without config", "-", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newBookkeepingEnv(t, "core", tc.config, true, 2)
			changeDir := filepath.Join(env.dir, "openspec", "changes", "demo")
			if err := os.MkdirAll(changeDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(changeDir, "proposal.md"), []byte("p\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.config {
				runCmdIn(t, env.dir, "", "git", "add", ".validator")
			}
			if out, err := env.run("commit-change-plan.sh", selectionPayload(tc.selection)); err != nil {
				t.Fatalf("commit-change-plan: %v\n%s", err, out)
			}
			advanced := len(env.validatorCalls()) == 1 && env.validatorCalls()[0] == "agent-validator skip"
			if advanced != tc.wantAdvance {
				t.Errorf("baseline advanced = %v, want %v (calls %v)", advanced, tc.wantAdvance, env.validatorCalls())
			}
		})
	}
}

// The symlinked-openspec path obeys the same gate.
func TestCommitChangePlanSymlinkedOpenspecFollowsSelection(t *testing.T) {
	for _, tc := range []struct {
		selection   string
		wantAdvance bool
	}{{"none", false}, {"agent-validator", true}} {
		t.Run(tc.selection, func(t *testing.T) {
			env := newBookkeepingEnv(t, "core", true, true, 2)
			target := filepath.Join(t.TempDir(), "elsewhere")
			if err := os.MkdirAll(filepath.Join(target, "changes", "demo"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(env.dir, "openspec")); err != nil {
				t.Fatal(err)
			}
			if out, err := env.run("commit-change-plan.sh", selectionPayload(tc.selection)); err != nil {
				t.Fatalf("commit-change-plan: %v\n%s", err, out)
			}
			if advanced := len(env.validatorCalls()) == 1; advanced != tc.wantAdvance {
				t.Errorf("baseline advanced = %v, want %v", advanced, tc.wantAdvance)
			}
		})
	}
}

// validator-pr-gate.sh treats a skipped validation like a pass.
func TestValidatorPRGateAcceptsSkipped(t *testing.T) {
	script := materializeAsset(t, "core/validator-pr-gate.sh")
	for _, tc := range []struct {
		name, result string
		wantOK       bool
	}{
		{"pass", "PASS\n", true},
		{"skipped", "SKIPPED\nno config\n", true},
		{"missing", "", false},
		{"garbage", "MAYBE\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resultFile := filepath.Join(t.TempDir(), "result.txt")
			if tc.result != "" {
				if err := os.WriteFile(resultFile, []byte(tc.result), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := osexec.Command("sh", script)
			cmd.Dir = newScratchRepo(t)
			cmd.Stdin = strings.NewReader(`{"result_file":"` + resultFile + `"}`)
			err := cmd.Run()
			if (err == nil) != tc.wantOK {
				t.Errorf("ok = %v, want %v", err == nil, tc.wantOK)
			}
		})
	}
}
