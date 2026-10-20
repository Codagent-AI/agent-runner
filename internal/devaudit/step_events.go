//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
)

type stepEvent struct {
	attempt, line                   int
	stepType, mode, command, stdout string
	missingReason                   string
	stdoutTruncated                 bool
	outputWrites, repositoryPatch   *audit.WriteRecord
	start, end                      audit.GitCheckpoint
}
type stepEventIndex struct {
	events  map[string][]stepEvent
	missing bool
	reason  string
}

func stepEventKey(session, path string) string { return session + "\x00" + path }
func decodeEventField(data map[string]any, key string, target any) bool {
	value, ok := data[key]
	if !ok {
		return false
	}
	encoded, err := json.Marshal(value)
	return err == nil && json.Unmarshal(encoded, target) == nil
}
func readStepEvents(snapshot string) (stepEventIndex, error) {
	result := stepEventIndex{events: map[string][]stepEvent{}}
	file, err := openEvidenceFile(snapshot, "audit.log", os.O_RDONLY)
	if os.IsNotExist(err) {
		result.missing = true
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer func() { _ = file.Close() }()
	starts := map[string]stepEvent{}
	err = audit.VisitAuditLines(file, func(lineNo int, line string) error {
		event, err := audit.ParseAuditLine(line)
		if err != nil {
			return nil
		}
		session, _ := event.Data["execution_session_id"].(string)
		if session == "" {
			return nil
		}
		startKey := stepEventKey(session, event.Prefix)
		if event.Type == audit.EventStepStart {
			entry := stepEvent{}
			entry.mode, _ = event.Data["mode"].(string)
			entry.command, _ = event.Data["script"].(string)
			if cmd, ok := event.Data["command"].(string); ok {
				entry.command += " " + cmd
			}
			decodeEventField(event.Data, "git_checkpoint", &entry.start)
			starts[startKey] = entry
			return nil
		}
		if event.Type != audit.EventStepEnd {
			return nil
		}
		var identity model.ExecutionIdentity
		if !decodeEventField(event.Data, "identity", &identity) || identity.StepID == "" {
			return nil
		}
		if identity.ExecutionSessionID != "" && identity.ExecutionSessionID != session {
			return nil
		}
		entry := starts[startKey]
		delete(starts, startKey)
		entry.line = lineNo
		entry.attempt = identity.Attempt
		entry.stepType = identity.StepType
		entry.stdout, _ = event.Data["stdout"].(string)
		entry.stdoutTruncated, _ = event.Data["stdout_truncated"].(bool)
		entry.stdoutTruncated = entry.stdoutTruncated || strings.HasSuffix(entry.stdout, "...[truncated]")
		if mode, ok := event.Data["mode"].(string); ok {
			entry.mode = mode
		}
		decodeEventField(event.Data, "output_writes", &entry.outputWrites)
		decodeEventField(event.Data, "repository_patch", &entry.repositoryPatch)
		decodeEventField(event.Data, "git_start_checkpoint", &entry.start)
		decodeEventField(event.Data, "git_end_checkpoint", &entry.end)
		key := stepEventKey(session, logicalPath(identity.Prefix, identity.StepID))
		result.events[key] = append(result.events[key], entry)
		return nil
	})
	return result, err
}
func validatorLeaf(artifact *metrics.Artifact, session, path string, events []stepEvent) bool {
	for i := range artifact.Steps {
		r := &artifact.Steps[i]
		if r.ExecutionSessionID == session && r.Tool == "agent-validator" && (r.Prefix == path || logicalPath(r.Prefix, r.ID) == path || logicalPath(r.Prefix, "") == path) {
			return true
		}
	}
	for i := range events {
		event := &events[i]
		for _, field := range strings.FieldsFunc(event.command, func(r rune) bool {
			return r == ' ' || r == '\n' || r == '\t' || r == ';' || r == '|' || r == '&' || r == '\'' || r == '"'
		}) {
			if filepath.Base(field) == "agent-validator" {
				return true
			}
		}
	}
	return false
}

type artifactCoverage struct {
	absent, nonempty, missingCheckpoint, delta bool
	unavailable, partial                       []string
	contributions                              int
}

func (c *artifactCoverage) observe(event *stepEvent) {
	c.absent = c.absent || event.outputWrites == nil
	c.missingCheckpoint = c.missingCheckpoint || !event.start.Available || !event.end.Available
	c.delta = c.delta || len(audit.ChangedDirtyPaths(&event.start, &event.end)) > 0
}
func (c *artifactCoverage) status(git *GitEvidence) (status, reason string) {
	switch {
	case c.absent:
		return "unavailable", "written files not recorded for this run"
	case len(c.unavailable) > 0:
		return "unavailable", strings.Join(c.unavailable, "; ")
	case len(c.partial) > 0:
		return "partial", strings.Join(c.partial, "; ")
	case c.contributions > 0:
		return "available", ""
	case !c.nonempty && !c.missingCheckpoint && !c.delta && git.Attribution == "no_change" && len(git.CommitSHAs) == 0 && len(git.DeferredSHAs) == 0:
		return "none_produced", "observed no written files or Git delta"
	default:
		return "unavailable", "no retained contribution available"
	}
}
func stepReferenceBase(session, id, category, status, detail string) EvidenceReference {
	return EvidenceReference{ID: id, Category: category, Status: status, Detail: detail, ProducerExecutionSession: session, Lineage: "new"}
}
func outputReferenceForEvent(session, id, category string, event *stepEvent) EvidenceReference {
	out := stepReferenceBase(session, id+"-output", category, "available", event.stdout)
	if event.line > 0 {
		out.Ref = fmt.Sprintf("audit.log#%d", event.line)
	}
	out.Truncated = event.stdoutTruncated
	if event.stdout != "" {
		return out
	}
	out.Status = "unavailable"
	if event.missingReason != "" {
		out.Detail = event.missingReason
		return out
	}
	switch {
	case event.mode == "interactive":
		out.Detail = "interactive step records no final response"
	case event.stepType == "shell":
		out.Detail = "shell step without capture records no output"
	default:
		out.Detail = "no output recorded"
	}
	return out
}
func stepReferences(snapshot, session, path string, index stepEventIndex, artifact *metrics.Artifact, git *GitEvidence) []EvidenceReference {
	events := append([]stepEvent(nil), index.events[stepEventKey(session, path)]...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].attempt != events[j].attempt {
			return events[i].attempt > events[j].attempt
		}
		return events[i].line > events[j].line
	})
	narrative, category := "narrative", "artifact"
	if validatorLeaf(artifact, session, path, events) {
		narrative, category = "validation", "validation"
	}
	refs := []EvidenceReference{}
	coverage := artifactCoverage{absent: len(events) == 0}
	if len(events) == 0 {
		reason := "no output recorded"
		if index.missing {
			reason = "audit.log not in snapshot"
		}
		refs = append(refs, stepReferenceBase(session, "output-"+observationID("", session, path), narrative, "unavailable", reason))
	}
	for i := range events {
		event := &events[i]
		id := "step-" + observationID(session, path, fmt.Sprintf("%d:%d", event.attempt, event.line))
		refs = append(refs, outputReferenceForEvent(session, id, narrative, event))
		coverage.observe(event)
		for recordIndex, record := range []*audit.WriteRecord{event.outputWrites, event.repositoryPatch} {
			refs = append(refs, recordReferences(snapshot, session, fmt.Sprintf("%s-file-%d", id, recordIndex), category, record, &coverage)...)
		}
	}
	status, reason := coverage.status(git)
	refs = append(refs, stepReferenceBase(session, "written-coverage-"+observationID("", session, path), category, status, reason))
	return refs
}
func recordReferences(snapshot, session, id, category string, record *audit.WriteRecord, coverage *artifactCoverage) []EvidenceReference {
	if record == nil {
		return nil
	}
	switch record.Status {
	case "unavailable":
		coverage.unavailable = append(coverage.unavailable, record.Reason)
	case "truncated":
		coverage.partial = append(coverage.partial, record.Reason)
	case "observed":
	default:
		coverage.unavailable = append(coverage.unavailable, "unknown observation status")
	}
	coverage.nonempty = coverage.nonempty || len(record.Files) > 0
	files := append([]audit.FileWrite(nil), record.Files...)
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Path != files[j].Path {
			return files[i].Path < files[j].Path
		}
		return files[i].Segment < files[j].Segment
	})
	refs := make([]EvidenceReference, 0, len(files))
	for i := range files {
		ref := fileReference(snapshot, session, fmt.Sprintf("%s-%d", id, i), category, &files[i])
		coverage.observeFile(&files[i], &ref)
		refs = append(refs, ref)
	}
	return refs
}
func (c *artifactCoverage) observeFile(file *audit.FileWrite, ref *EvidenceReference) {
	if file.Change == "deleted" {
		return
	}
	if ref.Status == "unavailable" {
		c.partial = append(c.partial, ref.Detail)
	}
	if ref.Contribution == "context_possibly_inherited" {
		c.partial = append(c.partial, ref.Reason)
	}
	if ref.Truncated {
		c.partial = append(c.partial, "contribution truncated")
	}
	if ref.Status == "available" && oneOf(ref.Contribution, "append", "created", "diff") {
		c.contributions++
	}
}
func fileReference(snapshot, session, id, category string, file *audit.FileWrite) EvidenceReference {
	ref := stepReferenceBase(session, id, category, "available", "")
	ref.Path = file.Path
	ref.Segment = file.Segment
	c := file.Contribution
	if c == nil {
		ref.Status = "unavailable"
		ref.Detail = "contribution not recorded"
		if file.Change == "deleted" {
			ref.Detail = "deleted file has no textual contribution"
		}
		return ref
	}
	ref.Contribution = c.Kind
	ref.Truncated = c.Truncated
	ref.Reason = c.Reason
	blob := c.Blob
	if c.Kind == "unresolved" {
		ref.Contribution = "context_possibly_inherited"
		blob = c.Context
	}
	if c.Kind == "unavailable" {
		ref.Status = "unavailable"
		ref.Detail = c.Reason
		return ref
	}
	ref.Ref = "evidence/" + blob
	ref.LocalPath = blob
	detail, err := readSnapshotContent(snapshot, blob, maxContributionDetail)
	if err != nil {
		ref.Status = "unavailable"
		ref.Detail = "retained evidence unavailable: " + err.Error()
	} else {
		ref.Detail = detail
	}
	return ref
}

const maxContributionDetail = 64 << 10

func readSnapshotContent(snapshot, rel string, limit int) (string, error) {
	if rel == "" || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", fmt.Errorf("invalid snapshot reference")
	}
	f, err := openEvidenceFile(snapshot, rel, os.O_RDONLY)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)))
	return string(data), err
}

// snapshotReplayContent admits only durable session-owned events and copies
// only the blobs those events reference. Legacy, unowned lines are excluded.
func snapshotReplayContent(source, dest string, allowed map[string]struct{}) error {
	input, err := openEvidenceFile(source, "audit.log", os.O_RDONLY)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := openEvidenceFile(dest, "audit.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return err
	}
	blobs := map[string]bool{}
	err = audit.VisitAuditLines(input, func(_ int, line string) error {
		event, err := audit.ParseAuditLine(line)
		if err != nil {
			return nil
		}
		session, _ := event.Data["execution_session_id"].(string)
		if session == "" {
			return nil
		}
		if _, ok := allowed[session]; !ok {
			return nil
		}
		if _, err = fmt.Fprintln(output, line); err != nil {
			return err
		}
		collectEventBlobs(&event, blobs)
		return nil
	})
	closeErr := output.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, blob := range sortedBlobPaths(blobs) {
		name := strings.TrimPrefix(blob, "step-evidence/blobs/sha256/")
		if len(name) != 64 || strings.Trim(name, "0123456789abcdef") != "" {
			continue
		}
		content, err := readSnapshotContent(source, blob, maxVersionBytesForReplay)
		if err != nil {
			continue
		}
		path := filepath.Join(dest, blob)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func collectEventBlobs(event *audit.Event, blobs map[string]bool) {
	if event.Type != audit.EventStepEnd {
		return
	}
	for _, key := range []string{"output_writes", "repository_patch"} {
		var record audit.WriteRecord
		if !decodeEventField(event.Data, key, &record) {
			continue
		}
		for i := range record.Files {
			collectFileBlobs(&record.Files[i], blobs)
		}
	}
}

func collectFileBlobs(file *audit.FileWrite, blobs map[string]bool) {
	for _, version := range []*audit.FileVersion{file.Before, file.After} {
		if version != nil && version.Blob != "" {
			blobs[version.Blob] = true
		}
	}
	if file.Contribution != nil {
		for _, blob := range []string{file.Contribution.Blob, file.Contribution.Context} {
			if blob != "" {
				blobs[blob] = true
			}
		}
	}
}

const maxVersionBytesForReplay = 1 << 20

func sortedBlobPaths(blobs map[string]bool) []string {
	paths := make([]string, 0, len(blobs))
	for path := range blobs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// completeAttempts makes gaps visible even when other attempts have content.
func (index *stepEventIndex) completeAttempts(session, path string, records []metrics.StepRecord) {
	key := stepEventKey(session, path)
	events := index.events[key]
	if len(events) >= len(records) {
		return
	}
	known := map[int]bool{}
	for i := range events {
		known[events[i].attempt] = true
	}
	for i := range records {
		record := &records[i]
		if known[record.Attempt] {
			continue
		}
		reason := "no output recorded"
		if index.missing {
			reason = "audit.log not in snapshot"
		}
		if index.reason != "" {
			reason = index.reason
		}
		events = append(events, stepEvent{attempt: record.Attempt, stepType: record.Type, missingReason: reason})
	}
	index.events[key] = events
}
func firstStepLine(events []stepEvent) int {
	line := int(^uint(0) >> 1)
	for i := range events {
		if events[i].line > 0 && events[i].line < line {
			line = events[i].line
		}
	}
	return line
}

// Scope audit-log opens to the resolved evidence directory, including symlinks.
func openEvidenceFile(dir, name string, flags int) (*os.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.OpenFile(name, flags, 0o600)
}
