package textfmt

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var placeholderRe = regexp.MustCompile(`\{\{(\w+(?:\.\w+)?)\}\}`)

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
		if !ok && strings.Contains(key, ".") {
			parts := strings.SplitN(key, ".", 2)
			raw, rawOK := capturedVars[parts[0]]
			if rawOK {
				var fields map[string]string
				if err := json.Unmarshal([]byte(raw), &fields); err != nil {
					errFound = fmt.Errorf("variable {{%s}} is not a map capture", parts[0])
					return match
				}
				value, ok = fields[parts[1]]
			}
		}
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

// ShellQuote wraps s in single quotes with proper escaping so it is safe
// for interpolation into a shell command string.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// InterpolateShellSafe performs the same substitution as Interpolate but
// shell-quotes every substituted value, preventing injection when the
// result is passed to sh -c.
func InterpolateShellSafe(template string, params, capturedVars, builtins map[string]string) (string, error) {
	return Interpolate(template, shellQuoteMap(params), shellQuoteMap(capturedVars), shellQuoteMap(builtins))
}

func shellQuoteMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	quoted := make(map[string]string, len(m))
	for k, v := range m {
		quoted[k] = ShellQuote(v)
	}
	return quoted
}
