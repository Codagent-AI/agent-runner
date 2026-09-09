package exec

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

// This opt-in test must be run for acceptance with the explicitly selected
// local feature build. All provider processes are deterministic local fakes.
func TestLocalValidatorDeliveryIntegration(t *testing.T) {
	entry := os.Getenv("AGENT_RUNNER_TEST_VALIDATOR_ENTRYPOINT")
	if entry == "" {
		t.Skip("set AGENT_RUNNER_TEST_VALIDATOR_ENTRYPOINT to the local Validator dist/index.js for acceptance")
	}
	build, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("local Validator entrypoint=%s sha256=%x", entry, sha256.Sum256(build))
	node, err := osexec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"reviews", "failed-review", "zero-dispatch"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			project := filepath.Join(root, "project")
			bin := filepath.Join(root, "bin")
			dir := filepath.Join(root, "run")
			for _, p := range []string{filepath.Join(project, ".validator"), bin, dir} {
				if err := os.MkdirAll(p, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, text string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(text), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			config := "base_branch: main\nlog_dir: logs\nmax_previous_logs: 0\nallow_parallel: false\ncli:\n  default_preference: [codex]\n  adapters:\n    codex:\n      allow_tool_use: false\nentry_points:\n  - path: .\n"
			if scenario == "zero-dispatch" {
				config += "    checks:\n      - local-check:\n          command: 'true'\n"
			} else {
				config += "    reviews:\n      - all-reviewers:\n          builtin: all-reviewers\n          num_reviews: 2\n          parallel: false\n"
			}
			write(filepath.Join(project, ".validator", "config.yml"), config)
			write(filepath.Join(project, ".gitignore"), "logs/\n")
			write(filepath.Join(project, "example.go"), "package example\nconst value=1\n")
			for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "initial"}} {
				cmd := osexec.Command("git", args...)
				cmd.Dir = project
				if b, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git: %v %s", err, b)
				}
			}
			write(filepath.Join(project, "example.go"), "package example\nconst value=2\n")
			telemetry, err := os.ReadFile("../measurements/testdata/codex-0.153.4.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(string(telemetry))
			exitCode := 0
			if scenario == "failed-review" {
				exitCode = 17
			}
			fake := fmt.Sprintf("#!%s\nconst fs=require('fs');if(!process.argv.includes('exec')){console.log('codex-cli fixture');process.exit(0)};fs.readFileSync(0,'utf8');process.stdout.write(%s);process.stdout.write(JSON.stringify({type:'item.completed',item:{type:'agent_message',text:JSON.stringify({status:'pass',message:'fixture pass'})}})+'\\n');process.exitCode=%d;\n", node, encoded, exitCode)
			write(filepath.Join(bin, "codex"), fake)
			for _, name := range []string{"claude", "copilot", "gemini", "opencode", "agent"} {
				write(filepath.Join(bin, name), "#!/bin/sh\nexit 1\n")
			}
			wrapper := filepath.Join(bin, "agent-validator")
			calls := filepath.Join(root, "producer-calls")
			write(wrapper, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> "+textfmt.ShellQuote(calls)+"\nexec "+textfmt.ShellQuote(node)+" "+textfmt.ShellQuote(entry)+" \"$@\"\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE", wrapper)
			for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITHUB_BASE_REF", "GITHUB_SHA"} {
				t.Setenv(key, "")
			}
			collector := metrics.NewCollector(dir, "run", "wf", time.Now())
			ctx := &model.ExecutionContext{SessionDir: dir, WorkingDir: project, ExecutionSessionID: "original", AuditLogger: metrics.NewPipeline(collector, nil)}
			step := &model.Step{ID: "validate", MetricsSource: "agent-validator"}
			capture, env, err := prepareNestedMetricsEnvironment(step, ctx)
			if err != nil || capture.contextID == "" {
				t.Fatalf("instrumentation failed: %v", err)
			}
			cmd := osexec.Command(wrapper, "run", "--metrics-consumer", "agent-runner", "--metrics-context", capture.contextID)
			cmd.Dir = project
			cmd.Env = append(os.Environ(), env...)
			output, runErr := cmd.CombinedOutput()
			if scenario == "failed-review" && runErr == nil || scenario != "failed-review" && runErr != nil {
				t.Fatalf("unexpected validation result: %v\n%s", runErr, output)
			}
			launch, err := readValidatorMetricsLaunch(capture.path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = exportValidatorMetrics(&launch); err != nil {
				t.Fatalf("real export rejected: %v", err)
			}
			emitNestedMetricCapture(ctx, step, "", capture)
			raw, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
			if err != nil {
				t.Fatal(err)
			}
			var artifact metrics.Artifact
			if err = json.Unmarshal(raw, &artifact); err != nil {
				t.Fatal(err)
			}
			wantCollection := "complete"
			if scenario == "failed-review" {
				wantCollection = "partial"
			}
			if artifact.ValidatorDelivery == nil || artifact.ValidatorDelivery.Delivery != "complete" || artifact.ValidatorDelivery.Collection != wantCollection {
				t.Fatalf("incomplete real delivery: %+v contexts=%+v", artifact.ValidatorDelivery, artifact.ValidatorContexts)
			}
			want := 2
			if scenario == "zero-dispatch" {
				want = 0
			}
			if len(artifact.Steps) != want {
				t.Fatalf("dispatch count=%d, want %d", len(artifact.Steps), want)
			}
			if scenario == "reviews" && (artifact.Totals.TokenTotals == nil || artifact.Totals.TokenTotals.Total != 12771*int64(want)) {
				t.Fatalf("tokens=%+v", artifact.Totals.TokenTotals)
			}
			if scenario == "failed-review" {
				total := artifact.MeasurementTotals["normalized_total"]
				if artifact.Totals.TokenTotals != nil || total.Availability != "partial" || total.KnownSubtotal != 25542 {
					t.Fatalf("failed provider lost known subtotal or fabricated complete scalar: %+v", total)
				}
			}
			// Repeated metrics-only recovery must retain work and original attribution.
			ctx.ExecutionSessionID = "later"
			recoveryErr := RecoverValidatorMetrics(ctx)
			if scenario == "failed-review" && recoveryErr == nil || scenario != "failed-review" && recoveryErr != nil {
				t.Fatalf("unexpected recovery result: %v", recoveryErr)
			}
			raw, err = os.ReadFile(filepath.Join(dir, metrics.FileName))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), `"execution_session_id": "later"`) {
				t.Fatal("recovery stole original attribution")
			}
			log, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("scenario=%s heads=%d dispatches=%d tokens=%+v\nlocal operations:\n%s", scenario, len(artifact.MeasurementHeads), len(artifact.Steps), artifact.Totals.TokenTotals, log)
		})
	}
}
