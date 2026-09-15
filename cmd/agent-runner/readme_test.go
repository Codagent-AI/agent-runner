package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadmeFeedbackSentenceUsesProjectVoice(t *testing.T) {
	path := filepath.Join(findRepoRoot(t), "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	readme := string(data)
	if strings.Contains(readme, "The core value I want feedback on") {
		t.Fatal("README.md uses first-person I in the feedback sentence")
	}
	if !strings.Contains(readme, "The core value we want feedback on") {
		t.Fatal("README.md is missing the project-voice feedback sentence")
	}
}
