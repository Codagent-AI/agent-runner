package externaluser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExchangeProtocol(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reply.json")
	for _, body := range []string{`{`, `{"schema_version":2,"text":"yes"}`, `{"schema_version":1}`, `{"schema_version":1,"action":"unknown"}`} {
		if err := AtomicWrite(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := WaitReply(context.Background(), path, time.Second); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	if err := AtomicWrite(path, []byte(`{"schema_version":1,"text":"- option a"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	reply, err := WaitReply(context.Background(), path, time.Second)
	if err != nil || reply.Text != "- option a" {
		t.Fatalf("reply=%+v error=%v", reply, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Fatal(info.Mode())
	}
	os.Remove(path)
	if _, err := WaitReply(context.Background(), path, time.Millisecond); err == nil {
		t.Fatal("missing timeout")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := WaitReply(ctx, path, 0); err == nil {
		t.Fatal("missing cancellation")
	}
}
func TestReplayRecord(t *testing.T) {
	dir := t.TempDir()
	id := Identity{StepKey: StepKey("define.proposal"), Attempt: 1, Turn: 2}
	if id.RequestName() != "define.proposal-1-2.request.json" {
		t.Fatal(id.RequestName())
	}
	event := Event{Type: "reply_acted", Identity: id, Reply: &Reply{SchemaVersion: 1, Text: "yes"}}
	if err := Append(dir, &event); err != nil {
		t.Fatal(err)
	}
	last, err := Last(dir, id.StepKey)
	if err != nil || last.Reply.Text != "yes" {
		t.Fatalf("last=%+v error=%v", last, err)
	}
}

func TestStepKeyDoesNotCollide(t *testing.T) {
	paths := []string{"define.proposal", "define:1.proposal", "define_3a_1.proposal", "define/child.proposal", "define[1].proposal"}
	seen := map[string]bool{}
	for _, path := range paths {
		key := StepKey(path)
		if seen[key] {
			t.Fatalf("collision: %s", key)
		}
		seen[key] = true
		for _, r := range key {
			if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-", r) {
				t.Fatal(key)
			}
		}
	}
}

func TestReplayIgnoresInterruptedAppend(t *testing.T) {
	dir := t.TempDir()
	id := Identity{StepKey: "proposal", Attempt: 1, Turn: 1}
	event := Event{Type: "request_written", Identity: id, Request: &Request{SchemaVersion: 1, Step: "proposal"}}
	if err := Append(dir, &event); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(RecordPath(dir), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"reply_acted"`)
	f.Close()
	last, err := Last(dir, id.StepKey)
	if err != nil || last.Type != "request_written" {
		t.Fatalf("last=%+v err=%v", last, err)
	}
	event.Type = "turn_finished"
	if err := Append(dir, &event); err != nil {
		t.Fatal(err)
	}
	last, err = Last(dir, id.StepKey)
	if err != nil || last.Type != "turn_finished" {
		t.Fatalf("last=%+v err=%v", last, err)
	}
}

func TestReplayRecordLargeRequest(t *testing.T) {
	dir := t.TempDir()
	message := strings.Repeat("x", 9*1024*1024)
	if err := Append(dir, &Event{Type: "request_written", Identity: Identity{StepKey: "large"}, Request: &Request{AgentMessage: message}}); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, &Event{Type: "turn_finished", Identity: Identity{StepKey: "other"}}); err != nil {
		t.Fatal(err)
	}
	last, err := Last(dir, "large")
	if err != nil {
		t.Fatal(err)
	}
	if last.Request == nil || last.Request.AgentMessage != message {
		t.Fatal("large request was not preserved")
	}
	last, err = Last(dir, "other")
	if err != nil || last.Type != "turn_finished" {
		t.Fatalf("other step blocked by large request: %q, %v", last.Type, err)
	}
}

func TestExchangeRejectsEscapingSymlinks(t *testing.T) {
	t.Run("reply", func(t *testing.T) {
		dir, outside := t.TempDir(), t.TempDir()
		target := filepath.Join(outside, "reply.json")
		if err := os.WriteFile(target, []byte(`{"schema_version":1,"text":"outside"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "reply.json")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := WaitReply(context.Background(), path, time.Second); err == nil {
			t.Fatal("read reply outside exchange directory")
		}
	})
	t.Run("record", func(t *testing.T) {
		dir, outside := t.TempDir(), t.TempDir()
		if err := os.Symlink(outside, filepath.Join(dir, "external-user")); err != nil {
			t.Fatal(err)
		}
		if err := Append(dir, &Event{Type: "turn_finished"}); err == nil {
			t.Fatal("wrote replay record outside session directory")
		}
	})
}
