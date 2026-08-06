package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func makeJWT(payloadJSON string) string {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return enc(`{"alg":"HS256","typ":"JWT"}`) + "." + enc(payloadJSON) + "." + enc("sig")
}

func TestUserCacheKey(t *testing.T) {
	// Same token -> same key (so a session's repeated requests hit the cache).
	// The second call gets a distinct copy: the key must come from the token's
	// bytes, not from the identity of the string it arrived in.
	tok := makeJWT(`{"uidNumber":124849,"sub":"a"}`)
	key := userCacheKey(tok)
	if again := userCacheKey(strings.Clone(tok)); again != key {
		t.Fatalf("identical tokens produced different keys: %q vs %q", again, key)
	}

	// IDOR guard: tokens that differ in ANY bytes must NOT collide — even if they
	// share an identity claim. A forged token can't reproduce a victim's key.
	forged := makeJWT(`{"uidNumber":124849}`) // same uid, different payload bytes
	if userCacheKey(forged) == key {
		t.Fatal("tokens differing in bytes must not share a cache key")
	}

	// Distinct users -> distinct keys.
	if userCacheKey(makeJWT(`{"sub":"a"}`)) == userCacheKey(makeJWT(`{"sub":"b"}`)) {
		t.Fatal("distinct tokens collided")
	}

	// Never empty, and works for an opaque (non-JWT) token too.
	if k := userCacheKey("not-a-jwt"); k == "" {
		t.Fatal("key must be non-empty")
	}
	if userCacheKey("not-a-jwt") == userCacheKey("other") {
		t.Fatal("distinct opaque tokens collided")
	}
}
