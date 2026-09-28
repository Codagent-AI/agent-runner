package usersettings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRunRetentionAtEmptyFileUsesDefaults(t *testing.T) {
	for name, body := range map[string]string{
		"empty":         "",
		"whitespace":    "  \n\n",
		"comments only": "# nothing configured yet\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.yaml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			got, known, err := LoadRunRetentionAt(path)
			if err != nil || !known {
				t.Fatalf("LoadRunRetentionAt() known=%v err=%v, want known defaults", known, err)
			}
			if got.Enabled != nil || got.MaxAgeDays != nil || got.ResumableMaxAgeDays != nil || got.MaxRunsPerProject != nil || len(got.Invalid) != 0 {
				t.Fatalf("LoadRunRetentionAt() = %+v, want defaults", got)
			}
		})
	}
}

func TestLoadRunRetentionAtNullRootIsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("~\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, known, err := LoadRunRetentionAt(path); known || err == nil {
		t.Fatalf("LoadRunRetentionAt() known=%v err=%v, want unknown policy", known, err)
	}
}
