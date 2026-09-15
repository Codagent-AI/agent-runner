package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadmeOpeningSentenceUsesAllows(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	body, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	sentence := readmeOpeningSentence(string(body))
	if strings.Contains(sentence, "It lets you define") {
		t.Fatal(`README.md opening sentence still uses "lets"`)
	}
	if !strings.Contains(sentence, "It allows you to define") {
		t.Fatal(`README.md opening sentence should use "allows"`)
	}
}

func TestReadmeOpeningSentenceIgnoresLaterWording(t *testing.T) {
	readme := strings.Join([]string{
		"# Agent Runner",
		"",
		"Agent Runner is a local workflow runner for coding agents.",
		"",
		"It allows you to define workflows.",
		"",
		"This later paragraph lets you define unrelated wording.",
	}, "\n")
	got := readmeOpeningSentence(readme)
	want := "It allows you to define workflows."
	if got != want {
		t.Fatalf("readmeOpeningSentence() = %q, want %q", got, want)
	}
}

func readmeOpeningSentence(readme string) string {
	text := strings.TrimSpace(readme)
	if strings.HasPrefix(text, "#") {
		_, text, _ = strings.Cut(text, "\n")
		text = strings.TrimSpace(text)
	}
	paragraphs := strings.SplitN(text, "\n\n", 3)
	target := text
	if len(paragraphs) >= 2 {
		target = paragraphs[1]
	}
	return firstSentence(target)
}

func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	sentence, _, found := strings.Cut(text, ".")
	if !found {
		return text
	}
	return strings.TrimSpace(sentence) + "."
}
