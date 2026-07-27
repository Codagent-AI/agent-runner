// Package workflowcatalog defines source-neutral workflow filename identity,
// validation, grouping, and version selection.
package workflowcatalog

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RequiredFilenamePattern is the filename shape required for workflow
// definitions.
const RequiredFilenamePattern = "<logical-name>-v<major>.<minor>.yaml or .yml"

var (
	versionedStemPattern       = regexp.MustCompile(`^(.*)-v(\d+)\.(\d+)$`)
	malformedVersionAttempt    = regexp.MustCompile(`^(.*)-v\d.*$`)
	validLogicalNamePattern    = regexp.MustCompile(`^[a-z0-9_-]+$`)
	invalidExampleRunPattern   = regexp.MustCompile(`[^a-z0-9_-]+`)
	repeatedExampleDashPattern = regexp.MustCompile(`-+`)
)

// Version is a validated workflow major/minor version. Components remain
// decimal strings so their size is not limited by a machine integer.
type Version struct {
	Major string
	Minor string
}

// String returns the version without its leading "v".
func (v Version) String() string {
	return v.Major + "." + v.Minor
}

// Label returns the version formatted for display.
func (v Version) Label() string {
	return "v" + v.String()
}

// Compare returns -1, 0, or 1 when v is less than, equal to, or greater than
// other. Versions returned by Parse always have normalized decimal components.
func (v Version) Compare(other Version) int {
	if comparison := compareDecimal(v.Major, other.Major); comparison != 0 {
		return comparison
	}
	return compareDecimal(v.Minor, other.Minor)
}

// Definition contains the filename-derived facts for one valid workflow
// definition.
type Definition struct {
	Path          string
	LogicalName   string
	CanonicalName string
	Version       Version
	DisplayLabel  string
}

// FilenameErrorKind classifies an invalid workflow filename.
type FilenameErrorKind string

const (
	// FilenameErrorPattern means the filename does not match the required
	// versioned workflow pattern.
	FilenameErrorPattern FilenameErrorKind = "pattern"
	// FilenameErrorUppercase means the logical basename or one of its
	// directory qualifiers contains uppercase ASCII letters.
	FilenameErrorUppercase FilenameErrorKind = "uppercase"
)

// FilenameError retains actionable facts about an invalid workflow filename.
type FilenameError struct {
	Path    string
	Group   string
	Kind    FilenameErrorKind
	Pattern string
	Example string
}

func (e *FilenameError) Error() string {
	if e.Kind == FilenameErrorUppercase {
		return fmt.Sprintf(
			"workflow file %q must use a lowercase logical name and match %s; rename it to %q",
			e.Path,
			e.Pattern,
			e.Example,
		)
	}
	return fmt.Sprintf(
		"workflow file %q must match %s; rename it to %q",
		e.Path,
		e.Pattern,
		e.Example,
	)
}

// DuplicateVersionError retains every file that defines the same numeric
// version within one canonical logical group.
type DuplicateVersionError struct {
	CanonicalName string
	Version       Version
	Paths         []string
	Pattern       string
	Example       string
}

func (e *DuplicateVersionError) Error() string {
	quotedPaths := make([]string, len(e.Paths))
	for index, candidatePath := range e.Paths {
		quotedPaths[index] = fmt.Sprintf("%q", candidatePath)
	}
	return fmt.Sprintf(
		"workflow group %q defines duplicate version %s in files %s; filenames must match %s and one file can be renamed to %q",
		e.CanonicalName,
		e.Version.Label(),
		strings.Join(quotedPaths, ", "),
		e.Pattern,
		e.Example,
	)
}

// GroupError contains every filename and duplicate-version problem associated
// with one logical group.
type GroupError struct {
	CanonicalName     string
	InvalidFilenames  []FilenameError
	DuplicateVersions []DuplicateVersionError
}

func (e *GroupError) Error() string {
	problems := make([]string, 0, len(e.InvalidFilenames)+len(e.DuplicateVersions))
	for index := range e.InvalidFilenames {
		problems = append(problems, e.InvalidFilenames[index].Error())
	}
	for index := range e.DuplicateVersions {
		problems = append(problems, e.DuplicateVersions[index].Error())
	}
	return strings.Join(problems, "; ")
}

// Unwrap exposes each structured group problem to errors.Is and errors.As.
func (e *GroupError) Unwrap() []error {
	problems := make([]error, 0, len(e.InvalidFilenames)+len(e.DuplicateVersions))
	for index := range e.InvalidFilenames {
		problems = append(problems, &e.InvalidFilenames[index])
	}
	for index := range e.DuplicateVersions {
		problems = append(problems, &e.DuplicateVersions[index])
	}
	return problems
}

// Group contains every valid definition associated with one canonical logical
// name and, when valid, the numerically latest selected definition.
type Group struct {
	CanonicalName string
	Definitions   []Definition
	Selected      *Definition
	Err           *GroupError
}

// Catalog is the deterministic set of logical workflow groups from one source.
// Source precedence is deliberately applied by callers.
type Catalog struct {
	Groups []Group
}

// Lookup returns a logical group by its canonical name.
func (c Catalog) Lookup(canonicalName string) (Group, bool) {
	index := sort.Search(len(c.Groups), func(index int) bool {
		return c.Groups[index].CanonicalName >= canonicalName
	})
	if index == len(c.Groups) || c.Groups[index].CanonicalName != canonicalName {
		return Group{}, false
	}
	return c.Groups[index], true
}

// Build groups source-relative candidate paths from one workflow source.
// Underscore-prefixed paths and repeated enumeration of the same path are
// ignored.
func Build(candidatePaths []string) Catalog {
	uniquePaths := make(map[string]struct{}, len(candidatePaths))
	for _, candidatePath := range candidatePaths {
		if IsExempt(candidatePath) {
			continue
		}
		uniquePaths[candidatePath] = struct{}{}
	}

	orderedPaths := make([]string, 0, len(uniquePaths))
	for candidatePath := range uniquePaths {
		orderedPaths = append(orderedPaths, candidatePath)
	}
	sort.Strings(orderedPaths)

	type groupBuilder struct {
		definitions []Definition
		invalid     []FilenameError
	}
	builders := make(map[string]*groupBuilder)
	for _, candidatePath := range orderedPaths {
		definition, err := Parse(candidatePath)
		if err == nil {
			groupName := definition.CanonicalName
			builder := builders[groupName]
			if builder == nil {
				builder = &groupBuilder{}
				builders[groupName] = builder
			}
			builder.definitions = append(builder.definitions, definition)
			continue
		}

		filenameErr, ok := err.(*FilenameError)
		if !ok {
			continue
		}
		builder := builders[filenameErr.Group]
		if builder == nil {
			builder = &groupBuilder{}
			builders[filenameErr.Group] = builder
		}
		builder.invalid = append(builder.invalid, *filenameErr)
	}

	groupNames := make([]string, 0, len(builders))
	for groupName := range builders {
		groupNames = append(groupNames, groupName)
	}
	sort.Strings(groupNames)

	groups := make([]Group, 0, len(groupNames))
	for _, groupName := range groupNames {
		builder := builders[groupName]
		sort.Slice(builder.definitions, func(left, right int) bool {
			comparison := builder.definitions[left].Version.Compare(builder.definitions[right].Version)
			if comparison != 0 {
				return comparison < 0
			}
			return builder.definitions[left].Path < builder.definitions[right].Path
		})

		duplicates := findDuplicateVersions(groupName, builder.definitions)
		group := Group{
			CanonicalName: groupName,
			Definitions:   builder.definitions,
		}
		if len(builder.invalid) > 0 || len(duplicates) > 0 {
			group.Err = &GroupError{
				CanonicalName:     groupName,
				InvalidFilenames:  builder.invalid,
				DuplicateVersions: duplicates,
			}
		} else if len(group.Definitions) > 0 {
			selected := group.Definitions[len(group.Definitions)-1]
			group.Selected = &selected
		}
		groups = append(groups, group)
	}

	return Catalog{Groups: groups}
}

// IsExempt reports whether the final path element begins with an underscore.
// Source adapters use this to exclude workflow metadata and helper YAML files.
func IsExempt(candidatePath string) bool {
	return strings.HasPrefix(path.Base(slashPath(candidatePath)), "_")
}

// HasYAMLExtension reports whether candidatePath ends in .yaml or .yml,
// regardless of extension case. Source adapters use this to decide which
// files the catalog must validate.
func HasYAMLExtension(candidatePath string) bool {
	ext := strings.ToLower(path.Ext(slashPath(candidatePath)))
	return ext == ".yaml" || ext == ".yml"
}

// Parse validates a source-relative workflow path and derives its logical
// identity. Directory segments are retained in CanonicalName.
func Parse(candidatePath string) (Definition, error) {
	normalizedPath := slashPath(candidatePath)
	base := path.Base(normalizedPath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	group := bestEffortGroup(normalizedPath)
	filenameErr := func(kind FilenameErrorKind) error {
		return &FilenameError{
			Path:    candidatePath,
			Group:   group,
			Kind:    kind,
			Pattern: RequiredFilenamePattern,
			Example: renameExample(group),
		}
	}

	if ext != ".yaml" && ext != ".yml" {
		return Definition{}, filenameErr(FilenameErrorPattern)
	}

	matches := versionedStemPattern.FindStringSubmatch(stem)
	if matches == nil {
		return Definition{}, filenameErr(FilenameErrorPattern)
	}

	logicalName, major, minor := matches[1], matches[2], matches[3]
	directorySegments := catalogDirectorySegments(candidatePath, normalizedPath)
	if containsUppercaseASCII(logicalName) || anySegment(directorySegments, containsUppercaseASCII) {
		return Definition{}, filenameErr(FilenameErrorUppercase)
	}
	if !validLogicalNamePattern.MatchString(logicalName) ||
		anySegment(directorySegments, func(segment string) bool {
			return !validLogicalNamePattern.MatchString(segment)
		}) ||
		!isNormalizedDecimal(major) ||
		!isNormalizedDecimal(minor) {
		return Definition{}, filenameErr(FilenameErrorPattern)
	}

	version := Version{Major: major, Minor: minor}
	segments := strings.Split(normalizedPath, "/")
	canonicalDirectorySegments := segments[:len(segments)-1]
	canonicalName := logicalName
	if len(canonicalDirectorySegments) > 0 {
		canonicalName = strings.Join(append(canonicalDirectorySegments, logicalName), "/")
	}
	return Definition{
		Path:          candidatePath,
		LogicalName:   logicalName,
		CanonicalName: canonicalName,
		Version:       version,
		DisplayLabel:  version.Label(),
	}, nil
}

func slashPath(candidatePath string) string {
	return strings.ReplaceAll(candidatePath, `\`, "/")
}

func compareDecimal(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func findDuplicateVersions(canonicalName string, definitions []Definition) []DuplicateVersionError {
	var duplicates []DuplicateVersionError
	for start := 0; start < len(definitions); {
		end := start + 1
		for end < len(definitions) && definitions[start].Version.Compare(definitions[end].Version) == 0 {
			end++
		}
		if end-start > 1 {
			paths := make([]string, 0, end-start)
			for _, definition := range definitions[start:end] {
				paths = append(paths, definition.Path)
			}
			version := definitions[start].Version
			duplicates = append(duplicates, DuplicateVersionError{
				CanonicalName: canonicalName,
				Version:       version,
				Paths:         paths,
				Pattern:       RequiredFilenamePattern,
				Example:       duplicateRenameExample(canonicalName, version),
			})
		}
		start = end
	}
	return duplicates
}

func bestEffortGroup(candidatePath string) string {
	base := path.Base(candidatePath)
	ext := path.Ext(base)
	stem := base
	if strings.EqualFold(ext, ".yaml") || strings.EqualFold(ext, ".yml") {
		stem = strings.TrimSuffix(base, ext)
	}

	if matches := malformedVersionAttempt.FindStringSubmatch(stem); matches != nil {
		stem = matches[1]
	}
	stem = strings.ToLower(stem)

	dir := strings.ToLower(path.Dir(candidatePath))
	group := stem
	if dir != "." {
		group = path.Join(dir, stem)
	}
	return group
}

func renameExample(group string) string {
	logicalName := exampleLogicalName(group)
	return logicalName + "-v1.0.yaml"
}

func duplicateRenameExample(group string, version Version) string {
	return exampleLogicalName(group) + "-v" + version.Major + "." + incrementDecimal(version.Minor) + ".yaml"
}

func exampleLogicalName(group string) string {
	logicalName := strings.ToLower(path.Base(group))
	logicalName = invalidExampleRunPattern.ReplaceAllString(logicalName, "-")
	logicalName = repeatedExampleDashPattern.ReplaceAllString(logicalName, "-")
	logicalName = strings.Trim(logicalName, "-")
	if logicalName == "" {
		logicalName = "workflow"
	}
	return logicalName
}

func incrementDecimal(value string) string {
	digits := []byte(value)
	carry := byte(1)
	for index := len(digits) - 1; index >= 0 && carry == 1; index-- {
		if digits[index] == '9' {
			digits[index] = '0'
			continue
		}
		digits[index]++
		carry = 0
	}
	if carry == 1 {
		return "1" + string(digits)
	}
	return string(digits)
}

func containsUppercaseASCII(value string) bool {
	for _, char := range value {
		if char >= 'A' && char <= 'Z' {
			return true
		}
	}
	return false
}

func catalogDirectorySegments(candidatePath, normalizedPath string) []string {
	if filepath.IsAbs(candidatePath) ||
		strings.HasPrefix(normalizedPath, "./") ||
		strings.HasPrefix(normalizedPath, "../") {
		return nil
	}
	identityPath := strings.TrimPrefix(normalizedPath, "builtin:")
	identityPath = strings.TrimPrefix(identityPath, ".agent-runner/workflows/")
	identityPath = strings.TrimPrefix(identityPath, "workflows/")
	segments := strings.Split(identityPath, "/")
	return segments[:len(segments)-1]
}

func anySegment(segments []string, predicate func(string) bool) bool {
	for _, segment := range segments {
		if predicate(segment) {
			return true
		}
	}
	return false
}

func isNormalizedDecimal(value string) bool {
	return value == "0" || (value != "" && value[0] != '0')
}
