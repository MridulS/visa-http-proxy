package main

import (
	"crypto/sha256"
	"encoding/base64"
)

// userCacheKey derives the instance-authorization cache key from the access
// token. It is a hash of the WHOLE token, NOT a JWT claim.
//
// The proxy does not verify the token's signature (the VISA API is the
// authority), so keying the cache on a claim like uidNumber/sub would let a
// forged token reproduce a victim's cache key and hit a warm cache entry,
// skipping the API authorization entirely (cross-user instance access). Hashing
// the exact token bytes means a cache hit can only be produced by possessing the
// genuine signed token. Repeated requests in one session reuse the same token
// (so they still hit the cache); a token refresh produces a new key (a fresh
// authorization), which is correct.
func userCacheKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
