package cli

import "strings"

func excludedSessionIDSet(ids []string) map[string]struct{} {
	excluded := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			excluded[id] = struct{}{}
		}
	}
	return excluded
}
