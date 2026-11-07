package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runview"
	"github.com/codagent/agent-runner/internal/stateio"
)

// E2E-001/002/003 share one build; every run has an isolated HOME and repo.
func TestCompletionLoopE2E(t *testing.T) {
	runnerBin := filepath.Join(t.TempDir(), "agent-runner")
	buildAgentRunner(t, findRepoRoot(t), runnerBin)
	for _, scenario := range []string{"early-stop", "budget-resume", "stall-resume", "before-repair"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			repo := t.TempDir()
			bin := t.TempDir()
			repo, e := filepath.EvalSymlinks(repo)
			if e != nil {
				t.Fatal(e)
			}
			writeSmokeProfileConfig(t, home)
			configDir := filepath.Join(repo, ".agent-runner")
			if e = os.MkdirAll(configDir, 0o700); e != nil {
				t.Fatal(e)
			}
			config := `profiles:
  completion_test:
    extends: default
    agents:
      implementor:
        cli: claude
        default_mode: autonomous
      tester:
        cli: claude
        default_mode: autonomous
      crosscheck:
        cli: claude
        default_mode: autonomous
      lead:
        cli: claude
        default_mode: autonomous
`
			mustWriteCompletionFile(t, filepath.Join(configDir, "config.yaml"), config, 0o600)
			mustWriteCompletionFile(t, filepath.Join(repo, ".gitignore"), ".agent-runner/\n", 0o600)
			change := filepath.Join(repo, "change")
			if e = os.MkdirAll(filepath.Join(change, "specs", "tiny"), 0o700); e != nil {
				t.Fatal(e)
			}
			for _, name := range []string{"proposal.md", "design.md", "test-plan.md"} {
				mustWriteCompletionFile(t, filepath.Join(change, name), "Tiny change\n", 0o600)
			}
			mustWriteCompletionFile(t, filepath.Join(change, "specs", "tiny", "spec.md"), "### Requirement: tiny\nThe product SHALL work.\n#### Scenario: works\n- **WHEN** used\n- **THEN** it works\n", 0o600)
			for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"config", "commit.gpgsign", "false"}, {"add", "."}, {"commit", "-qm", "docs: definition"}} {
				c := exec.Command("git", args...)
				c.Dir = repo
				if b, e := c.CombinedOutput(); e != nil {
					t.Fatalf("git: %v %s", e, b)
				}
			}
			mustWriteCompletionFile(t, filepath.Join(bin, "claude"), completionFakeCLI, 0o700)
			// A guard proves these flows never start the later Validator workflow.
			mustWriteCompletionFile(t, filepath.Join(bin, "agent-validator"), "#!/bin/sh\necho unexpected-validator > \"$HOME/validator-called\"\nexit 99\n", 0o700)
			env := smokeCommandEnv(os.Environ(), "HOME="+home, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AGENT_RUNNER_NO_TUI=1", "COMPLETION_SCENARIO="+scenario)
			args := []string{"run", "core:implement-change", "--headless", "--profile", "completion_test", "--until", "audit", "--param", "change_name=tiny", "--param", "change_dir=change", "--param", "change_label=change", "--param", "change_kind=spec-driven", "--param", "artifact_validation_instruction=", "--param", "skip_validator=true"}
			if scenario == "before-repair" {
				workflows := filepath.Join(configDir, "workflows")
				if e = os.MkdirAll(workflows, 0o700); e != nil {
					t.Fatal(e)
				}
				writeWorkflowFile(t, workflows, "before-repair-v1.0.yaml", `name: before-repair
steps:
  - id: check
    command: "echo original-check-output >&2; exit 2"
    repair:
      agent: implementor
      max: 1
      prompt: "BEFORE_REPAIR_MARKER"
`)
				args = []string{"--headless", "--profile", "completion_test", "before-repair"}
			}
			first := runRepairBlockedCLI(t, runnerBin, repo, env, args...)
			dirs, _ := filepath.Glob(filepath.Join(home, ".agent-runner", "projects", "*", "runs", "*"))
			if len(dirs) != 1 {
				t.Fatalf("runs %v\n%s", dirs, first.text)
			}
			session := dirs[0]
			state, e := stateio.ReadState(filepath.Join(session, "state.json"))
			if e != nil {
				t.Fatal(e)
			}
			switch scenario {
			case "early-stop":
				if first.exitCode != 0 || !strings.Contains(first.text, "audit") {
					t.Fatalf("early-stop exit %d\n%s", first.exitCode, first.text)
				}
				audit := readCompletionFile(t, filepath.Join(session, "audit.log"))
				if strings.Count(audit, "judge-completion] step_start") != 1 || strings.Count(audit, "continue-implementation] step_start") != 1 || strings.Contains(audit, "complete-evidence]") {
					t.Fatalf("wrong events:\n%s", audit)
				}
				var invocations []struct {
					Role, Session string
					Resume        bool
				}
				for _, line := range strings.Split(strings.TrimSpace(readCompletionFile(t, filepath.Join(home, "cli-calls.jsonl"))), "\n") {
					var call struct {
						Role, Session string
						Resume        bool
					}
					if e = json.Unmarshal([]byte(line), &call); e != nil {
						t.Fatal(e)
					}
					invocations = append(invocations, call)
				}
				sessions := map[string]string{}
				for _, call := range invocations {
					sessions[call.Role] = call.Session
					if call.Role == "continue" && !call.Resume {
						t.Fatal("continue did not resume")
					}
					if call.Role == "judge" && call.Resume {
						t.Fatal("judge resumed")
					}
				}
				if sessions["implement"] == "" || sessions["implement"] != sessions["continue"] || sessions["judge"] == sessions["implement"] {
					t.Fatalf("sessions %v", sessions)
				}
			case "before-repair":
				want := "check failed: before repair: original-check-output; blocked: human decision needed after 1 repair attempts"
				if first.exitCode == 0 || state.FailureReason != want || !strings.Contains(first.text, want) || runview.FailureReasonForSession(session) != want {
					t.Fatalf("state %q\n%s", state.FailureReason, first.text)
				}
				// Debug starts a workflow that captures the same reason before its fake
				// diagnostic agent. The captured value is persisted for downstream use.
				debug := runRepairBlockedCLI(t, runnerBin, repo, env, "debug", "--state", filepath.Base(session))
				if debug.exitCode != 0 || !strings.Contains(debug.text, want) {
					t.Fatalf("debug: %d\n%s", debug.exitCode, debug.text)
				}
			default:
				if first.exitCode == 0 || state.FailureKind != model.FailureInfrastructure || !strings.Contains(state.FailureReason, "failed (infrastructure)") {
					t.Fatalf("first: %+v\n%s", state, first.text)
				}
				snapshot := readCompletionFile(t, filepath.Join(session, "output", "continue-snapshot.json"))
				ledger := readCompletionFile(t, filepath.Join(session, "output", "completion-loop.jsonl"))
				if strings.Count(ledger, "\n") != 2 {
					t.Fatalf("ledger before resume %s", ledger)
				}
				// The fake resumed invocation saves the snapshot it sees before doing work.
				second := runRepairBlockedCLI(t, runnerBin, repo, env, "--headless", "--profile", "completion_test", "--resume", filepath.Base(session))
				if _, e := os.Stat(filepath.Join(home, "resumed-snapshot.json")); e != nil {
					t.Fatalf("no resumed invocation: %v\n%s", e, second.text)
				}
				if got := readCompletionFile(t, filepath.Join(home, "resumed-snapshot.json")); got != snapshot {
					t.Fatalf("snapshot changed on resume: %s -> %s", snapshot, got)
				}
				state, e = stateio.ReadState(filepath.Join(session, "state.json"))
				if e != nil {
					t.Fatal(e)
				}
				want := "implementation incomplete"
				judgments := 4
				passes := 4 // includes the resumed second pass
				if scenario == "stall-resume" {
					want = "implementation stalled"
					judgments = 2
					passes = 3
				}
				if second.exitCode == 0 || !strings.Contains(state.FailureReason, want) || !strings.Contains(second.text, state.FailureReason) {
					t.Fatalf("resumed state %+v\n%s", state, second.text)
				}
				ledger = readCompletionFile(t, filepath.Join(session, "output", "completion-loop.jsonl"))
				if strings.Count(ledger, "\n") != judgments {
					t.Fatalf("ledger %s", ledger)
				}
				count := strings.Count(readCompletionFile(t, filepath.Join(home, "cli-calls.jsonl")), `"Role": "continue"`)
				if count != passes {
					t.Fatalf("continue invocations=%d want %d", count, passes)
				}
			}
			if _, e = os.Stat(filepath.Join(home, "validator-called")); !os.IsNotExist(e) {
				t.Fatal("unexpected validator invocation")
			}
		})
	}
}

func mustWriteCompletionFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if e := os.WriteFile(path, []byte(body), mode); e != nil {
		t.Fatal(e)
	}
}
func readCompletionFile(t *testing.T, path string) string {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

const completionFakeCLI = `#!/usr/bin/env python3
import glob,json,os,pathlib,shutil,subprocess,sys
args=sys.argv[1:]
if '--version' in args:
 print('claude fixture');sys.exit(0)
home=pathlib.Path(os.environ['HOME'])
scenario=os.environ['COMPLETION_SCENARIO']
prompt=args[-1] if args else ''
def option(name):
 return args[args.index(name)+1] if name in args else ''
session=option('--resume') or option('--session-id')
output=pathlib.Path(glob.glob(str(home/'.agent-runner/projects/*/runs/*/output'))[0])
repo=pathlib.Path.cwd()
checklist=repo/'change/checklist.md'
header='| ID | Behavior | How to verify | Code | Evidence | Status |\n|---|---|---|---|---|---|\n'
def rows(second=False):
 return header+'| R-1 | first | unit | first.txt | TestFirst: '+(''+chr(96)+'first.log'+chr(96)+'' if (output/'evidence/first.log').exists() else 'not run')+' | '+('verified' if (output/'evidence/first.log').exists() else 'not verified')+' |\n| R-2 | second | unit | second.txt | '+('TestSecond: '+chr(96)+'second.log'+chr(96)+' | verified' if second else 'not yet implemented | not verified')+' |\n'
def commit(message):
 subprocess.run(['git','add','change/checklist.md']+[str(p.name) for p in repo.glob('*.txt')],check=True,stdout=subprocess.DEVNULL)
 subprocess.run(['git','commit','-qm',message,'--allow-empty'],check=True,stdout=subprocess.DEVNULL)
def record(role):
 with (home/'cli-calls.jsonl').open('a') as f: f.write(json.dumps(dict(Role=role,Session=session,Resume='--resume' in args))+'\n')
result='done'
if 'BEFORE_REPAIR_MARKER' in prompt:
 record('repair');result='human decision needed\nREPAIR_BLOCKED'
elif 'Judge whether the implementation' in prompt:
 record('judge')
 if scenario=='early-stop' and 'early stop with R-2 unfinished' not in prompt: raise RuntimeError('implementor response missing')
 (output/'completion-verdict.json').write_text(json.dumps(dict(verdict='continue',reason='unfinished',remaining=['finish R-2'])))
elif '<remaining-work>' in prompt:
 record('continue')
 counter=home/'passes';n=int(counter.read_text())+1 if counter.exists() else 1;counter.write_text(str(n))
 if scenario=='early-stop':
  (repo/'second.txt').write_text('second row implemented');(output/'evidence/second.log').write_text('TestSecond PASS\n');checklist.write_text(rows(True));commit('feat: finish second row');(output/'implementation-complete.md').write_text('done')
 else:
  if n==2:
   if scenario=='budget-resume':commit('feat: progress before crash')
   print('Selected model is at capacity',file=sys.stderr);sys.exit(1)
  if n==3:
   shutil.copyfile(output/'continue-snapshot.json',home/'resumed-snapshot.json')
  else:
   (repo/'progress.txt').write_text(str(n));commit('feat: progress')
elif 'Implement change ' in prompt and 'against the checklist' in prompt:
 record('implement');(output/'evidence').mkdir(exist_ok=True)
 (repo/'first.txt').write_text('first row implemented');(output/'evidence/first.log').write_text('TestFirst PASS\n');checklist.write_text(rows());commit('feat: first row');result='early stop with R-2 unfinished'
elif 'checklist' in prompt and ('Plan how' in prompt or 'Write' in prompt or 'write' in prompt) and 'Audit it against' not in prompt:
 record('checklist');checklist.write_text(rows())
elif 'Audit it against' in prompt:
 record('audit');result='No findings.'
else:
 record('debug');print(prompt)
print(json.dumps(dict(type='result',result=result,session_id=session)))
`
