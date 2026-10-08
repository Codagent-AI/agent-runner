package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExternalUserSettings(t *testing.T) {
	dir := t.TempDir()
	settings, err := validateExternalUserSettings(dir, "10m")
	if err != nil || !filepath.IsAbs(settings.Dir) {
		t.Fatalf("settings=%v err=%v", settings, err)
	}
	for _, timeout := range []string{"wrong", "0s", "-1s"} {
		if _, err := validateExternalUserSettings(dir, timeout); err == nil {
			t.Fatal(timeout)
		}
	}
	os.WriteFile(filepath.Join(dir, "stale.reply.json"), []byte("{}"), 0o644)
	if _, err := validateExternalUserSettings(dir, ""); err == nil {
		t.Fatal("accepted stale files")
	}
	if _, err := validateExternalUserSettings(filepath.Join(dir, "missing"), ""); err == nil {
		t.Fatal("accepted missing directory")
	}
}
func TestExternalUserRunFlags(t *testing.T) {
	args, opts, err := parseRunCommandArgs([]string{"run", "workflow", "--external-user", "/exchange", "--external-user-timeout=10m"})
	if err != nil || len(args) != 1 || opts.externalUser != "/exchange" || opts.externalUserTimeout != "10m" {
		t.Fatalf("args=%v opts=%+v err=%v", args, opts, err)
	}
	for _, arg := range []string{"--external-user=", "--external-user-timeout="} {
		if _, _, err := parseRunCommandArgs([]string{"run", "workflow", arg}); err == nil {
			t.Fatal(arg)
		}
	}
}
