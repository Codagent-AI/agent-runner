package builtinworkflows

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ciFixture() map[string]any {
	page := func() map[string]any {
		return map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false, "hasPreviousPage": false}}
	}
	return map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{
		"url": "https://github.com/example/project/pull/12", "headRefOid": "abcdef123456", "baseRefOid": "bbbbbb", "mergeable": "MERGEABLE",
		"author": map[string]any{"login": "alice", "__typename": "User"}, "headRef": map[string]any{"target": map[string]any{"checkSuites": page(), "statusCheckRollup": map[string]any{"contexts": page()}}},
		"timelineItems": page(), "reviews": page(), "reviewThreads": page(), "comments": page(),
	}}}}
}

func ciPR(f map[string]any) map[string]any {
	return f["data"].(map[string]any)["repository"].(map[string]any)["pullRequest"].(map[string]any)
}

// ciConn returns a top-level pull-request connection such as "reviews".
func ciConn(pr map[string]any, field string) map[string]any {
	return pr[field].(map[string]any)
}

func ciCommit(pr map[string]any) map[string]any {
	return pr["headRef"].(map[string]any)["target"].(map[string]any)
}

func ciSuites(pr map[string]any) map[string]any {
	return ciCommit(pr)["checkSuites"].(map[string]any)
}

func ciChecks(pr map[string]any) map[string]any {
	return ciCommit(pr)["statusCheckRollup"].(map[string]any)["contexts"].(map[string]any)
}

// useCISequence makes the fake gh serve one snapshot per GraphQL call, repeating the last.
func useCISequence(t *testing.T, fixtures ...map[string]any) {
	t.Helper()
	dir := t.TempDir()
	for i, fixture := range fixtures {
		data, err := json.Marshal(fixture)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", i+1)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "count"), []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CI_SEQUENCE_DIR", dir)
}

func writeCIPage(t *testing.T, page string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "page.json")
	if err := os.WriteFile(path, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runCIFixture(t *testing.T, fixture map[string]any, inputs string) (output string, exitCode int, elapsed time.Duration) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"ci-wait.sh", "ci_wait.py"} {
		data, err := ReadAsset("core/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	stub := `#!/bin/sh
if [ "$CI_FAKE_GH_MODE" = hang ]; then sleep 10; fi
if [ "$CI_FAKE_GH_MODE" = no_pr ] && [ "$1" = pr ]; then echo 'no pull requests found' >&2; exit 1; fi
if [ "$CI_FAKE_GH_MODE" = auth ] && [ "$1" = api ]; then echo 'HTTP 401 Bad credentials' >&2; exit 1; fi
if [ "$CI_FAKE_GH_MODE" = unreadable ] && [ "$1" = api ]; then echo 'temporary error' >&2; exit 1; fi
case "$*" in
  "pr view --json number,url") echo '{"number":12,"url":"https://github.com/example/project/pull/12"}' ;;
  "api graphql"*)
    if [ -n "$CI_SEQUENCE_DIR" ]; then
      n=$(cat "$CI_SEQUENCE_DIR/count")
      n=$((n + 1))
      echo "$n" > "$CI_SEQUENCE_DIR/count"
      if [ ! -f "$CI_SEQUENCE_DIR/$n.json" ]; then n=$(ls "$CI_SEQUENCE_DIR" | grep -c '\.json$'); fi
      cat "$CI_SEQUENCE_DIR/$n.json"
      exit 0
    fi
    case "$*" in
      *'after: $cursor'*)
        if [ "$CI_PAGE" = fail ]; then echo 'temporary page error' >&2; exit 1; fi
        if [ -n "$CI_PAGE" ]; then cat "$CI_PAGE"; else cat "$CI_SNAPSHOT"; fi ;;
      *) cat "$CI_SNAPSHOT" ;;
    esac ;;
  "run view"*) echo 'failed job log excerpt' ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", filepath.Join(dir, "ci-wait.sh"))
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "CI_SNAPSHOT="+filepath.Join(dir, "snapshot.json"))
	cmd.Stdin = strings.NewReader(inputs)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return string(out), code, time.Since(start)
}

func TestCIWaitClassification(t *testing.T) {
	past := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	fresh := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	bot := map[string]any{"login": "coderabbitai[bot]", "__typename": "Bot"}
	human := map[string]any{"login": "reviewer", "__typename": "User"}
	tests := []struct {
		name     string
		setup    func(map[string]any)
		inputs   string
		want     string
		contains string
	}{
		{"green snapshot", func(map[string]any) {}, "", "CI_PASSED", "**PR:**"},
		{"failed check and log", func(pr map[string]any) {
			ciChecks(pr)["nodes"] = []any{map[string]any{"name": "unit tests", "conclusion": "FAILURE", "detailsUrl": "https://github.com/example/project/actions/runs/123"}}
		}, "", "CI_FAILED", "failed job log excerpt"},
		{"merge conflict", func(pr map[string]any) { pr["mergeable"] = "CONFLICTING" }, "", "CI_FAILED", "Merge Conflicts"},
		{"blocking review", func(pr map[string]any) {
			ciConn(pr, "reviews")["nodes"] = []any{map[string]any{"author": human, "state": "CHANGES_REQUESTED", "body": "fix this"}}
		}, "", "CI_FAILED", "Blocking Reviews"},
		{"actionable thread", func(pr map[string]any) {
			ciConn(pr, "reviewThreads")["nodes"] = []any{map[string]any{"isResolved": false, "comments": map[string]any{"nodes": []any{map[string]any{"author": human, "body": "fix", "path": "a.go", "line": 3}}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		}, "", "CI_COMMENTS", "a.go:3"},
		{"comment outranks pending CI", func(pr map[string]any) {
			ciChecks(pr)["nodes"] = []any{map[string]any{"name": "build", "status": "IN_PROGRESS"}}
			ciConn(pr, "reviewThreads")["nodes"] = []any{map[string]any{"isResolved": false, "comments": map[string]any{"nodes": []any{map[string]any{"author": human, "body": "fix", "path": "a.go", "line": 3}}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		}, "", "CI_COMMENTS", "Still Running"},
		{"deferred thread", func(pr map[string]any) {
			ciConn(pr, "reviewThreads")["nodes"] = []any{map[string]any{"isResolved": false, "comments": map[string]any{"nodes": []any{map[string]any{"author": human, "body": "fix", "path": "a.go", "line": 3}, map[string]any{"author": map[string]any{"login": "alice", "__typename": "User"}, "body": "deferred"}}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		}, "", "CI_PASSED", "Deferred Threads"},
		{"human reply reopens", func(pr map[string]any) {
			ciConn(pr, "reviewThreads")["nodes"] = []any{map[string]any{"isResolved": false, "comments": map[string]any{"nodes": []any{map[string]any{"author": human, "body": "fix", "path": "a.go", "line": 3}, map[string]any{"author": map[string]any{"login": "alice", "__typename": "User"}, "body": "deferred"}, map[string]any{"author": human, "body": "still broken"}}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		}, "", "CI_COMMENTS", "PR Comments"},
		{"bot summary", func(pr map[string]any) {
			ciConn(pr, "comments")["nodes"] = []any{map[string]any{"author": bot, "body": "summary"}}
		}, "", "CI_PASSED", "Informational Bot Comments"},
		{"pending check", func(pr map[string]any) {
			ciChecks(pr)["nodes"] = []any{map[string]any{"name": "build", "status": "IN_PROGRESS"}}
		}, "", "CI_PENDING", "Still Running"},
		{"unknown mergeability", func(pr map[string]any) { pr["mergeable"] = "UNKNOWN" }, "", "CI_PENDING", "pending"},
		{"configured bot absent", func(pr map[string]any) {}, `"review_bots":"CodeRabbitAI[bot]",`, "CI_REVIEW_INCOMPLETE", "coderabbitai: not started"},
		{"old bot review", func(pr map[string]any) {
			ciConn(pr, "reviews")["nodes"] = []any{map[string]any{"author": bot, "state": "APPROVED", "submittedAt": past, "commit": map[string]any{"oid": "old-head"}}}
		}, "", "CI_REVIEW_INCOMPLETE", "Unfinished Review Bots"},
		{"fresh bot success", func(pr map[string]any) {
			ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": past}}
			ciChecks(pr)["nodes"] = []any{map[string]any{"context": "CodeRabbit", "state": "SUCCESS", "createdAt": fresh, "creator": map[string]any{"login": "coderabbitai[bot]"}}}
		}, `"review_bots":"coderabbitai",`, "CI_PASSED", "passed"},
		{"pending bot status", func(pr map[string]any) {
			ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": past}}
			ciChecks(pr)["nodes"] = []any{map[string]any{"context": "CodeRabbit", "state": "PENDING", "createdAt": fresh, "creator": map[string]any{"login": "coderabbitai[bot]"}}}
		}, `"review_bots":"coderabbitai",`, "CI_REVIEW_INCOMPLETE", "coderabbitai: pending"},
		{"first-time bot pending", func(pr map[string]any) {
			ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": past}}
			ciChecks(pr)["nodes"] = []any{map[string]any{"context": "CodeRabbit", "state": "PENDING", "createdAt": fresh, "creator": map[string]any{"login": "coderabbitai[bot]"}}}
		}, "", "CI_REVIEW_INCOMPLETE", "coderabbitai: pending"},
		{"first-time bot failure stays CI", func(pr map[string]any) {
			ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": past}}
			ciChecks(pr)["nodes"] = []any{map[string]any{"context": "codecov/patch", "state": "FAILURE", "createdAt": fresh, "creator": map[string]any{"login": "codecov[bot]"}}}
		}, "", "CI_FAILED", "codecov/patch"},
		{"skipped bot check", func(pr map[string]any) {
			ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": past}}
			ciChecks(pr)["nodes"] = []any{map[string]any{"name": "CodeRabbit", "conclusion": "SKIPPED", "completedAt": fresh, "checkSuite": map[string]any{"app": map[string]any{"slug": "coderabbitai"}}}}
		}, `"review_bots":"coderabbitai",`, "CI_REVIEW_INCOMPLETE", "coderabbitai: skipped"},
		{"bot comment does not finish", func(pr map[string]any) {
			ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": past}}
			ciConn(pr, "comments")["nodes"] = []any{map[string]any{"author": bot, "body": "review complete", "updatedAt": fresh}}
		}, `"review_bots":"coderabbitai",`, "CI_REVIEW_INCOMPLETE", "coderabbitai: commented"},
		{"stale after ready", func(pr map[string]any) {
			ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": past}}
			ciConn(pr, "timelineItems")["nodes"] = []any{map[string]any{"createdAt": fresh}}
			ciChecks(pr)["nodes"] = []any{map[string]any{"context": "CodeRabbit", "state": "SUCCESS", "createdAt": past, "creator": map[string]any{"login": "coderabbitai[bot]"}}}
		}, `"review_bots":"coderabbitai",`, "CI_REVIEW_INCOMPLETE", "not started"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := ciFixture()
			tt.setup(ciPR(fixture))
			input := fmt.Sprintf(`{%s"deadline_seconds":"0.7","poll_interval_seconds":"0.05","bot_start_grace_seconds":"0.15","call_timeout_seconds":"0.3"}`, tt.inputs)
			out, code, elapsed := runCIFixture(t, fixture, input)
			if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), tt.want) || !strings.Contains(out, tt.contains) {
				t.Fatalf("code=%d elapsed=%s output=\n%s", code, elapsed, out)
			}
			if elapsed > 2*time.Second {
				t.Fatalf("wait took %s", elapsed)
			}
		})
	}
}

// INT-004 uses a recorded, body-redacted response from public PR 194. The
// primary and continuation queries were also checked against GitHub's schema
// during implementation; this fixture keeps their response shape offline.
func TestCIWaitRecordedGraphQLFixture(t *testing.T) {
	data, err := os.ReadFile("../testdata/ci_wait_graphql_pr.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	pr := ciPR(fixture)
	for _, conn := range []map[string]any{
		ciConn(pr, "reviews"), ciConn(pr, "reviewThreads"), ciConn(pr, "comments"),
		ciConn(pr, "timelineItems"), ciSuites(pr), ciChecks(pr),
	} {
		if _, ok := conn["pageInfo"].(map[string]any); !ok {
			t.Fatal("connection missing pageInfo")
		}
	}
	out, code, _ := runCIFixture(t, fixture, `{"deadline_seconds":"0.3","poll_interval_seconds":"0.05","bot_start_grace_seconds":"0.1"}`)
	if code != 0 || strings.Contains(out, "snapshot:") || !strings.Contains(out, "**PR:**") {
		t.Fatalf("recorded fixture: code=%d\n%s", code, out)
	}
}

func TestCIWaitFatalAndBoundedCalls(t *testing.T) {
	for _, tt := range []struct {
		name, mode, want string
		code             int
	}{
		{"missing PR", "no_pr", "no open pull request", 1},
		{"authentication", "auth", "authentication failed", 1},
		{"hung gh", "hang", "CI_PENDING", 0},
		{"unreadable snapshot", "unreadable", "CI_PENDING", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CI_FAKE_GH_MODE", tt.mode)
			out, code, elapsed := runCIFixture(t, ciFixture(), `{"deadline_seconds":"0.7","poll_interval_seconds":"0.05","bot_start_grace_seconds":"0.1","call_timeout_seconds":"0.2"}`)
			if code != tt.code || !strings.Contains(out, tt.want) || elapsed > time.Second {
				t.Fatalf("code=%d elapsed=%s output=%s", code, elapsed, out)
			}
		})
	}
}

func TestCIWaitReadsContinuationPages(t *testing.T) {
	for _, tt := range []struct{ name, field, page, want, detail string }{
		{"failed check on page two", "checks", `{"data":{"repository":{"pullRequest":{"headRef":{"target":{"statusCheckRollup":{"contexts":{"nodes":[{"name":"late failure","conclusion":"FAILURE"}],"pageInfo":{"hasNextPage":false}}}}}}}}}`, "CI_FAILED", "late failure"},
		{"actionable thread on page two", "threads", `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"id":"thread2","isResolved":false,"comments":{"nodes":[{"author":{"login":"reviewer","__typename":"User"},"body":"later issue","path":"late.go","line":9}],"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}}}}}}`, "CI_COMMENTS", "later issue"},
		{"unread later page", "threads", "fail", "CI_PENDING", "pending"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := ciFixture()
			pr := ciPR(fixture)
			var conn map[string]any
			if tt.field == "checks" {
				conn = ciChecks(pr)
			} else {
				conn = ciConn(pr, "reviewThreads")
			}
			conn["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "cursor1"}
			if tt.page == "fail" {
				t.Setenv("CI_PAGE", "fail")
			} else {
				t.Setenv("CI_PAGE", writeCIPage(t, tt.page))
			}
			out, code, _ := runCIFixture(t, fixture, `{"deadline_seconds":"0.5","poll_interval_seconds":"0.05","bot_start_grace_seconds":"0.1"}`)
			if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), tt.want) || !strings.Contains(out, tt.detail) {
				t.Fatalf("code=%d report=%s", code, out)
			}
		})
	}
}

func TestCIWaitPaginatesThreadRepliesBeforeDeferring(t *testing.T) {
	fixture := ciFixture()
	pr := ciPR(fixture)
	ciConn(pr, "reviewThreads")["nodes"] = []any{map[string]any{
		"id": "thread1", "isResolved": false, "comments": map[string]any{"nodes": []any{
			map[string]any{"author": map[string]any{"login": "reviewer", "__typename": "User"}, "body": "fix", "path": "a.go", "line": 3},
		}, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": "cursor1"}},
	}}
	page := `{"data":{"node":{"comments":{"nodes":[{"author":{"login":"alice","__typename":"User"},"body":"deferred"}],"pageInfo":{"hasNextPage":false}}}}}`
	t.Setenv("CI_PAGE", writeCIPage(t, page))
	out, code, _ := runCIFixture(t, fixture, `{"deadline_seconds":"0.5","poll_interval_seconds":"0.05","bot_start_grace_seconds":"0.1"}`)
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "CI_PASSED") || !strings.Contains(out, "Deferred Threads") {
		t.Fatalf("code=%d report=%s", code, out)
	}
}

func TestCIWaitRechecksBotWhenHeadChanges(t *testing.T) {
	past := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	fresh := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	first := ciFixture()
	firstPR := ciPR(first)
	ciSuites(firstPR)["nodes"] = []any{map[string]any{"createdAt": past}}
	ciChecks(firstPR)["nodes"] = []any{
		map[string]any{"name": "build", "status": "IN_PROGRESS"},
		map[string]any{"context": "CodeRabbit", "state": "SUCCESS", "createdAt": fresh, "creator": map[string]any{"login": "coderabbitai[bot]"}},
	}
	second := ciFixture()
	ciPR(second)["headRefOid"] = "newhead123456"
	useCISequence(t, first, second)
	out, code, _ := runCIFixture(t, first, `{"review_bots":"coderabbitai","deadline_seconds":"0.8","poll_interval_seconds":"0.05","bot_start_grace_seconds":"0.1"}`)
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "CI_REVIEW_INCOMPLETE") || !strings.Contains(out, "**Head:** newhead") {
		t.Fatalf("code=%d report=%s", code, out)
	}
}

func TestCIWaitBotPendingThenSuccess(t *testing.T) {
	past := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	fresh := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	makeSnapshot := func(state string) map[string]any {
		fixture := ciFixture()
		pr := ciPR(fixture)
		ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": past}}
		ciChecks(pr)["nodes"] = []any{map[string]any{"context": "CodeRabbit", "state": state, "createdAt": fresh, "creator": map[string]any{"login": "coderabbitai[bot]"}}}
		return fixture
	}
	first, second := makeSnapshot("PENDING"), makeSnapshot("SUCCESS")
	useCISequence(t, first, second)
	out, code, _ := runCIFixture(t, first, `{"review_bots":"coderabbitai","deadline_seconds":"0.8","poll_interval_seconds":"0.05","bot_start_grace_seconds":"0.1"}`)
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "CI_PASSED") {
		t.Fatalf("code=%d report=%s", code, out)
	}
}

func TestCIWaitAddressedFeedbackAndLatestBotEvidence(t *testing.T) {
	push := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	old := time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339)
	success := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	pending := time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339)
	bot := map[string]any{"login": "coderabbitai[bot]"}
	for _, tt := range []struct {
		name           string
		setup          func(map[string]any)
		inputs, marker string
	}{
		{"completed current-head status without push timestamp", func(pr map[string]any) {
			ciChecks(pr)["nodes"] = []any{map[string]any{"context": "CodeRabbit", "state": "SUCCESS", "createdAt": success, "creator": bot}}
		}, `"review_bots":"coderabbitai",`, "CI_PASSED"},
		{"human comment addressed by push", func(pr map[string]any) {
			ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": push}}
			ciConn(pr, "comments")["nodes"] = []any{map[string]any{"author": map[string]any{"login": "reviewer", "__typename": "User"}, "body": "fixed already", "updatedAt": old}}
		}, "", "CI_PASSED"},
		{"human comment addressed by author reply", func(pr map[string]any) {
			ciConn(pr, "comments")["nodes"] = []any{
				map[string]any{"author": map[string]any{"login": "reviewer", "__typename": "User"}, "body": "please fix", "updatedAt": old},
				map[string]any{"author": map[string]any{"login": "alice", "__typename": "User"}, "body": "fixed", "updatedAt": success},
			}
		}, "", "CI_PASSED"},
		{"newer pending status supersedes success", func(pr map[string]any) {
			ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": push}}
			ciChecks(pr)["nodes"] = []any{
				map[string]any{"context": "CodeRabbit", "state": "SUCCESS", "createdAt": success, "creator": bot},
				map[string]any{"context": "CodeRabbit", "state": "PENDING", "createdAt": pending, "creator": bot},
			}
		}, `"review_bots":"coderabbitai",`, "CI_REVIEW_INCOMPLETE"},
		{"old-head review does not supersede current-head success", func(pr map[string]any) {
			ciSuites(pr)["nodes"] = []any{map[string]any{"createdAt": push}}
			ciChecks(pr)["nodes"] = []any{
				map[string]any{"context": "CodeRabbit", "state": "SUCCESS", "createdAt": success, "creator": bot},
			}
			ciConn(pr, "reviews")["nodes"] = []any{map[string]any{"author": map[string]any{"login": "coderabbitai[bot]", "__typename": "Bot"}, "state": "COMMENTED", "submittedAt": pending, "commit": map[string]any{"oid": "old-head"}}}
		}, `"review_bots":"coderabbitai",`, "CI_PASSED"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := ciFixture()
			tt.setup(ciPR(fixture))
			input := fmt.Sprintf(`{%s"deadline_seconds":"0.8","poll_interval_seconds":"0.05","bot_start_grace_seconds":"0.1"}`, tt.inputs)
			out, code, _ := runCIFixture(t, fixture, input)
			if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), tt.marker) {
				t.Fatalf("code=%d report=%s", code, out)
			}
		})
	}
}

// The start grace, not the poll interval, decides when an idle wait may finish.
func TestCIWaitStopsAtGraceEndNotNextPoll(t *testing.T) {
	for _, tt := range []struct{ name, inputs, want string }{
		{"no expected bot", "", "CI_PASSED"},
		{"expected bot never starts", `"review_bots":"coderabbitai",`, "CI_REVIEW_INCOMPLETE"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := fmt.Sprintf(`{%s"deadline_seconds":"3","poll_interval_seconds":"1","bot_start_grace_seconds":"0.1"}`, tt.inputs)
			out, code, elapsed := runCIFixture(t, ciFixture(), input)
			if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), tt.want) || elapsed > 700*time.Millisecond {
				t.Fatalf("code=%d elapsed=%s output=%s", code, elapsed, out)
			}
		})
	}
}
