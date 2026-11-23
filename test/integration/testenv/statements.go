package testenv

import "strings"

// StatementName is the sqlc name of a statement ("-- name: GetLiveItem :many"), or its first words. A statement budget counts
// the statements that ran with a request's ID by this name (RequestTransactions, Q-ARCH-700); no test reads the database's log.
func StatementName(s string) string {
	if i := strings.Index(s, "-- name: "); i >= 0 {
		// The first word after the marker, without splitting the rest of the statement (RequestTransactions names every
		// statement of a request with it while the request runs).
		rest := strings.TrimLeft(s[i+len("-- name: "):], " \t\r\n")
		if j := strings.IndexAny(rest, " \t\r\n"); j >= 0 {
			rest = rest[:j]
		}
		if rest != "" {
			return rest
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
