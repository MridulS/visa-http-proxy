package main

import (
	"net/http"
	"strings"
)

// ExtractToken returns the access token from the Authorization Bearer header,
// falling back to the access_token cookie. A malformed Authorization scheme (no
// "Bearer " prefix) or an empty bearer value falls through to the cookie. ok is
// false when neither source yields a non-empty token.
func ExtractToken(r *http.Request) (string, bool) {
	if a := r.Header.Get("Authorization"); a != "" {
		// scheme match is case-insensitive ("bearer ", "BEARER ", ...)
		if len(a) >= 7 && strings.EqualFold(a[:7], "bearer ") {
			if tok := strings.TrimSpace(a[7:]); tok != "" {
				return tok, true
			}
			// empty after trim -> fall through to cookie
		}
		// non-Bearer scheme -> fall through to cookie
	}
	if c, err := r.Cookie("access_token"); err == nil && c.Value != "" {
		return c.Value, true
	}
	return "", false
}
