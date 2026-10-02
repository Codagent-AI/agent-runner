"""Bounded, agent-free PR CI and review collector for core:finalize-pr."""
import json
import os
import re
import signal
import subprocess
import sys
import time
from datetime import datetime, timezone
from urllib.parse import urlparse

PAGE = "pageInfo { hasNextPage endCursor hasPreviousPage startCursor }"
AUTHOR = "author { login __typename }"
COMMENT = f"{AUTHOR} body updatedAt path line originalLine"
TOP_COMMENT = f"{AUTHOR} body updatedAt"
REVIEW = f"{AUTHOR} state submittedAt commit {{ oid }} body"
TIMELINE_TYPES = "itemTypes: [READY_FOR_REVIEW_EVENT, HEAD_REF_FORCE_PUSHED_EVENT]"
TIMELINE = "... on ReadyForReviewEvent { createdAt } ... on HeadRefForcePushedEvent { createdAt afterCommit { oid } }"
FAILED_STATES = {"FAILURE", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE", "ERROR"}
PASSED_STATES = {"SUCCESS", "SKIPPED", "NEUTRAL"}
TERMINAL_CHECK_STATES = FAILED_STATES | PASSED_STATES | {"STALE"}
DEFAULT_TIMINGS = {"deadline_seconds": 900, "poll_interval_seconds": 15,
                   "bot_start_grace_seconds": 180, "call_timeout_seconds": 30}
LOG_MARGIN_SECONDS = 60
LOG_TAIL_LINES = 100
BOT_COMMENT_CHARS = 500
THREAD = f"id isResolved comments(first: 100) {{ nodes {{ {COMMENT} }} {PAGE} }}"
CHECK = """... on CheckRun { name status conclusion startedAt completedAt detailsUrl
  checkSuite { app { slug } } }
... on StatusContext { context state targetUrl creator { login } createdAt }"""
QUERY = f"""query($owner: String!, $repo: String!, $number: Int!) {{
 repository(owner: $owner, name: $repo) {{ pullRequest(number: $number) {{
  url headRefOid baseRefOid mergeable isDraft {AUTHOR}
  headRef {{ target {{ ... on Commit {{
   checkSuites(first: 100) {{ nodes {{ createdAt }} {PAGE} }}
   statusCheckRollup {{ contexts(first: 100) {{ nodes {{ {CHECK} }} {PAGE} }} }}
  }} }} }}
  timelineItems({TIMELINE_TYPES}, last: 100) {{ nodes {{ {TIMELINE} }} {PAGE} }}
  reviews(last: 100) {{ nodes {{ {REVIEW} }} {PAGE} }}
  reviewThreads(first: 100) {{ nodes {{ {THREAD} }} {PAGE} }}
  comments(last: 100) {{ nodes {{ {TOP_COMMENT} }} {PAGE} }}
 }} }} }}"""
THREAD_COMMENTS_PAGE = ("query($id: ID!, $cursor: String!) { node(id: $id) { ... on PullRequestReviewThread { "
                        f"comments(first: 100, after: $cursor) {{ nodes {{ {COMMENT} }} {PAGE} }}" " } } }")


def pr_page_query(fragment):
    """Continuation query for one paginated connection under the pull request."""
    return ("query($owner: String!, $repo: String!, $number: Int!, $cursor: String!) "
            "{ repository(owner: $owner, name: $repo) { pullRequest(number: $number) { " + fragment + " } } }")


def head_commit(fragment):
    return f"headRef {{ target {{ ... on Commit {{ {fragment} }} }} }}"

class Fatal(Exception):
    pass

class Transient(Exception):
    pass


def stamp(value):
    if not value:
        return 0.0
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()
    except (ValueError, TypeError):
        return 0.0


def identity(value):
    return re.sub(r"\[bot\]$", "", (value or "").strip().lower())


def actor(item):
    return item.get("author") or {}


def now():
    return time.time()


class Collector:
    def __init__(self, inputs):
        # Stdin keys win; the environment override exists for workflow-level tests.
        timings = {**DEFAULT_TIMINGS, **json.loads(os.environ.get("AGENT_RUNNER_CI_WAIT_TIMINGS", "{}"))}
        timings = {key: float(inputs.get(key, value)) for key, value in timings.items() if key in DEFAULT_TIMINGS}
        self.deadline_seconds = timings["deadline_seconds"]
        self.interval = timings["poll_interval_seconds"]
        self.grace = timings["bot_start_grace_seconds"]
        self.call_timeout = timings["call_timeout_seconds"]
        self.started = now()
        self.deadline = self.started + self.deadline_seconds
        self.configured = {identity(x) for x in inputs.get("review_bots", "").split(",") if identity(x)}
        self.first_seen = {}
        self.discovered = set()
        self.head = None
        self.snapshot = None
        self.complete = False
        self.url = ""

    def run(self, args, margin=0, allow_failure=False):
        remaining = self.deadline + margin - now()
        if remaining <= 0:
            raise Transient("deadline reached")
        timeout = min(self.call_timeout, remaining)
        process = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   text=True, start_new_session=True)
        try:
            stdout, stderr = process.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            except PermissionError:
                if process.poll() is None:
                    process.kill()
            process.communicate()
            raise Transient("timed out: " + " ".join(args[:3]))
        if process.returncode:
            if allow_failure:
                return stdout
            message = (stderr or stdout).strip()
            if any(s in message.lower() for s in ("gh auth login", "http 401", "bad credentials", "authentication failed")):
                raise Fatal("GitHub authentication failed: " + message)
            raise Transient(message or "command failed: " + " ".join(args[:3]))
        return stdout

    def graphql(self, query, extra=None):
        variables = {"owner": self.owner, "repo": self.repo, "number": str(self.number)}
        variables.update(extra or {})
        args = ["gh", "api", "graphql", "-f", "query=" + query]
        for key, value in variables.items():
            args += ["-F" if key == "number" else "-f", key + "=" + str(value)]
        try:
            result = json.loads(self.run(args))
        except ValueError as exc:
            raise Transient("invalid GraphQL JSON: " + str(exc)) from exc
        if result.get("errors"):
            raise Transient("GraphQL: " + str(result["errors"]))
        return result.get("data") or {}

    def resolve(self):
        while now() < self.deadline:
            try:
                result = json.loads(self.run(["gh", "pr", "view", "--json", "number,url"]))
                self.number = result["number"]
                self.url = result["url"]
                parts = urlparse(self.url).path.strip("/").split("/")
                self.owner, self.repo = parts[0], parts[1]
                return
            except (Transient, ValueError, KeyError, IndexError) as exc:
                if "no pull requests" in str(exc).lower() or "could not find a pull request" in str(exc).lower():
                    raise Fatal("no open pull request for current branch") from exc
                print("ci-wait: PR lookup: " + str(exc), file=sys.stderr)
                time.sleep(min(self.interval, max(0, self.deadline - now())))
        raise Transient("PR lookup deadline reached")

    def more(self, pr, field, query, path, reverse=False, extra=None):
        connection = pr.get(field) or {"nodes": [], "pageInfo": {}}
        nodes = list(connection.get("nodes") or [])
        page = connection.get("pageInfo") or {}
        direction = "hasPreviousPage" if reverse else "hasNextPage"
        cursor_name = "startCursor" if reverse else "endCursor"
        seen = set()
        while page.get(direction):
            cursor = page.get(cursor_name)
            if not cursor or cursor in seen:
                raise Transient("invalid pagination cursor for " + field)
            seen.add(cursor)
            data = self.graphql(query, {"cursor": cursor, **(extra or {})})
            conn = data
            for key in path:
                conn = (conn or {}).get(key)
            if not isinstance(conn, dict):
                raise Transient("missing page for " + field)
            chunk = conn.get("nodes") or []
            nodes = chunk + nodes if reverse else nodes + chunk
            page = conn.get("pageInfo") or {}
        return nodes

    def read_snapshot(self):
        data = self.graphql(QUERY)
        pr = ((data.get("repository") or {}).get("pullRequest"))
        if not pr:
            raise Fatal("no open pull request for current branch")
        head = pr.get("headRefOid")
        if not head:
            raise Transient("PR head unavailable")
        if head != self.head:
            self.head = head
            self.first_seen[head] = now()
            self.discovered.clear()
        self.snapshot = pr
        self.complete = False
        commit = (((pr.get("headRef") or {}).get("target")) or {})
        def connection(field, selection, reverse=False, filters=""):
            side = "last: 100, before: $cursor" if reverse else "first: 100, after: $cursor"
            query = pr_page_query(f"{field}({filters}{side}) {{ nodes {{ {selection} }} {PAGE} }}")
            return self.more(pr, field, query, ["repository", "pullRequest", field], reverse)
        pr["reviews"]["nodes"] = connection("reviews", REVIEW, True)
        pr["comments"]["nodes"] = connection("comments", TOP_COMMENT, True)
        pr["timelineItems"]["nodes"] = connection("timelineItems", TIMELINE, True, TIMELINE_TYPES + ", ")
        pr["reviewThreads"]["nodes"] = connection("reviewThreads", THREAD)
        for thread in pr["reviewThreads"]["nodes"]:
            if thread.get("comments"):
                thread["comments"]["nodes"] = self.more(thread, "comments", THREAD_COMMENTS_PAGE, ["node", "comments"],
                                                        extra={"id": thread.get("id")})
        if commit.get("checkSuites"):
            query = pr_page_query(head_commit(f"checkSuites(first: 100, after: $cursor) {{ nodes {{ createdAt }} {PAGE} }}"))
            commit["checkSuites"]["nodes"] = self.more(commit, "checkSuites", query,
                ["repository", "pullRequest", "headRef", "target", "checkSuites"])
        rollup = commit.get("statusCheckRollup") or {}
        if rollup.get("contexts"):
            query = pr_page_query(head_commit(
                f"statusCheckRollup {{ contexts(first: 100, after: $cursor) {{ nodes {{ {CHECK} }} {PAGE} }} }}"))
            rollup["contexts"]["nodes"] = self.more(rollup, "contexts", query,
                ["repository", "pullRequest", "headRef", "target", "statusCheckRollup", "contexts"])
        self.complete = True
        return pr

    def classify(self, pr):
        commit = (((pr.get("headRef") or {}).get("target")) or {})
        checks = (((commit.get("statusCheckRollup") or {}).get("contexts")) or {}).get("nodes") or []
        suites = ((commit.get("checkSuites") or {}).get("nodes")) or []
        reviews = ((pr.get("reviews") or {}).get("nodes")) or []
        comments = ((pr.get("comments") or {}).get("nodes")) or []
        threads = ((pr.get("reviewThreads") or {}).get("nodes")) or []
        timeline = ((pr.get("timelineItems") or {}).get("nodes")) or []
        head = pr.get("headRefOid") or ""
        push = min((stamp(s.get("createdAt")) for s in suites if stamp(s.get("createdAt"))), default=0)
        pushes = [stamp(e.get("createdAt")) for e in timeline if (e.get("afterCommit") or {}).get("oid") == head]
        observed_push = max(push, max(pushes, default=0))
        ready = max((stamp(e.get("createdAt")) for e in timeline if "afterCommit" not in e), default=0)
        # The observation time bounds the start grace, but is not evidence that
        # a current-head status posted before this process started is stale.
        fresh = max(observed_push, ready)
        grace_end = min(self.deadline, max(fresh, self.first_seen.get(head, self.started)) + self.grace)
        expected = set(self.configured)
        prior_reviewers = set()
        for review in reviews:
            if actor(review).get("__typename") == "Bot" and review.get("submittedAt"):
                prior_reviewers.add(identity(actor(review).get("login")))
        expected |= prior_reviewers
        if now() <= grace_end:
            for check in checks:
                name, state, timestamp = self.check_fields(check)
                creator = self.check_creator(check)
                # Only a pending check or status can reveal a first-time reviewer;
                # a terminal result from an unexpected bot stays ordinary CI.
                if state in TERMINAL_CHECK_STATES:
                    continue
                if creator and (creator.lower().endswith("[bot]") or name.lower().startswith("coderabbit")) and timestamp > fresh:
                    self.discovered.add(identity(creator))
        expected |= self.discovered
        grace_only = self.discovered - self.configured - prior_reviewers
        failed, pending, passed = [], [], []
        bot_progress = {bot: [] for bot in expected}
        bot_activity = {bot: [] for bot in expected}
        for check in checks:
            name, state, timestamp = self.check_fields(check)
            bot = identity(self.check_creator(check))
            if bot in expected and not (bot in grace_only and state in FAILED_STATES):
                if timestamp > fresh:
                    bot_progress[bot].append((state == "SUCCESS", timestamp, state))
                continue
            entry = {"name": name, "state": state, "link": check.get("detailsUrl") or check.get("targetUrl") or ""}
            if state in FAILED_STATES:
                failed.append(entry)
            elif state in PASSED_STATES:
                passed.append(entry)
            else:
                pending.append(entry)
        for review in reviews:
            bot = identity(actor(review).get("login"))
            if bot in expected and stamp(review.get("submittedAt")) > fresh:
                submitted = stamp(review.get("submittedAt"))
                if (review.get("commit") or {}).get("oid") == head:
                    bot_progress[bot].append((True, submitted, review.get("state") or "review"))
                else:
                    bot_activity[bot].append((submitted, "reviewed earlier head"))
        for comment in comments:
            bot = identity(actor(comment).get("login"))
            if bot in expected and stamp(comment.get("updatedAt")) > fresh:
                bot_activity[bot].append((stamp(comment.get("updatedAt")), "commented"))
        unfinished = []
        for bot in sorted(expected):
            progress = bot_progress[bot]
            latest = max(progress, key=lambda x: (x[1], not x[0])) if progress else None
            if latest and latest[0]:
                continue
            activity = max(bot_activity[bot], default=None)
            status = latest[2].lower() if latest else (activity[1] if activity else "not started")
            unfinished.append((bot, status))
        latest_reviews = {}
        for review in reviews:
            if review.get("state") != "PENDING":
                latest_reviews[identity(actor(review).get("login"))] = review
        blocking = [r for r in latest_reviews.values() if r.get("state") == "CHANGES_REQUESTED"]
        author_login = (pr.get("author") or {}).get("login") or ""
        actionable, deferred = [], []
        for thread in threads:
            if thread.get("isResolved"):
                continue
            replies = ((thread.get("comments") or {}).get("nodes")) or []
            if not replies:
                continue
            first = replies[0]
            finding = actor(first)
            significant = list(replies)
            while (finding.get("__typename") == "Bot" and len(significant) > 1
                   and actor(significant[-1]).get("__typename") == "Bot"
                   and actor(significant[-1]).get("login") == finding.get("login")):
                significant.pop()
            last = actor(significant[-1])
            item = {"file": first.get("path") or "", "line": first.get("line") or first.get("originalLine"),
                    "author": finding.get("login") or "unknown", "body": first.get("body") or ""}
            is_deferred = ((last.get("__typename") != "Bot" and author_login and last.get("login") == author_login)
                           or (last.get("__typename") == "Bot" and len(significant) > 1
                               and last.get("login") != finding.get("login")))
            (deferred if is_deferred else actionable).append(item)
        human_comments, bot_comments = [], []
        for comment in comments:
            who = actor(comment)
            if author_login and who.get("login") == author_login:
                continue
            item = {"author": who.get("login") or "unknown", "body": comment.get("body") or ""}
            if who.get("__typename") == "Bot":
                bot_comments.append(item)
                continue
            comment_time = stamp(comment.get("updatedAt"))
            if comment_time and observed_push and comment_time < observed_push:
                continue
            human_comments.append(item)
        return {"pr": pr, "failed": failed, "pending": pending, "passed": passed, "blocking": blocking,
                "actionable": actionable, "deferred": deferred, "human_comments": human_comments,
                "bot_comments": bot_comments, "unfinished": unfinished, "expected": expected,
                "grace_end": grace_end}

    @staticmethod
    def check_creator(check):
        return ((check.get("creator") or {}).get("login")
                or ((check.get("checkSuite") or {}).get("app") or {}).get("slug"))

    @staticmethod
    def check_fields(check):
        if "name" in check:
            return (check.get("name") or "unnamed check", (check.get("conclusion") or check.get("status") or "").upper(),
                    stamp(check.get("completedAt") or check.get("startedAt")))
        return (check.get("context") or "unnamed status", (check.get("state") or "").upper(), stamp(check.get("createdAt")))

    def marker(self, state):
        if state["failed"] or state["blocking"] or state["pr"].get("mergeable") == "CONFLICTING":
            return "CI_FAILED"
        if state["actionable"] or state["human_comments"]:
            return "CI_COMMENTS"
        if not self.complete or state["pending"] or state["pr"].get("mergeable") != "MERGEABLE":
            return "CI_PENDING"
        if state["unfinished"]:
            return "CI_REVIEW_INCOMPLETE"
        return "CI_PASSED"

    def wait(self):
        try:
            self.resolve()
        except Transient:
            return self.report(None, "CI_PENDING")
        poll = 0
        state = None
        while True:
            if poll and now() >= self.deadline - min(self.call_timeout, 0.1):
                break
            poll += 1
            try:
                pr = self.read_snapshot()
                state = self.classify(pr)
            except (Transient, KeyError, TypeError) as exc:
                self.complete = False
                print("ci-wait: snapshot: " + str(exc), file=sys.stderr)
                if self.snapshot:
                    state = self.classify(self.snapshot)
            if state:
                marker = self.marker(state)
                grace_end = state["grace_end"]
                bot_settled = not state["unfinished"] or (
                    all(status == "not started" for _, status in state["unfinished"]) and now() >= grace_end)
                if marker == "CI_FAILED" or (self.complete and marker != "CI_PENDING" and bot_settled
                    and (state["expected"] or now() >= grace_end)):
                    break
            if now() >= self.deadline:
                break
            print(f"ci-wait: poll {poll}, {len(state['pending']) if state else '?'} checks pending, "
                  f"{max(0, int(self.deadline-now()))}s left", file=sys.stderr)
            wake = self.deadline
            if state and now() < state["grace_end"]:
                wake = state["grace_end"]
            time.sleep(min(self.interval, max(0, wake - now())))
        if not state:
            return self.report(None, "CI_PENDING")
        return self.report(state, self.marker(state))

    def report(self, state, marker):
        label = marker.removeprefix("CI_").lower().replace("_", " ")
        lines = [f"## CI Status: {label}", "", f"**PR:** {self.url}",
                 f"**Head:** {self.head or 'unknown'}",
                 f"**Elapsed:** ~{round((now()-self.started)/60, 1)} minutes"]
        if state:
            def section(title, entries):
                if entries:
                    lines.extend(["", "### " + title])
                    lines.extend("- " + entry for entry in entries)
            failed = []
            logs = {}  # jobs from one Actions run share its failed-log download
            for check in state["failed"]:
                detail = check["name"] + (" (" + check["link"] + ")" if check["link"] else "")
                match = re.search(r"/actions/runs/(\d+)", check["link"])
                if match and marker == "CI_FAILED":
                    run_id = match.group(1)
                    if run_id not in logs:
                        try:
                            log = self.run(["gh", "run", "view", run_id, "--log-failed"], margin=LOG_MARGIN_SECONDS)
                            logs[run_id] = "\n  " + "\n  ".join(log.splitlines()[-LOG_TAIL_LINES:])
                        except (Transient, Fatal) as exc:
                            logs[run_id] = " (log unavailable: " + str(exc) + ")"
                    detail += logs[run_id]
                failed.append(detail)
            section("Failed Checks", failed)
            if state["pr"].get("mergeable") == "CONFLICTING":
                conflicts = ["PR cannot merge cleanly"]
                base = state["pr"].get("baseRefOid")
                head = state["pr"].get("headRefOid")
                if base and head:
                    try:
                        paths = self.run(["git", "merge-tree", "--name-only", "--no-messages", base, head], allow_failure=True)
                        conflicts += [line for line in paths.splitlines() if line and not re.fullmatch(r"[0-9a-f]{40,64}", line)]
                    except Transient:
                        pass
                section("Merge Conflicts", conflicts)
            section("Blocking Reviews", [f"{actor(r).get('login', 'unknown')}: {r.get('body') or 'changes requested'}" for r in state["blocking"]])
            section("PR Comments", [f"{x['file']}:{x['line']} {x['author']}: {x['body']}" for x in state["actionable"]]
                    + [f"{x['author']}: {x['body']}" for x in state["human_comments"]])
            section("Deferred Threads", [f"{x['file']}:{x['line']} {x['author']}: {x['body']}" for x in state["deferred"]])
            section("Unfinished Review Bots", [f"{bot}: {status}" for bot, status in state["unfinished"]])
            section("Informational Bot Comments", [f"{x['author']}: {x['body'][:BOT_COMMENT_CHARS]}" for x in state["bot_comments"]])
            section("Still Running", [x["name"] for x in state["pending"]])
            section("Passing Checks", [x["name"] for x in state["passed"]])
        lines.extend(["", marker])
        return "\n".join(lines) + "\n"


def main():
    try:
        inputs = json.load(sys.stdin)
        if len(sys.argv) == 4 and sys.argv[1] == "--verify-reuse":
            inputs.update({"deadline_seconds": 30, "call_timeout_seconds": 10, "poll_interval_seconds": 0.1})
            collector = Collector(inputs)
            collector.resolve()
            state = collector.classify(collector.read_snapshot())
            if collector.head != sys.argv[2] or collector.marker(state) != sys.argv[3]:
                return 1
            return 0
        collector = Collector(inputs)
        sys.stdout.write(collector.wait())
    except Fatal as exc:
        print("ci-wait: " + str(exc), file=sys.stderr)
        return 1
    except (Transient, ValueError, TypeError, KeyError) as exc:
        print("ci-wait: " + str(exc), file=sys.stderr)
        return 2
    return 0

if __name__ == "__main__":
    sys.exit(main())
