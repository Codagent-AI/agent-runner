package textfmt

import (
	"fmt"
	"regexp"
)

var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Interpolate replaces {{variable}} placeholders in a template string.
// Captured variables take precedence over params when names collide.
func Interpolate(template string, params, capturedVars map[string]string) (string, error) {
	merged := make(map[string]string)
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
