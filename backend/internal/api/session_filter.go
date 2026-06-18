package api

import "strings"

func isSupportedSessionID(sessionID string) bool {
	return strings.HasPrefix(strings.TrimSpace(sessionID), "ses_")
}
