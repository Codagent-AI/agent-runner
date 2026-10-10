package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/runview"
	"github.com/google/go-cmp/cmp"
)

func TestPostRunEscapeRelaunchCwd(t *testing.T) {
	for _, path := range []string{"switcher", "live"} {
		for _, originCase := range []string{"recorded origin", "unknown origin", "deleted origin", "file origin"} {
			t.Run(path+"/"+originCase, func(t *testing.T) {
				current := t.TempDir()
				t.Chdir(current)
				project := t.TempDir()
				home := t.TempDir()
				t.Setenv("HOME", home)
				storage := filepath.Join(home, ".agent-runner", "projects", "encoded-project")
				session := filepath.Join(storage, "runs", "example-run")
				if err := os.MkdirAll(session, 0o755); err != nil {
					t.Fatal(err)
				}
				origin := ""
				wantCwd := current
				switch originCase {
				case "recorded origin":
					origin, wantCwd = project, project
				case "deleted origin":
					origin = filepath.Join(project, "deleted-worktree")
					if err := os.Mkdir(origin, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(origin); err != nil {
						t.Fatal(err)
					}
				case "file origin":
					origin = filepath.Join(project, "file")
					if err := os.WriteFile(origin, nil, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				meta, err := json.Marshal(map[string]string{"path": origin})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(storage, "meta.json"), meta, 0o600); err != nil {
					t.Fatal(err)
				}
				wantCwd, err = filepath.EvalSymlinks(wantCwd)
				if err != nil {
					t.Fatal(err)
				}
				rv, err := runview.New(session, storage, runview.FromInspect)
				if err != nil {
					t.Fatal(err)
				}

				originalExecutable, originalExec := currentExecutable, execProcess
				t.Cleanup(func() { currentExecutable, execProcess = originalExecutable, originalExec })
				currentExecutable = func() (string, error) { return "/tmp/agent-runner", nil }
				var gotArgs []string
				var gotCwd string
				execProcess = func(_ string, args []string, _ []string) error {
					gotArgs = append([]string(nil), args[1:]...)
					var err error
					gotCwd, err = os.Getwd()
					if err != nil {
						return err
					}
					gotCwd, err = filepath.EvalSymlinks(gotCwd)
					return err
				}
				if path == "switcher" {
					sw := &switcher{runview: rv, mode: showingRunView}
					sw.Update(runview.ResumeListMsg{})
					if code, handled := terminalSwitcherResult(sw, ""); !handled || code != 0 {
						t.Fatalf("exit result = (%d, %v)", code, handled)
					}
				} else {
					rv.Update(runview.ResumeListMsg{})
					resultCh := make(chan runner.WorkflowResult)
					close(resultCh)
					result, handled := terminalLiveTUIResult(rv, resultCh, storage, session, liveTUIOptions{})
					if !handled || result.exitCode != 0 {
						t.Fatalf("exit result = (%+v, %v)", result, handled)
					}
				}
				if diff := cmp.Diff([]string{"--resume"}, gotArgs); diff != "" {
					t.Errorf("exec args (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(wantCwd, gotCwd); diff != "" {
					t.Errorf("exec cwd (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestResumeRunRejectsUnusableProjectDir(t *testing.T) {
	t.Chdir(t.TempDir())
	originalExec := execProcess
	t.Cleanup(func() { execProcess = originalExec })
	called := false
	execProcess = func(_ string, _ []string, _ []string) error { called = true; return nil }
	missing := filepath.Join(t.TempDir(), "missing")
	if code := execRunnerResumeWithProfile("run-123", missing, "copilot"); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if called {
		t.Fatal("run resume must not exec from an incorrect working directory")
	}
}
