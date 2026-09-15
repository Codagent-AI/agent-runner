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
	text := string(body)
	if strings.Contains(text, "It lets you define") {
		t.Fatal(`README.md opening sentence still uses "lets"`)
	}
	if !strings.Contains(text, "It allows you to define") {
		t.Fatal(`README.md opening sentence should use "allows"`)
	}
}
