package model

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func aliasParam() Param {
	return Param{Name: "v", Default: "auto", Aliases: map[string]string{
		"auto": "builtin:core/auto-v1.0.yaml",
		"none": "builtin:core/none-v1.0.yaml",
	}}
}

func TestParamBindReturnsAliasTargetOrValue(t *testing.T) {
	p := aliasParam()
	if got := p.Bind("none"); got != "builtin:core/none-v1.0.yaml" {
		t.Fatalf("Bind(none) = %q", got)
	}
	if got := p.Bind("/abs/path-v1.0.yaml"); got != "/abs/path-v1.0.yaml" {
		t.Fatalf("Bind(path) = %q", got)
	}
}

func TestParamBindIsIdempotent(t *testing.T) {
	p := aliasParam()
	once := p.Bind("auto")
	if twice := p.Bind(once); twice != once {
		t.Fatalf("second bind changed %q to %q", once, twice)
	}
}

func TestWorkflowBindAliasesRewritesOnlyAliasedParams(t *testing.T) {
	w := Workflow{Params: []Param{aliasParam(), {Name: "other"}}}
	params := map[string]string{"v": "none", "other": "none"}
	w.BindAliases(params)
	want := map[string]string{"v": "builtin:core/none-v1.0.yaml", "other": "none"}
	if diff := cmp.Diff(want, params); diff != "" {
		t.Fatalf("params mismatch (-want +got):\n%s", diff)
	}
}

func TestWorkflowValidateRejectsEmptyAlias(t *testing.T) {
	for name, aliases := range map[string]map[string]string{
		"empty key":   {"": "x"},
		"empty value": {"a": ""},
	} {
		t.Run(name, func(t *testing.T) {
			w := Workflow{
				Name:   "w",
				Params: []Param{{Name: "v", Aliases: aliases}},
				Steps:  []Step{{ID: "s", Command: "true"}},
			}
			err := w.Validate(nil)
			if err == nil || !strings.Contains(err.Error(), "alias") {
				t.Fatalf("Validate error = %v, want alias error", err)
			}
		})
	}
}

func TestParamCheckShortNameRejectsUnknownBareName(t *testing.T) {
	p := aliasParam()
	err := p.CheckShortName("bogus")
	if err == nil {
		t.Fatal("CheckShortName(bogus) succeeded, want an unknown short name error")
	}
	want := `unknown short name "bogus" for parameter "v"; use one of: auto, none, or a full value such as builtin:core/auto-v1.0.yaml`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestParamCheckShortNameAcceptsKnownNamesAndFullValues(t *testing.T) {
	p := aliasParam()
	for _, value := range []string{"auto", "none", "", "builtin:core/none-v1.0.yaml", "/abs/custom-v1.0.yaml", "custom-v1.0.yaml", "dir/custom-v1.0.yaml"} {
		if err := p.CheckShortName(value); err != nil {
			t.Errorf("CheckShortName(%q) = %v, want nil", value, err)
		}
	}
	plain := Param{Name: "other"}
	if err := plain.CheckShortName("bogus"); err != nil {
		t.Errorf("param without aliases: CheckShortName(bogus) = %v, want nil", err)
	}
}
