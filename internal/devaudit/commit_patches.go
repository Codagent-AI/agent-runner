//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/codagent/agent-runner/internal/audit"
)

func exportCommitPatch(projectRoot, snapshot string, commit *snapshottedGitCommit) {
	if !audit.ValidGitObjectID(commit.SHA) {
		commit.PatchUnavailable = "invalid commit object ID"
		return
	}
	command := exec.Command("git", "-C", projectRoot, "show", "--no-ext-diff", "--no-textconv", "--format=%H%n%s", "--patch", "--end-of-options", commit.SHA, "--") // #nosec G204 -- full hexadecimal object ID validated above; Git options and paths are delimited.
	pipe, err := command.StdoutPipe()
	if err != nil {
		commit.PatchUnavailable = err.Error()
		return
	}
	if err = command.Start(); err != nil {
		commit.PatchUnavailable = err.Error()
		return
	}
	data, readErr := io.ReadAll(io.LimitReader(pipe, maxContributionDetail+1))
	if len(data) > maxContributionDetail || readErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if readErr != nil {
		commit.PatchUnavailable = readErr.Error()
		return
	}
	if len(data) <= maxContributionDetail && waitErr != nil {
		commit.PatchUnavailable = waitErr.Error()
		return
	}
	if len(data) > maxContributionDetail {
		const marker = "\n[patch truncated]\n"
		data = append(data[:maxContributionDetail-len(marker)], marker...)
		commit.PatchTruncated = true
	}
	rel := "git-patches/" + commit.SHA + ".patch"
	path := filepath.Join(snapshot, rel)
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		err = os.WriteFile(path, data, 0o600)
	}
	if err != nil {
		commit.PatchUnavailable = err.Error()
		return
	}
	commit.Patch = rel
}

// patchSections uses Git's +++/--- paths, including its quoted path encoding.
// show emits diff --git sections even for empty-file, rename, and binary edits.
func patchSections(patch string) map[string]string {
	result := map[string]string{}
	starts := []int{}
	offset := 0
	for _, line := range strings.SplitAfter(patch, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			starts = append(starts, offset)
		}
		offset += len(line)
	}
	for i, start := range starts {
		end := len(patch)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		section := patch[start:end]
		path := patchSectionPath(section)
		if path != "" {
			result[path] += section
		}
	}
	return result
}
func patchSectionPath(section string) string {
	path := ""
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "+++ ") && line != "+++ /dev/null" {
			path = decodePatchPath(strings.TrimPrefix(line, "+++ "))
			break
		}
		if strings.HasPrefix(line, "--- ") {
			path = decodePatchPath(strings.TrimPrefix(line, "--- "))
		}
	}
	if path == "" {
		line, _, _ := strings.Cut(section, "\n")
		_, right, ok := strings.Cut(line, " b/")
		if ok {
			path = right
		} else {
			fields := strings.Fields(strings.TrimPrefix(line, "diff --git "))
			if len(fields) == 2 {
				path = decodePatchPath(fields[1])
			}
		}
	}
	return path
}
func decodePatchPath(path string) string {
	if strings.HasPrefix(path, "\"") {
		if decoded, err := strconv.Unquote(path); err == nil {
			path = decoded
		}
	}
	if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
		path = path[2:]
	}
	return path
}
func joinPatchSections(sections map[string]string, paths []string, include bool) string {
	selected := map[string]bool{}
	for _, path := range paths {
		selected[path] = true
	}
	keys := map[string]bool{}
	for path := range sections {
		keys[path] = true
	}
	var out strings.Builder
	for _, path := range sortedBlobPaths(keys) {
		if selected[path] == include {
			out.WriteString(sections[path])
		}
	}
	return out.String()
}
func attachCommitReferences(leaves []LeafEvidence, commits map[string]snapshottedGitCommit, snapshot string) {
	deferred := map[string][]string{}
	for i := range leaves {
		git := &leaves[i].Skeleton.Git
		for _, sha := range git.DeferredSHAs {
			deferred[sha] = append(deferred[sha], git.DeferralPaths...)
		}
	}
	for i := range leaves {
		leaf := &leaves[i]
		git := &leaf.Skeleton.Git

		// no_change may retain commit identities after every path was deferred;
		// splitting also handles a mixed commit with some directly owned paths.
		if git.Attribution == "attributed" || git.Attribution == "no_change" {
			for _, sha := range git.CommitSHAs {
				if ref := commitReference(leaf, commits, snapshot, sha, "commit_patch", deferred[sha], false); ref != nil {
					leaf.Evidence = append(leaf.Evidence, *ref)
				}
			}
		}
		for _, sha := range git.DeferredSHAs {
			if ref := commitReference(leaf, commits, snapshot, sha, "commit_patch_confirmation", git.DeferralPaths, true); ref != nil {
				leaf.Evidence = append(leaf.Evidence, *ref)
			}
		}
		refreshCommitCoverage(leaf)
	}
}

func commitReference(leaf *LeafEvidence, commits map[string]snapshottedGitCommit, snapshot, sha, kind string, paths []string, include bool) *EvidenceReference {
	commit, ok := commits[sha]
	ref := EvidenceReference{ID: fmt.Sprintf("%s-%s-%s", kind, leaf.Skeleton.ObservationID, sha), Category: "artifact", Status: "available", Contribution: kind, ProducerExecutionSession: leaf.Skeleton.ExecutionSessionID, Lineage: leaf.Skeleton.Lineage, Truncated: commit.PatchTruncated}

	if !ok || commit.Patch == "" {
		ref.Status = "unavailable"
		ref.Detail = commit.PatchUnavailable
		if ref.Detail == "" {
			ref.Detail = "commit patch not in snapshot"
		}
		return &ref
	}
	ref.Ref = "evidence/" + commit.Patch
	ref.LocalPath = commit.Patch
	content, err := readSnapshotContent(snapshot, commit.Patch, maxContributionDetail)
	if err != nil {
		ref.Status = "unavailable"
		ref.Detail = err.Error()
		return &ref
	}
	ref.Detail = joinPatchSections(patchSections(content), paths, include)
	if ref.Detail == "" {
		return nil
	}
	return &ref
}

// Commit evidence can fill an otherwise empty observation, but cannot repair
// missing recording coverage. Incomplete patches remain explicit limitations.
func refreshCommitCoverage(leaf *LeafEvidence) {
	hasPatch := false
	limitations := []string{}
	for i := range leaf.Evidence {
		ref := &leaf.Evidence[i]
		if ref.Category == "git" {
			data, _ := json.Marshal(leaf.Skeleton.Git)
			ref.Detail = string(data)
		}
		if ref.Contribution != "commit_patch" && ref.Contribution != "commit_patch_confirmation" {
			continue
		}
		if ref.Contribution == "commit_patch" && ref.Status == "available" {
			hasPatch = true
		}
		if ref.Truncated {
			limitations = append(limitations, "commit patch truncated")
		}
		if ref.Status == "unavailable" {
			limitations = append(limitations, ref.Detail)
		}
	}
	for i := range leaf.Evidence {
		ref := &leaf.Evidence[i]
		if !strings.HasPrefix(ref.ID, "written-coverage-") {
			continue
		}
		if ref.Detail == "no retained contribution available" && hasPatch {
			ref.Status = "available"
			ref.Detail = ""
		}
		if len(limitations) > 0 && ref.Status != "unavailable" {
			ref.Status = "partial"
			ref.Detail = strings.Trim(ref.Detail+"; "+strings.Join(limitations, "; "), "; ")
		}
	}
}
