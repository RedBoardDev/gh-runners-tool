package notification

import "strings"

type EventFilter struct {
	Patterns []string
}

func (f EventFilter) Matches(eventType, level string) bool {
	if len(f.Patterns) == 0 {
		return true
	}

	for _, p := range f.Patterns {
		if matchesPattern(p, eventType, level) {
			return true
		}
	}
	return false
}

func matchesPattern(pattern, eventType, level string) bool {
	if strings.HasPrefix(pattern, "*:") {
		return strings.EqualFold(pattern[2:], level)
	}

	if strings.HasSuffix(pattern, ".*") {
		prefix := pattern[:len(pattern)-2]
		return strings.HasPrefix(eventType, prefix+".")
	}

	return pattern == eventType
}
