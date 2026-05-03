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
