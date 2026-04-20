package textfmt

import (
	"fmt"
	"regexp"
)

var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Interpolate replaces {{variable}} placeholders in a template string.
// Precedence, lowest to highest: builtins, params, capturedVars.
// Builtins are runner-provided values like session_dir that workflows can
// reference without declaring them as params.
func Interpolate(template string, params, capturedVars, builtins map[string]string) (string, error) {
	merged := make(map[string]string)
	for k, v := range builtins {
		merged[k] = v
	}
	for k, v := range params {
		merged[k] = v
	}
	for k, v := range capturedVars {
		merged[k] = v
	}

	var errFound error
	result := placeholderRe.ReplaceAllStringFunc(template, func(match string) string {
		key := placeholderRe.FindStringSubmatch(match)[1]
		value, ok := merged[key]
		if !ok {
			errFound = fmt.Errorf("undefined variable: {{%s}}", key)
			return match
		}
		return value
	})

	if errFound != nil {
		return "", errFound
	}
	return result, nil
}
