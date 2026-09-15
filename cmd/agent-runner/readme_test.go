package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const projectVoiceFeedbackSentence = "The core value we want feedback on"

func TestReadmeFeedbackSentenceUsesProjectVoice(t *testing.T) {
	assertProjectVoiceFeedbackSentence(t, "README.md")
}

func TestIntroductionFeedbackSentenceUsesProjectVoice(t *testing.T) {
	assertProjectVoiceFeedbackSentence(t, filepath.Join("docs", "introduction.md"))
}

func assertProjectVoiceFeedbackSentence(t *testing.T, relPath string) {
	t.Helper()
	path := filepath.Join(findRepoRoot(t), relPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	content := string(data)
	if strings.Contains(content, "The core value I want feedback on") {
		t.Fatalf("%s uses first-person I in the feedback sentence", relPath)
	}
	if !strings.Contains(content, projectVoiceFeedbackSentence) {
		t.Fatalf("%s is missing the project-voice feedback sentence", relPath)
	}
}
