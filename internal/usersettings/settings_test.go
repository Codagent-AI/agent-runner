package usersettings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestPathResolvesSettingsFileUnderHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)

	got, err := Path()
	if err != nil {
		t.Fatalf("Path() returned error: %v", err)
	}

	want := filepath.Join(home, ".agent-runner", "settings.yaml")
	if got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestLoadMissingFileReturnsEmptySettingsAndDoesNotCreateParent(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != (Settings{}) {
		t.Fatalf("Load() = %#v, want empty settings", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-runner")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings parent stat error = %v, want not exist", err)
	}
}

func TestLoadThemeValues(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Theme
	}{
		{name: "light", body: "theme: light\n", want: ThemeLight},
		{name: "dark", body: "theme: dark\n", want: ThemeDark},
		{name: "unknown keys ignored", body: "experimental_foo: 7\ntheme: dark\n", want: ThemeDark},
		{name: "missing theme", body: "experimental_foo: 7\n", want: ""},
		{name: "capitalized light unset", body: "theme: Light\n", want: ""},
		{name: "capitalized dark unset", body: "theme: DARK\n", want: ""},
		{name: "auto unset", body: "theme: auto\n", want: ""},
		{name: "integer unset", body: "theme: 7\n", want: ""},
		{name: "sequence unset", body: "theme: [light]\n", want: ""},
		{name: "empty file unset", body: "", want: ""},
		{name: "whitespace file unset", body: "  \n# comment\n", want: ""},
		{name: "unparseable yaml unset", body: "theme: [unterminated\n", want: ""},
		{name: "sequence root unset", body: "- theme\n- light\n", want: ""},
		{name: "scalar root unset", body: "plain\n", want: ""},
		{name: "null root unset", body: "null\n", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			writeSettingsFile(t, home, tt.body)

			got, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error: %v", err)
			}
			if got.Theme != tt.want {
				t.Fatalf("Load().Theme = %q, want %q", got.Theme, tt.want)
			}
		})
	}
}

func TestLoadAutonomousBackendValues(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    AutonomousBackend
		wantErr string
	}{
		{name: "interactive claude", body: "autonomous_backend: interactive-claude\n", want: BackendInteractiveClaude},
		{name: "interactive", body: "autonomous_backend: interactive\n", want: BackendInteractive},
		{name: "headless", body: "autonomous_backend: headless\n", want: BackendHeadless},
		{name: "missing defaults empty", body: "theme: dark\n", want: ""},
		{name: "invalid rejected", body: "autonomous_backend: magic\n", wantErr: `invalid autonomous_backend "magic"`},
		{name: "sequence rejected", body: "autonomous_backend: [headless]\n", wantErr: "invalid autonomous_backend"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			writeSettingsFile(t, home, tt.body)

			got, err := Load()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("Load() returned nil error, want validation error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, want containing %q", err, tt.wantErr)
				}
				for _, valid := range []string{"headless", "interactive", "interactive-claude"} {
					if !strings.Contains(err.Error(), valid) {
						t.Fatalf("Load() error = %v, want valid option %q", err, valid)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() returned error: %v", err)
			}
			if got.AutonomousBackend != tt.want {
				t.Fatalf("Load().AutonomousBackend = %q, want %q", got.AutonomousBackend, tt.want)
			}
		})
	}
}

func TestSavePreservesAutonomousBackendOnUnrelatedWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSettingsFile(t, home, "autonomous_backend: interactive\ntheme: light\nexperimental_foo: 7\n")

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	settings.Theme = ThemeDark
	if err := Save(settings); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Reload() returned error: %v", err)
	}
	if reloaded.AutonomousBackend != BackendInteractive {
		t.Fatalf("AutonomousBackend = %q, want interactive", reloaded.AutonomousBackend)
	}
	body, err := os.ReadFile(filepath.Join(home, ".agent-runner", "settings.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(settings) returned error: %v", err)
	}
	if !strings.Contains(string(body), "experimental_foo: 7") {
		t.Fatalf("settings body lost unrelated key:\n%s", body)
	}
}

func TestSaveCreatesParentAndWritesMode0600(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := Save(Settings{Theme: ThemeDark}); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	settingsPath := filepath.Join(home, ".agent-runner", "settings.yaml")
	body, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("ReadFile(settings) returned error: %v", err)
	}
	if string(body) != "theme: dark\n" {
		t.Fatalf("settings body = %q, want theme: dark", body)
	}

	dirInfo, err := os.Stat(filepath.Join(home, ".agent-runner"))
	if err != nil {
		t.Fatalf("stat settings dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o755 {
		t.Fatalf("settings dir mode = %v, want 0755", got)
	}

	fileInfo, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatalf("stat settings file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("settings file mode = %v, want 0600", got)
	}
}

func TestSaveLeavesExistingParentModeUntouched(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".agent-runner")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}

	if err := Save(Settings{Theme: ThemeLight}); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat settings dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("existing settings dir mode = %v, want 0700", got)
	}
}

func TestConcurrentSavesLeaveCompletePayload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var wg sync.WaitGroup
	for _, theme := range []Theme{ThemeLight, ThemeDark} {
		theme := theme
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				if err := Save(Settings{Theme: theme}); err != nil {
					t.Errorf("Save(%s) returned error: %v", theme, err)
				}
			}
		}()
	}
	wg.Wait()

	body, err := os.ReadFile(filepath.Join(home, ".agent-runner", "settings.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(settings) returned error: %v", err)
	}
	if got := string(body); got != "theme: light\n" && got != "theme: dark\n" {
		t.Fatalf("settings body = %q, want exactly one complete payload", got)
	}
}

func TestSaveCleansUpTempFileWhenWriteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	original := writePayload
	writePayload = func(f *os.File, payload []byte) error {
		if _, err := f.Write(payload[:5]); err != nil {
			return err
		}
		return syscall.ENOSPC
	}
	t.Cleanup(func() { writePayload = original })

	err := Save(Settings{Theme: ThemeDark})
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("Save() error = %v, want ENOSPC", err)
	}

	matches, globErr := filepath.Glob(filepath.Join(home, ".agent-runner", "settings-*.yaml.tmp"))
	if globErr != nil {
		t.Fatalf("glob temp files: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}
}

func TestSaveRenameErrorIdentifiesSettingsPathAndUnderlyingError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	original := renameFile
	renameFile = func(oldpath, newpath string) error {
		return syscall.EACCES
	}
	t.Cleanup(func() { renameFile = original })

	err := Save(Settings{Theme: ThemeLight})
	if !errors.Is(err, syscall.EACCES) {
		t.Fatalf("Save() error = %v, want EACCES", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(home, ".agent-runner", "settings.yaml")) {
		t.Fatalf("Save() error = %v, want settings path", err)
	}
}

func TestSaveSetupCompletedAtPersistsWhenOnboardingAlreadySet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeSettingsFile(t, home, "theme: light\nonboarding:\n  completed_at: 2026-05-04T04:25:43Z\n")

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	settings.Setup.CompletedAt = "2026-05-08T12:00:00Z"

	if err := Save(settings); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Reload() returned error: %v", err)
	}
	if reloaded.Setup.CompletedAt != "2026-05-08T12:00:00Z" {
		t.Fatalf("Setup.CompletedAt = %q, want 2026-05-08T12:00:00Z", reloaded.Setup.CompletedAt)
	}
	if reloaded.Onboarding.CompletedAt != "2026-05-04T04:25:43Z" {
		t.Fatalf("Onboarding.CompletedAt = %q, want 2026-05-04T04:25:43Z", reloaded.Onboarding.CompletedAt)
	}
	if reloaded.Theme != ThemeLight {
		t.Fatalf("Theme = %q, want light", reloaded.Theme)
	}
}

func writeSettingsFile(t *testing.T, home, body string) {
	t.Helper()
	path := filepath.Join(home, ".agent-runner", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings file: %v", err)
	}
}
