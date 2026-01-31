package main

import "regexp"

// Pre-compiled dangerous action detection patterns for Polish/English voice commands (improves performance vs runtime compilation)
var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)wyłącz wszystk`),
	regexp.MustCompile(`(?i)turn off all`),
	regexp.MustCompile(`(?i)zamknij dom`),
	regexp.MustCompile(`(?i)ustaw temperatur[ęe] (na|do) (1[0-5]|[0-9])($|\s)`),
}

func isDangerousAction(text string) bool {
	for _, pattern := range dangerousPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}

	return false
}
