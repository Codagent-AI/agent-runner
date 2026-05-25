package audit

import "regexp"

const Placeholder = "<REDACTED>"

var Patterns = []*regexp.Regexp{
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]+`),
	regexp.MustCompile(`sk-[A-Za-z0-9]+`),
	regexp.MustCompile(`Bearer [A-Za-z0-9._\-]+`),
	regexp.MustCompile(`[A-Za-z0-9_]*_TOKEN=\S+`),
	regexp.MustCompile(`password=\S+`),
}

func Redact(s string) string {
	for _, pattern := range Patterns {
		s = pattern.ReplaceAllString(s, Placeholder)
	}
	return s
}
