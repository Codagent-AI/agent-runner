package runview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

func TestCostBasisAuditReplayINT003(t *testing.T) {
	dir := t.TempDir()
	wf := filepath.Join(dir, "costs-v1.0.yaml")
	writeFile(t, wf, "name: costs\nsteps:\n  - id: reported\n    agent: tester\n    session: new\n    prompt: reported\n    cli: claude\n    mode: autonomous\n  - id: estimated\n    agent: tester\n    session: new\n    prompt: estimated\n    cli: codex\n    mode: autonomous\n  - id: unknown\n    agent: tester\n    session: new\n    prompt: unknown\n    cli: codex\n    mode: autonomous\n")
	child := filepath.Join(dir, "child-v1.0.yaml")
	writeFile(t, child, `name: child
steps:
 - id: estimate
   agent: tester
   session: new
   prompt: estimate
   mode: autonomous
 - id: missing
   agent: tester
   session: new
   prompt: missing
   mode: autonomous
`)
	data, err := os.ReadFile(wf)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, wf, string(data)+`  - id: loop
    loop:
      max: 1
    steps:
      - id: report
        agent: tester
        session: new
        prompt: report
        mode: autonomous
      - id: estimate
        agent: tester
        session: new
        prompt: estimate
        mode: autonomous
  - id: nested
    workflow: child-v1.0.yaml
`)
	writeFile(t, filepath.Join(dir, "state.json"), `{"workflowFile":"`+wf+`","workflowName":"costs","completed":true}`)
	writeFile(t, filepath.Join(dir, "audit.log"), strings.Join([]string{
		`2026-10-20T00:00:00Z run_start {}`,
		`2026-10-20T00:00:00Z [reported] step_start {"mode":"autonomous","cli":"codex"}`,
		`2026-10-20T00:00:01Z [reported] step_end {"outcome":"success","identity":{"agent_invoked":true},"estimated_api_cost_usd":0.2}`,
		`2026-10-20T00:00:00Z [estimated] step_start {"mode":"autonomous","cli":"codex"}`,
		`2026-10-20T00:00:02Z [estimated] step_end {"outcome":"success","identity":{"agent_invoked":true},"estimated_api_cost_usd":0.98,"cost_basis":"estimated"}`,
		`2026-10-20T00:00:00Z [unknown] step_start {"mode":"autonomous","cli":"codex"}`,
		`2026-10-20T00:00:03Z [unknown] step_end {"outcome":"success","identity":{"agent_invoked":true},"cost_unavailable_reason":"model-unpriced"}`,
		`2026-10-20T00:00:04Z [loop] step_start {"loop_type":"counted","max":1}`,
		`2026-10-20T00:00:04Z [loop:0] iteration_start {"iteration":0}`,
		`2026-10-20T00:00:04Z [loop:0, report] step_start {"mode":"autonomous","cli":"claude"}`,
		`2026-10-20T00:00:05Z [loop:0, report] step_end {"outcome":"success","identity":{"agent_invoked":true},"estimated_api_cost_usd":0.2,"cost_basis":"reported"}`,
		`2026-10-20T00:00:05Z [loop:0, estimate] step_start {"mode":"autonomous","cli":"codex"}`,
		`2026-10-20T00:00:06Z [loop:0, estimate] step_end {"outcome":"success","identity":{"agent_invoked":true},"estimated_api_cost_usd":0.98,"cost_basis":"estimated"}`,
		`2026-10-20T00:00:06Z [loop:0] iteration_end {"iteration":0,"outcome":"success"}`,
		`2026-10-20T00:00:06Z [loop] step_end {"outcome":"success"}`,
		`2026-10-20T00:00:07Z [nested] step_start {}`,
		`2026-10-20T00:00:07Z [nested] sub_workflow_start {"workflow_path":"` + child + `"}`,
		`2026-10-20T00:00:07Z [nested, sub:child, estimate] step_start {"mode":"autonomous","cli":"codex"}`,
		`2026-10-20T00:00:08Z [nested, sub:child, estimate] step_end {"outcome":"success","identity":{"agent_invoked":true},"estimated_api_cost_usd":0.98,"cost_basis":"estimated"}`,
		`2026-10-20T00:00:08Z [nested, sub:child, missing] step_start {"mode":"autonomous","cli":"codex"}`,
		`2026-10-20T00:00:09Z [nested, sub:child, missing] step_end {"outcome":"success","identity":{"agent_invoked":true},"cost_unavailable_reason":"model-unpriced"}`,
		`2026-10-20T00:00:10Z [nested] sub_workflow_end {"outcome":"success"}`,
		`2026-10-20T00:00:10Z [nested] step_end {"outcome":"success"}`,
	}, "\n")+"\n")
	m, err := New(dir, dir, FromInspect)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ id, want string }{{"reported", "$0.20"}, {"estimated", "~$0.98"}, {"unknown", "?"}} {
		n := childByID(m.tree.Root, tt.id)
		if n == nil {
			t.Fatalf("missing %s: %s", tt.id, m.loadErr)
		}
		metrics := aggregateSummaryMetrics(n, time.Time{})
		if got := formatSummaryCost(&metrics); got != tt.want {
			t.Fatalf("%s cost %q", tt.id, got)
		}
		m.setSelected(n)
		detail := m.selectedStepDetailText()
		if tt.id == "estimated" && !strings.Contains(detail, "~$0.98 (API-equivalent estimate)") {
			t.Fatal(detail)
		}
		if tt.id == "unknown" && !strings.Contains(detail, "? (model-unpriced)") {
			t.Fatal(detail)
		}
		if tt.id == "reported" && n.Attempts[0].CostBasis != "reported" {
			t.Fatal("legacy basis")
		}
	}
	for _, tt := range []struct{ id, want string }{{"loop", "~$1.18"}, {"nested", "~$0.98 (partial)"}} {
		n := childByID(m.tree.Root, tt.id)
		row := makeSummaryRow(n, false, time.Time{})
		if row.cost != tt.want {
			t.Fatalf("container %s cost %s", tt.id, row.cost)
		}
		if tt.id == "loop" {
			m.navigateToNode(n.Children[0].Children[0])
			if !strings.Contains(m.renderSummary(), "~$0.98") {
				t.Fatal("drilled loop missing estimated row")
			}
		}
	}
	metrics := aggregateSummaryMetrics(m.tree.Root, time.Time{})
	if got := formatSummaryCost(&metrics); got != "~$3.34 (partial)" {
		t.Fatal(got)
	}
	totals := summaryTotalsFromMetrics(&metrics)
	if got := formatCoveredCost(totals.EstimatedAPICostUSD, model.CoveragePartial, totals.CostBasis); got != "~$3.34 (partial)" {
		t.Fatal(got)
	}
	if !strings.Contains(m.renderLegend(), "estimated API-equivalent cost") {
		t.Fatal("legend missing")
	}
}
