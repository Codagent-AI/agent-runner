package textfmt

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
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
	return interpolateShellSafe(template, func(key string) (string, error) {
		return resolveLegacyStringValue(key, params, capturedVars, builtins)
	})
}

func InterpolateTyped(template string, params map[string]string, capturedVars map[string]model.CapturedValue, builtins map[string]string) (string, error) {
	var errFound error
	result := placeholderRe.ReplaceAllStringFunc(template, func(match string) string {
		key := placeholderRe.FindStringSubmatch(match)[1]
		value, err := resolveStringValue(key, params, capturedVars, builtins)
		if err != nil {
			errFound = err
			return match
		}
		return value
	})
	if errFound != nil {
		return "", errFound
	}
	return result, nil
}

func ResolveTypedValue(expr string, capturedVars map[string]model.CapturedValue) (model.CapturedValue, error) {
	matches := placeholderRe.FindStringSubmatch(expr)
	if len(matches) != 2 || matches[0] != expr {
		return model.CapturedValue{}, fmt.Errorf("typed value must be a whole capture expression like {{name}}")
	}
	key := matches[1]
	if strings.Contains(key, ".") {
		return model.CapturedValue{}, fmt.Errorf("typed value expression must reference a whole capture, got {{%s}}", key)
	}
	value, ok := capturedVars[key]
	if !ok {
		return model.CapturedValue{}, fmt.Errorf("undefined variable: {{%s}}", key)
	}
	return value, nil
}

func InterpolateShellSafeTyped(template string, params map[string]string, capturedVars map[string]model.CapturedValue, builtins map[string]string) (string, error) {
	return interpolateShellSafe(template, func(key string) (string, error) {
		return resolveStringValue(key, params, capturedVars, builtins)
	})
}

func interpolateShellSafe(template string, resolve func(string) (string, error)) (string, error) {
	matches := placeholderRe.FindAllStringSubmatchIndex(template, -1)
	if len(matches) == 0 {
		return template, nil
	}

	var out strings.Builder
	out.Grow(len(template))
	last := 0
	for _, match := range matches {
		out.WriteString(template[last:match[0]])
		key := template[match[2]:match[3]]
		value, err := resolve(key)
		if err != nil {
			return "", err
		}
		switch shellQuoteContext(template[:match[0]]) {
		case shellContextDouble:
			out.WriteString(escapeDoubleQuotedShell(value))
		case shellContextSingle:
			return "", fmt.Errorf("cannot safely interpolate {{%s}} inside single quotes", key)
		default:
			out.WriteString(ShellQuote(value))
		}
		last = match[1]
	}
	out.WriteString(template[last:])
	return out.String(), nil
}

type shellContext int

const (
	shellContextBare shellContext = iota
	shellContextSingle
	shellContextDouble
)

func shellQuoteContext(prefix string) shellContext {
	state := shellContextBare
	for i := 0; i < len(prefix); i++ {
		switch state {
		case shellContextBare:
			switch prefix[i] {
			case '\'':
				state = shellContextSingle
			case '"':
				state = shellContextDouble
			case '\\':
				i++
			}
		case shellContextSingle:
			if prefix[i] == '\'' {
				state = shellContextBare
			}
		case shellContextDouble:
			switch prefix[i] {
			case '"':
				state = shellContextBare
			case '\\':
				if i+1 < len(prefix) && strings.ContainsRune("$`\"\\\n", rune(prefix[i+1])) {
					i++
				}
			}
		}
	}
	return state
}

func escapeDoubleQuotedShell(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`$`, `\$`,
		"`", "\\`",
		`"`, `\"`,
	)
	return replacer.Replace(s)
}

func resolveLegacyStringValue(key string, params, capturedVars, builtins map[string]string) (string, error) {
	if value, ok := capturedVars[key]; ok {
		return value, nil
	}
	if value, ok := params[key]; ok {
		return value, nil
	}
	if value, ok := builtins[key]; ok {
		return value, nil
	}
	if strings.Contains(key, ".") {
		parts := strings.SplitN(key, ".", 2)
		raw, ok := capturedVars[parts[0]]
		if !ok {
			return "", fmt.Errorf("undefined variable: {{%s}}", key)
		}
		var fields map[string]string
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			return "", fmt.Errorf("variable {{%s}} is not a map capture", parts[0])
		}
		value, ok := fields[parts[1]]
		if !ok {
			return "", fmt.Errorf("undefined field: {{%s}}", key)
		}
		return value, nil
	}
	return "", fmt.Errorf("undefined variable: {{%s}}", key)
}

func resolveStringValue(key string, params map[string]string, capturedVars map[string]model.CapturedValue, builtins map[string]string) (string, error) {
	if strings.Contains(key, ".") {
		parts := strings.SplitN(key, ".", 2)
		captured, ok := capturedVars[parts[0]]
		if !ok {
			return "", fmt.Errorf("undefined variable: {{%s}}", key)
		}
		if captured.Kind != model.CaptureMap {
			return "", fmt.Errorf("field access requires map-typed capture: {{%s}}", key)
		}
		value, ok := captured.Map[parts[1]]
		if !ok {
			return "", fmt.Errorf("undefined field: {{%s}}", key)
		}
		return value, nil
	}
	if captured, ok := capturedVars[key]; ok {
		switch captured.Kind {
		case model.CaptureString:
			return captured.Str, nil
		case model.CaptureList:
			return "", fmt.Errorf("list capture {{%s}} cannot be interpolated in a string context", key)
		case model.CaptureMap:
			return "", fmt.Errorf("map capture {{%s}} cannot be interpolated in a string context; use {{%s.<field>}}", key, key)
		default:
			return "", fmt.Errorf("capture {{%s}} has unknown kind %q", key, captured.Kind)
		}
	}
	if value, ok := params[key]; ok {
		return value, nil
	}
	if value, ok := builtins[key]; ok {
		return value, nil
	}
	return "", fmt.Errorf("undefined variable: {{%s}}", key)
}
