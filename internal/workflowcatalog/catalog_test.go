package workflowcatalog

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want Definition
	}{
		{
			name: "yaml extension",
			path: "deploy-v1.0.yaml",
			want: Definition{
				Path:          "deploy-v1.0.yaml",
				LogicalName:   "deploy",
				CanonicalName: "deploy",
				Version:       Version{Major: "1", Minor: "0"},
				DisplayLabel:  "v1.0",
			},
		},
		{
			name: "yml extension",
			path: "verify-v2.3.yml",
			want: Definition{
				Path:          "verify-v2.3.yml",
				LogicalName:   "verify",
				CanonicalName: "verify",
				Version:       Version{Major: "2", Minor: "3"},
				DisplayLabel:  "v2.3",
			},
		},
		{
			name: "zero version",
			path: "prototype-v0.0.yaml",
			want: Definition{
				Path:          "prototype-v0.0.yaml",
				LogicalName:   "prototype",
				CanonicalName: "prototype",
				Version:       Version{Major: "0", Minor: "0"},
				DisplayLabel:  "v0.0",
			},
		},
		{
			name: "nested identity",
			path: "team/deploy-v2.0.yaml",
			want: Definition{
				Path:          "team/deploy-v2.0.yaml",
				LogicalName:   "deploy",
				CanonicalName: "team/deploy",
				Version:       Version{Major: "2", Minor: "0"},
				DisplayLabel:  "v2.0",
			},
		},
		{
			name: "final suffix determines identity",
			path: "save-v-data-v1.2.yaml",
			want: Definition{
				Path:          "save-v-data-v1.2.yaml",
				LogicalName:   "save-v-data",
				CanonicalName: "save-v-data",
				Version:       Version{Major: "1", Minor: "2"},
				DisplayLabel:  "v1.2",
			},
		},
		{
			name: "arbitrary size components",
			path: "future-v184467440737095516160.999999999999999999999.yaml",
			want: Definition{
				Path:          "future-v184467440737095516160.999999999999999999999.yaml",
				LogicalName:   "future",
				CanonicalName: "future",
				Version: Version{
					Major: "184467440737095516160",
					Minor: "999999999999999999999",
				},
				DisplayLabel: "v184467440737095516160.999999999999999999999",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Parse(tt.path)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.path, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("Parse(%q) mismatch (-want +got):\n%s", tt.path, diff)
			}
		})
	}
}

func TestParseRejectsInvalidFilenamesWithActionableFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		wantKind    FilenameErrorKind
		wantGroup   string
		wantExample string
	}{
		{
			name:        "uppercase logical name",
			path:        "Deploy-v1.0.yaml",
			wantKind:    FilenameErrorUppercase,
			wantGroup:   "deploy",
			wantExample: "deploy-v1.0.yaml",
		},
		{
			name:        "missing minor",
			path:        "deploy-v1.yaml",
			wantKind:    FilenameErrorPattern,
			wantGroup:   "deploy",
			wantExample: "deploy-v1.0.yaml",
		},
		{
			name:        "patch version",
			path:        "deploy-v1.2.3.yaml",
			wantKind:    FilenameErrorPattern,
			wantGroup:   "deploy",
			wantExample: "deploy-v1.0.yaml",
		},
		{
			name:        "leading zero",
			path:        "deploy-v01.2.yaml",
			wantKind:    FilenameErrorPattern,
			wantGroup:   "deploy",
			wantExample: "deploy-v1.0.yaml",
		},
		{
			name:        "non-decimal minor",
			path:        "deploy-v1.x.yaml",
			wantKind:    FilenameErrorPattern,
			wantGroup:   "deploy",
			wantExample: "deploy-v1.0.yaml",
		},
		{
			name:        "unversioned",
			path:        "deploy.yaml",
			wantKind:    FilenameErrorPattern,
			wantGroup:   "deploy",
			wantExample: "deploy-v1.0.yaml",
		},
		{
			name:        "malformed final attempt preserves earlier version text",
			path:        "save-v-data-v1.x.yaml",
			wantKind:    FilenameErrorPattern,
			wantGroup:   "save-v-data",
			wantExample: "save-v-data-v1.0.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(tt.path)
			if err == nil {
				t.Fatalf("Parse(%q) error = nil, want failure", tt.path)
			}

			var filenameErr *FilenameError
			if !errors.As(err, &filenameErr) {
				t.Fatalf("Parse(%q) error type = %T, want *FilenameError", tt.path, err)
			}
			if diff := cmp.Diff(tt.wantKind, filenameErr.Kind); diff != "" {
				t.Errorf("error kind mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantGroup, filenameErr.Group); diff != "" {
				t.Errorf("error group mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantExample, filenameErr.Example); diff != "" {
				t.Errorf("error example mismatch (-want +got):\n%s", diff)
			}
			for _, text := range []string{tt.path, RequiredFilenamePattern, tt.wantExample} {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("error %q does not contain %q", err, text)
				}
			}
			if tt.wantKind == FilenameErrorUppercase && !strings.Contains(err.Error(), "lowercase") {
				t.Errorf("uppercase error %q does not require lowercase", err)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  Version
		right Version
		want  int
	}{
		{
			name:  "minor compared numerically",
			left:  Version{Major: "2", Minor: "9"},
			right: Version{Major: "2", Minor: "10"},
			want:  -1,
		},
		{
			name:  "major takes precedence",
			left:  Version{Major: "1", Minor: "99"},
			right: Version{Major: "2", Minor: "0"},
			want:  -1,
		},
		{
			name:  "components beyond uint64",
			left:  Version{Major: "18446744073709551616", Minor: "0"},
			right: Version{Major: "9999999999999999999", Minor: "999999999999999999999"},
			want:  1,
		},
		{
			name:  "equal",
			left:  Version{Major: "12345678901234567890", Minor: "12345678901234567890"},
			right: Version{Major: "12345678901234567890", Minor: "12345678901234567890"},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.left.Compare(tt.right); got != tt.want {
				t.Fatalf("Version.Compare() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIsExempt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: "_group.yaml", want: true},
		{path: "core/_helpers.yml", want: true},
		{path: "team/deploy-v1.0.yaml", want: false},
		{path: "team/other.txt", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			if got := IsExempt(tt.path); got != tt.want {
				t.Fatalf("IsExempt(%q) = %t, want %t", tt.path, got, tt.want)
			}
		})
	}
}

func TestBuildGroupsAndSelectsLatestVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		paths      []string
		groupName  string
		wantPaths  []string
		wantLatest string
	}{
		{
			name: "minor versions compared numerically",
			paths: []string{
				"deploy-v2.9.yaml",
				"deploy-v2.10.yaml",
			},
			groupName: "deploy",
			wantPaths: []string{
				"deploy-v2.9.yaml",
				"deploy-v2.10.yaml",
			},
			wantLatest: "deploy-v2.10.yaml",
		},
		{
			name: "major takes precedence",
			paths: []string{
				"deploy-v1.99.yaml",
				"deploy-v2.0.yaml",
			},
			groupName: "deploy",
			wantPaths: []string{
				"deploy-v1.99.yaml",
				"deploy-v2.0.yaml",
			},
			wantLatest: "deploy-v2.0.yaml",
		},
		{
			name: "arbitrary size versions",
			paths: []string{
				"future-v99999999999999999999.999999999999999999999.yml",
				"future-v100000000000000000000.0.yaml",
			},
			groupName: "future",
			wantPaths: []string{
				"future-v99999999999999999999.999999999999999999999.yml",
				"future-v100000000000000000000.0.yaml",
			},
			wantLatest: "future-v100000000000000000000.0.yaml",
		},
		{
			name: "nested canonical groups remain distinct",
			paths: []string{
				"blue/deploy-v3.0.yaml",
				"green/deploy-v4.0.yaml",
			},
			groupName:  "green/deploy",
			wantPaths:  []string{"green/deploy-v4.0.yaml"},
			wantLatest: "green/deploy-v4.0.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			catalog := Build(tt.paths)
			group, ok := catalog.Lookup(tt.groupName)
			if !ok {
				t.Fatalf("Lookup(%q) found = false; groups = %#v", tt.groupName, catalog.Groups)
			}
			if group.Err != nil {
				t.Fatalf("Lookup(%q) error = %v", tt.groupName, group.Err)
			}
			var gotPaths []string
			for _, definition := range group.Definitions {
				gotPaths = append(gotPaths, definition.Path)
			}
			if diff := cmp.Diff(tt.wantPaths, gotPaths); diff != "" {
				t.Errorf("definition paths mismatch (-want +got):\n%s", diff)
			}
			if group.Selected == nil {
				t.Fatalf("Lookup(%q) selected = nil", tt.groupName)
			}
			if diff := cmp.Diff(tt.wantLatest, group.Selected.Path); diff != "" {
				t.Errorf("selected path mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildInvalidatesOnlyAssociatedGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		paths           []string
		invalidGroup    string
		wantInvalidPath string
		validGroup      string
		wantSelected    string
	}{
		{
			name: "malformed version attempt invalidates intended group",
			paths: []string{
				"deploy-v1.yaml",
				"deploy-v2.0.yaml",
				"verify-v1.0.yaml",
			},
			invalidGroup:    "deploy",
			wantInvalidPath: "deploy-v1.yaml",
			validGroup:      "verify",
			wantSelected:    "verify-v1.0.yaml",
		},
		{
			name: "uppercase filename invalidates lowercase group",
			paths: []string{
				"Deploy-v1.0.yaml",
				"deploy-v2.0.yaml",
				"verify-v1.0.yaml",
			},
			invalidGroup:    "deploy",
			wantInvalidPath: "Deploy-v1.0.yaml",
			validGroup:      "verify",
			wantSelected:    "verify-v1.0.yaml",
		},
		{
			name: "unversioned sibling invalidates group",
			paths: []string{
				"deploy.yaml",
				"deploy-v2.0.yaml",
				"verify-v1.0.yaml",
			},
			invalidGroup:    "deploy",
			wantInvalidPath: "deploy.yaml",
			validGroup:      "verify",
			wantSelected:    "verify-v1.0.yaml",
		},
		{
			name: "nested invalid group does not disable sibling directory",
			paths: []string{
				"blue/deploy-v1.x.yaml",
				"blue/deploy-v2.0.yaml",
				"green/deploy-v1.0.yaml",
			},
			invalidGroup:    "blue/deploy",
			wantInvalidPath: "blue/deploy-v1.x.yaml",
			validGroup:      "green/deploy",
			wantSelected:    "green/deploy-v1.0.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			catalog := Build(tt.paths)

			invalid, ok := catalog.Lookup(tt.invalidGroup)
			if !ok {
				t.Fatalf("Lookup(%q) found = false", tt.invalidGroup)
			}
			if invalid.Selected != nil {
				t.Errorf("invalid group selected = %#v, want nil", invalid.Selected)
			}
			if invalid.Err == nil {
				t.Fatalf("invalid group error = nil")
			}
			if len(invalid.Err.InvalidFilenames) != 1 {
				t.Fatalf("invalid filename errors = %#v, want one", invalid.Err.InvalidFilenames)
			}
			filenameErr := invalid.Err.InvalidFilenames[0]
			if diff := cmp.Diff(tt.wantInvalidPath, filenameErr.Path); diff != "" {
				t.Errorf("invalid path mismatch (-want +got):\n%s", diff)
			}
			for _, text := range []string{tt.wantInvalidPath, RequiredFilenamePattern, filenameErr.Example} {
				if !strings.Contains(invalid.Err.Error(), text) {
					t.Errorf("group error %q does not contain %q", invalid.Err, text)
				}
			}

			valid, ok := catalog.Lookup(tt.validGroup)
			if !ok {
				t.Fatalf("Lookup(%q) found = false", tt.validGroup)
			}
			if valid.Err != nil {
				t.Fatalf("valid group error = %v", valid.Err)
			}
			if valid.Selected == nil || valid.Selected.Path != tt.wantSelected {
				t.Fatalf("valid group selected = %#v, want path %q", valid.Selected, tt.wantSelected)
			}
		})
	}
}

func TestBuildRejectsDuplicateNumericVersionAcrossExtensions(t *testing.T) {
	t.Parallel()

	catalog := Build([]string{
		"deploy-v1.0.yml",
		"deploy-v2.0.yaml",
		"deploy-v1.0.yaml",
	})

	group, ok := catalog.Lookup("deploy")
	if !ok {
		t.Fatal("Lookup(\"deploy\") found = false")
	}
	if group.Selected != nil {
		t.Fatalf("duplicate group selected = %#v, want nil", group.Selected)
	}
	if group.Err == nil {
		t.Fatal("duplicate group error = nil")
	}
	want := []DuplicateVersionError{
		{
			CanonicalName: "deploy",
			Version:       Version{Major: "1", Minor: "0"},
			Paths:         []string{"deploy-v1.0.yaml", "deploy-v1.0.yml"},
			Pattern:       RequiredFilenamePattern,
			Example:       "deploy-v1.1.yaml",
		},
	}
	if diff := cmp.Diff(want, group.Err.DuplicateVersions); diff != "" {
		t.Errorf("duplicate errors mismatch (-want +got):\n%s", diff)
	}
	for _, text := range []string{
		"deploy-v1.0.yaml",
		"deploy-v1.0.yml",
		RequiredFilenamePattern,
		"deploy-v1.1.yaml",
	} {
		if !strings.Contains(group.Err.Error(), text) {
			t.Errorf("group error %q does not contain %q", group.Err, text)
		}
	}
}

func TestBuildIgnoresUnderscorePrefixedYAMLFiles(t *testing.T) {
	t.Parallel()

	catalog := Build([]string{
		"_helpers.yaml",
		"core/_group.yaml",
		"user/_notes.yml",
		"deploy-v1.0.yaml",
	})

	want := Catalog{
		Groups: []Group{
			{
				CanonicalName: "deploy",
				Definitions: []Definition{
					{
						Path:          "deploy-v1.0.yaml",
						LogicalName:   "deploy",
						CanonicalName: "deploy",
						Version:       Version{Major: "1", Minor: "0"},
						DisplayLabel:  "v1.0",
					},
				},
				Selected: &Definition{
					Path:          "deploy-v1.0.yaml",
					LogicalName:   "deploy",
					CanonicalName: "deploy",
					Version:       Version{Major: "1", Minor: "0"},
					DisplayLabel:  "v1.0",
				},
			},
		},
	}
	if diff := cmp.Diff(want, catalog); diff != "" {
		t.Fatalf("Build() mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildIsDeterministicAndIndependentOfEnumerationOrder(t *testing.T) {
	t.Parallel()

	forward := []string{
		"verify-v1.0.yml",
		"deploy-v2.10.yaml",
		"Deploy-v1.0.yaml",
		"deploy-v2.9.yaml",
		"verify-v1.0.yaml",
		"alpha-v100000000000000000000.0.yaml",
	}
	reverse := make([]string, len(forward))
	for index := range forward {
		reverse[len(forward)-1-index] = forward[index]
	}

	gotForward := Build(forward)
	gotReverse := Build(reverse)
	if diff := cmp.Diff(gotForward, gotReverse); diff != "" {
		t.Fatalf("catalog depends on enumeration order (-forward +reverse):\n%s", diff)
	}

	gotRepeated := Build(append(forward, forward...))
	if diff := cmp.Diff(gotForward, gotRepeated); diff != "" {
		t.Fatalf("catalog depends on repeated enumeration (-once +repeated):\n%s", diff)
	}
}
