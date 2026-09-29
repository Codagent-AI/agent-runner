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
  timelineItems(itemTypes: [READY_FOR_REVIEW_EVENT, HEAD_REF_FORCE_PUSHED_EVENT], last: 100) {{
   nodes {{ ... on ReadyForReviewEvent {{ createdAt }}
           ... on HeadRefForcePushedEvent {{ createdAt afterCommit {{ oid }} }} }} {PAGE} }}
  reviews(last: 100) {{ nodes {{ {AUTHOR} state submittedAt commit {{ oid }} body }} {PAGE} }}
  reviewThreads(first: 100) {{ nodes {{ {THREAD} }} {PAGE} }}
  comments(last: 100) {{ nodes {{ {TOP_COMMENT} }} {PAGE} }}
 }} }} }}"""

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
        overrides = json.loads(os.environ.get("AGENT_RUNNER_CI_WAIT_TIMINGS", "{}"))
        self.deadline_seconds = float(inputs.get("deadline_seconds", overrides.get("deadline_seconds", 900)))
        self.interval = float(inputs.get("poll_interval_seconds", overrides.get("poll_interval_seconds", 15)))
        self.grace = float(inputs.get("bot_start_grace_seconds", overrides.get("bot_start_grace_seconds", 180)))
        self.call_timeout = float(inputs.get("call_timeout_seconds", overrides.get("call_timeout_seconds", 30)))
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

    def more(self, pr, field, query, path, reverse=False):
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
            data = self.graphql(query, {"cursor": cursor})
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
            self.snapshot = None
        self.snapshot = pr
        self.complete = False
        commit = (((pr.get("headRef") or {}).get("target")) or {})
        def connection(field, selection, reverse=False):
            side = "last: 100, before: $cursor" if reverse else "first: 100, after: $cursor"
            filters = "itemTypes: [READY_FOR_REVIEW_EVENT, HEAD_REF_FORCE_PUSHED_EVENT], " if field == "timelineItems" else ""
            fragment = f"{field}({filters}{side}) {{ nodes {{ {selection} }} {PAGE} }}"
            query = ("query($owner: String!, $repo: String!, $number: Int!, $cursor: String!) "
                     "{ repository(owner: $owner, name: $repo) { pullRequest(number: $number) { "
                     + fragment + " } } }")
            return self.more(pr, field, query, ["repository", "pullRequest", field], reverse)
        pr["reviews"]["nodes"] = connection("reviews", f"{AUTHOR} state submittedAt commit {{ oid }} body", True)
        pr["comments"]["nodes"] = connection("comments", TOP_COMMENT, True)
        pr["timelineItems"]["nodes"] = connection("timelineItems", "... on ReadyForReviewEvent { createdAt } ... on HeadRefForcePushedEvent { createdAt afterCommit { oid } }", True)
        pr["reviewThreads"]["nodes"] = connection("reviewThreads", THREAD)
        for thread in pr["reviewThreads"]["nodes"]:
            conn = thread.get("comments") or {}
            page = conn.get("pageInfo") or {}
            comments = list(conn.get("nodes") or [])
            seen = set()
            while page.get("hasNextPage"):
                cursor = page.get("endCursor")
                if not cursor or cursor in seen:
                    raise Transient("invalid thread-comment cursor")
                seen.add(cursor)
                query = ("query($id: ID!, $cursor: String!) { node(id: $id) { ... on PullRequestReviewThread { "
                         f"comments(first: 100, after: $cursor) {{ nodes {{ {COMMENT} }} {PAGE} }}" " } } }")
                result = self.graphql(query, {"id": thread["id"], "cursor": cursor})
                conn = ((result.get("node") or {}).get("comments") or {})
                comments += conn.get("nodes") or []
                page = conn.get("pageInfo") or {}
            thread["comments"]["nodes"] = comments
        if commit.get("checkSuites"):
            query = ("query($owner: String!, $repo: String!, $number: Int!, $cursor: String!) "
                     "{ repository(owner: $owner, name: $repo) { pullRequest(number: $number) { "
                     f"headRef {{ target {{ ... on Commit {{ checkSuites(first: 100, after: $cursor) {{ nodes {{ createdAt }} {PAGE} }} }} }} }}" " } } }")
            commit["checkSuites"]["nodes"] = self.more(commit, "checkSuites", query,
                ["repository", "pullRequest", "headRef", "target", "checkSuites"])
        rollup = commit.get("statusCheckRollup") or {}
        if rollup.get("contexts"):
            query = ("query($owner: String!, $repo: String!, $number: Int!, $cursor: String!) "
                     "{ repository(owner: $owner, name: $repo) { pullRequest(number: $number) { "
                     f"headRef {{ target {{ ... on Commit {{ statusCheckRollup {{ contexts(first: 100, after: $cursor) {{ nodes {{ {CHECK} }} {PAGE} }} }} }} }} }}" " } } }")
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
        for review in reviews:
            if actor(review).get("__typename") == "Bot" and review.get("submittedAt"):
                expected.add(identity(actor(review).get("login")))
        expected |= self.discovered
        if now() <= grace_end:
            for check in checks:
                name, _, timestamp = self.check_fields(check)
                creator = (check.get("creator") or {}).get("login") or (((check.get("checkSuite") or {}).get("app") or {}).get("slug"))
                if creator and (creator.lower().endswith("[bot]") or name.lower().startswith("coderabbit")) and timestamp > fresh:
                    self.discovered.add(identity(creator))
            expected |= self.discovered
        failed, pending, passed = [], [], []
        bot_progress = {bot: [] for bot in expected}
        bot_activity = {bot: [] for bot in expected}
        for check in checks:
            name, state, timestamp = self.check_fields(check)
            bot = identity((check.get("creator") or {}).get("login") or (((check.get("checkSuite") or {}).get("app") or {}).get("slug")))
            if bot in expected:
                if timestamp > fresh:
                    bot_progress[bot].append((state == "SUCCESS", timestamp, state))
                continue
            entry = {"name": name, "state": state, "link": check.get("detailsUrl") or check.get("targetUrl") or ""}
            if state in {"FAILURE", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE", "ERROR"}:
                failed.append(entry)
            elif state in {"SUCCESS", "SKIPPED", "NEUTRAL"}:
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
            unfinished.append(f"{bot}: {status}")
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
        author_reply_at = max((stamp(c.get("updatedAt")) for c in comments
                               if actor(c).get("login") == author_login), default=0)
        for comment in comments:
            who = actor(comment)
            if author_login and who.get("login") == author_login:
                continue
            item = {"author": who.get("login") or "unknown", "body": comment.get("body") or ""}
            if who.get("__typename") == "Bot":
                bot_comments.append(item)
                continue
            comment_time = stamp(comment.get("updatedAt"))
            if comment_time and ((observed_push and comment_time < observed_push)
                                 or (author_reply_at and comment_time < author_reply_at)):
                continue
            human_comments.append(item)
        return locals()

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
        if not self.complete or state["pending"] or state["pr"].get("mergeable") not in ("MERGEABLE",):
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
            except Fatal:
                raise
            except (Transient, KeyError, TypeError) as exc:
                self.complete = False
                print("ci-wait: snapshot: " + str(exc), file=sys.stderr)
                if self.snapshot:
                    state = self.classify(self.snapshot)
            if state:
                marker = self.marker(state)
                grace_end = state["grace_end"]
                bot_settled = not state["unfinished"] or (all(x.endswith(": not started") for x in state["unfinished"]) and now() >= grace_end)
                if marker == "CI_FAILED" or (self.complete and marker != "CI_PENDING" and bot_settled
                    and (state["expected"] or now() >= grace_end)):
                    break
            if now() >= self.deadline:
                break
            print(f"ci-wait: poll {poll}, {len(state['pending']) if state else '?'} checks pending, "
                  f"{max(0, int(self.deadline-now()))}s left", file=sys.stderr)
            time.sleep(min(self.interval, max(0, self.deadline - now())))
        if not state:
            return self.report(None, "CI_PENDING")
        return self.report(state, self.marker(state))

    def report(self, state, marker):
        label = marker.removeprefix("CI_").lower().replace("_", " ")
        lines = [f"## CI Status: {label}", "", f"**PR:** {self.url}",
                 f"**Head:** {(self.head or 'unknown')[:12]}",
                 f"**Elapsed:** ~{round((now()-self.started)/60, 1)} minutes"]
        if state:
            def section(title, entries):
                if entries:
                    lines.extend(["", "### " + title])
                    lines.extend("- " + entry for entry in entries)
            failed = []
            for check in state["failed"]:
                detail = check["name"] + (" (" + check["link"] + ")" if check["link"] else "")
                match = re.search(r"/actions/runs/(\d+)", check["link"])
                if match and marker == "CI_FAILED":
                    try:
                        log = self.run(["gh", "run", "view", match.group(1), "--log-failed"], margin=60)
                        detail += "\n  " + "\n  ".join(log.splitlines()[-100:])
                    except (Transient, Fatal) as exc:
                        detail += " (log unavailable: " + str(exc) + ")"
                failed.append(detail)
            section("Failed Checks", failed)
            if state["pr"].get("mergeable") == "CONFLICTING":
                conflicts = ["PR cannot merge cleanly"]
                base = state["pr"].get("baseRefOid")
                head = state["pr"].get("headRefOid")
                if base and head:
                    try:
                        self.run(["git", "cat-file", "-e", base + "^{commit}"])
                        self.run(["git", "cat-file", "-e", head + "^{commit}"])
                        paths = self.run(["git", "merge-tree", "--name-only", "--no-messages", base, head], allow_failure=True)
                        conflicts += [line for line in paths.splitlines() if line and not re.fullmatch(r"[0-9a-f]{40,64}", line)]
                    except Transient:
                        pass
                section("Merge Conflicts", conflicts)
            section("Blocking Reviews", [f"{actor(r).get('login', 'unknown')}: {r.get('body') or 'changes requested'}" for r in state["blocking"]])
            section("PR Comments", [f"{x['file']}:{x['line']} {x['author']}: {x['body']}" for x in state["actionable"]]
                    + [f"{x['author']}: {x['body']}" for x in state["human_comments"]])
            section("Deferred Threads", [f"{x['file']}:{x['line']} {x['author']}: {x['body']}" for x in state["deferred"]])
            section("Unfinished Review Bots", state["unfinished"])
            section("Informational Bot Comments", [f"{x['author']}: {x['body'][:500]}" for x in state["bot_comments"]])
            section("Still Running", [x["name"] for x in state["pending"]])
            section("Passing Checks", [x["name"] for x in state["passed"]])
        lines.extend(["", marker])
        return "\n".join(lines) + "\n"


def main():
    try:
        inputs = json.load(sys.stdin)
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
