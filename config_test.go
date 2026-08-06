package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The pathRewrite rules must apply in JSON declaration order, first match wins —
// a Go map would lose this order. This guards the ordered decode.
func TestRewriteRulesOrderedFirstMatchWins(t *testing.T) {
	var conf ProxyConf
	raw := `{
		"match": "^/multi/(\\d+).*",
		"type": "service",
		"name": "Multi",
		"remotePort": 9000,
		"pathRewrite": {"^/multi/\\d+/a/": "/first/", "^/multi/\\d+/": "/second/"}
	}`
	if err := json.Unmarshal([]byte(raw), &conf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(conf.PathRewrite) != 2 {
		t.Fatalf("want 2 rules, got %d", len(conf.PathRewrite))
	}
	if conf.PathRewrite[0].Repl != "/first/" || conf.PathRewrite[1].Repl != "/second/" {
		t.Fatalf("rules out of order: %+v", conf.PathRewrite)
	}
	// /a/x matches both rules; the first-declared one wins.
	if got := conf.PathRewrite.Apply("/multi/123/a/x"); got != "/first/x" {
		t.Fatalf("Apply(/multi/123/a/x) = %q, want /first/x", got)
	}
	// /b/x matches only the second rule.
	if got := conf.PathRewrite.Apply("/multi/123/b/x"); got != "/second/b/x" {
		t.Fatalf("Apply(/multi/123/b/x) = %q, want /second/b/x", got)
	}
	// No rule matches -> unchanged.
	if got := conf.PathRewrite.Apply("/other/1/x"); got != "/other/1/x" {
		t.Fatalf("Apply(/other/1/x) = %q, want unchanged", got)
	}
}

// A handler's match regex must have a capture group for the instance id, else it
// would route requests that then always 404. LoadHandlers should reject it at load.
func TestLoadHandlersRequiresCaptureGroup(t *testing.T) {
	write := func(body string) string {
		p := filepath.Join(t.TempDir(), "proxy.conf.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := LoadHandlers(write(`[{"match":"^/x/.*","type":"service","name":"X","remotePort":1}]`)); err == nil {
		t.Fatal("expected an error for a match without a capture group")
	}
	if _, err := LoadHandlers(write(`[{"match":"^/x/(\\d+).*","type":"service","name":"X","remotePort":1}]`)); err != nil {
		t.Fatalf("unexpected error for a valid match: %v", err)
	}
}

func TestRewriteFirstMatchOnly(t *testing.T) {
	var rr RewriteRules
	if err := json.Unmarshal([]byte(`{"/a/": "/X/"}`), &rr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Only the first occurrence is replaced (not global).
	if got := rr.Apply("/a/a/a/"); got != "/X/a/a/" {
		t.Fatalf("Apply = %q, want /X/a/a/", got)
	}
}
